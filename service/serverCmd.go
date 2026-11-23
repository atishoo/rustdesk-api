package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/lib/ipregion"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/model/custom_types"
	"gorm.io/gorm/clause"
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

// CommandIPLocations persists command IP snapshots and returns locations keyed by their original display values.
func (is *ServerCmdService) CommandIPLocations(target, cmd, option, output string) (map[string]string, error) {
	locations := make(map[string]string)
	fields := strings.Fields(cmd + " " + option)
	if len(fields) == 0 || strings.HasPrefix(strings.TrimSpace(output), "Error:") {
		return locations, nil
	}
	displayIPs := make(map[string]string)
	add := func(display, raw string) {
		if address, err := ipregion.ParseIP(raw); err == nil {
			displayIPs[display] = address.String()
		}
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if target == model.ServerCmdTargetRelayServer {
		switch fields[0] {
		case "blacklist", "b", "blocklist", "B", "blacklist-add", "ba", "blocklist-add", "Ba", "blacklist-remove", "br", "blocklist-remove", "Br":
			arguments := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmd+" "+option), fields[0]))
			for _, raw := range strings.Split(arguments, "|") {
				raw = strings.TrimSpace(raw)
				add(raw, raw)
			}
			for _, raw := range lines {
				raw = strings.TrimSpace(raw)
				add(raw, raw)
			}
		case "usage", "u":
			for _, line := range lines {
				parts := strings.Fields(line)
				if len(parts) < 2 || !strings.HasSuffix(parts[0], ":") {
					continue
				}
				display := strings.TrimSuffix(parts[0], ":")
				separator := strings.LastIndexByte(display, ':')
				if separator < 0 {
					continue
				}
				if _, err := strconv.ParseUint(display[separator+1:], 10, 16); err == nil {
					add(display, display[:separator])
				}
			}
		}
	} else if target == model.ServerCmdTargetIdServer {
		switch fields[0] {
		case "ip-blocker", "ib":
			if len(fields) > 1 {
				add(fields[1], fields[1])
			}
			for _, line := range lines {
				if raw, _, found := strings.Cut(strings.TrimSpace(line), ": "); found {
					add(raw, raw)
				}
			}
		case "ip-changes", "ic":
			for _, line := range lines {
				start := strings.LastIndexByte(line, '{')
				if start < 0 {
					continue
				}
				var changes map[string]int
				if json.Unmarshal([]byte(strings.TrimSpace(line[start:])), &changes) == nil {
					for raw := range changes {
						add(raw, raw)
					}
				}
			}
		case "test-geo", "tg":
			for _, raw := range fields[1:min(len(fields), 3)] {
				add(raw, raw)
			}
		}
	}
	if len(displayIPs) == 0 {
		return locations, nil
	}
	snapshots := make(map[string]string)
	records := make([]model.IPLocation, 0, len(displayIPs))
	updatedAt := custom_types.AutoTime(time.Now())
	for _, ip := range displayIPs {
		if _, exists := snapshots[ip]; !exists {
			location := ipregion.Lookup(ip)
			snapshots[ip] = location
			records = append(records, model.IPLocation{IP: ip, Location: location, UpdatedAt: updatedAt})
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].IP < records[j].IP })
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{"location", "updated_at"}),
	}).CreateInBatches(records, 200).Error; err != nil {
		return nil, err
	}
	for display, ip := range displayIPs {
		locations[display] = snapshots[ip]
	}
	return locations, nil
}
