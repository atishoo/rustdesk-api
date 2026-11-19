package service

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

type ServerCmdService struct{}

// List
func (is *ServerCmdService) List(page, pageSize uint) (res *model.ServerCmdList) {
	res = &model.ServerCmdList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.ServerCmd{})
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.ServerCmds)
	return
}

// Info
func (is *ServerCmdService) Info(id uint) *model.ServerCmd {
	u := &model.ServerCmd{}
	DB.Where("id = ?", id).First(u)
	return u
}

// Delete
func (is *ServerCmdService) Delete(u *model.ServerCmd) error {
	return DB.Delete(u).Error
}

// Create
func (is *ServerCmdService) Create(u *model.ServerCmd) error {
	res := DB.Create(u).Error
	return res
}

// SendCmd 发送命令
func (is *ServerCmdService) SendCmd(port int, cmd string, arg string) (string, error) {
	return is.sendCmdToLoopback(port, cmd, arg, "::1", "127.0.0.1")
}

func (is *ServerCmdService) sendCmdToLoopback(port int, cmd, arg, host string, fallback ...string) (string, error) {
	deadline := time.Now().Add(3 * time.Second)
	dialer := net.Dialer{Deadline: deadline}
	portString := strconv.Itoa(port)
	var conn net.Conn
	var err error
	for _, host := range append([]string{host}, fallback...) {
		conn, err = dialer.Dial("tcp", net.JoinHostPort(host, portString))
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return "", err
	}
	if _, err := io.WriteString(conn, cmd+" "+arg); err != nil {
		return "", err
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		return "", err
	}
	const maxResponseBytes = 1 << 20
	result, err := io.ReadAll(io.LimitReader(conn, maxResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(result) > maxResponseBytes {
		return "", fmt.Errorf("server command response exceeds %d bytes", maxResponseBytes)
	}
	return string(result), nil
}

// CheckAnonymousWebClientShare checks the configured ID Server's login policy.
func (is *ServerCmdService) CheckAnonymousWebClientShare() error {
	if strings.TrimSpace(Config.Rustdesk.IdServer) == "" {
		return ErrWebClientMissingIDServer
	}
	if mustLogin := Config.Rustdesk.MustLogin; mustLogin != nil {
		if *mustLogin {
			return ErrWebClientShareRequiresLogin
		}
		return nil
	}
	port := Config.Admin.IdServerPort
	host, rawPort, err := net.SplitHostPort(addressBookServerKey(Config.Rustdesk.IdServer))
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return ErrWebClientLoginPolicyUnknown
	}
	configuredPort, err := strconv.Atoi(rawPort)
	if err != nil || port < 2 || configuredPort != port {
		return ErrWebClientLoginPolicyUnknown
	}
	status, err := is.sendCmdToLoopback(port-1, "ml", "", host)
	if err != nil {
		return ErrWebClientLoginPolicyUnknown
	}
	switch strings.TrimSpace(status) {
	case "MUST_LOGIN: false":
		return nil
	case "MUST_LOGIN: true":
		return ErrWebClientShareRequiresLogin
	}
	if os.Getenv("RUSTDESK_API_S6_LOCAL_CONTROL") != "1" {
		return ErrWebClientLoginPolicyUnknown
	}
	// Older bundled ID Servers do not implement ml. Only use the startup
	// policy when the same control endpoint identifies itself and omits ml.
	help, err := is.sendCmdToLoopback(port-1, "h", "", host)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(help), "relay-servers(rs) ") {
		return ErrWebClientLoginPolicyUnknown
	}
	for _, entry := range strings.Fields(help) {
		if entry == "ml" || strings.HasPrefix(entry, "must-login") || strings.Contains(entry, "(ml)") {
			return ErrWebClientLoginPolicyUnknown
		}
	}
	if strings.EqualFold(os.Getenv("MUST_LOGIN"), "Y") {
		return ErrWebClientShareRequiresLogin
	}
	return nil
}

func (is *ServerCmdService) Update(f *model.ServerCmd) error {
	return DB.Model(&model.ServerCmd{}).Where("id = ?", f.Id).Updates(map[string]interface{}{
		"cmd":     f.Cmd,
		"alias":   f.Alias,
		"option":  f.Option,
		"explain": f.Explain,
		"target":  f.Target,
	}).Error
}
