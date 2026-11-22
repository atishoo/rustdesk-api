package service

import (
	"fmt"
	"io"
	"net"
	"strconv"
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

func (is *ServerCmdService) Update(f *model.ServerCmd) error {
	return DB.Model(&model.ServerCmd{}).Where("id = ?", f.Id).Updates(map[string]interface{}{
		"cmd":     f.Cmd,
		"alias":   f.Alias,
		"option":  f.Option,
		"explain": f.Explain,
		"target":  f.Target,
	}).Error
}
