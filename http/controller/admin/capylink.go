package admin

import (
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/request/admin"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type Capylink struct {
}

type CapylinkCmd struct {
	Cmd    string `json:"cmd"`
	Option string `json:"option"`
	Target string `json:"target"`
}

func validCapylinkCmd(cmd, target string) bool {
	return strings.TrimSpace(cmd) != "" &&
		(target == model.ServerCmdTargetIdServer || target == model.ServerCmdTargetRelayServer)
}

func normalizeRelayAddressOption(cmd, option string) (string, bool) {
	addressOption := strings.TrimSpace(option)
	switch cmd {
	case "br", "Br", "blacklist-remove", "blocklist-remove":
		if addressOption == "all" {
			return "all", true
		}
	case "ba", "Ba", "blacklist-add", "blocklist-add":
	case "b", "B", "blacklist", "blocklist":
		if addressOption == "" {
			return "", true
		}
		if strings.Contains(addressOption, "|") {
			return "", false
		}
	default:
		return option, true
	}
	addresses := strings.Split(addressOption, "|")
	for i, value := range addresses {
		value = strings.TrimSpace(value)
		address, err := netip.ParseAddr(value)
		if err != nil || address.Zone() != "" {
			return "", false
		}
		addresses[i] = address.Unmap().String()
	}
	return strings.Join(addresses, "|"), true
}

func (r *Capylink) CmdList(c *gin.Context) {
	q := &admin.PageQuery{}
	if err := c.ShouldBindQuery(q); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	res := service.AllService.ServerCmdService.List(q.Page, 9999)
	//在列表前添加系统命令
	list := make([]*model.ServerCmd, 0)
	list = append(list, model.SysIdServerCmds...)
	list = append(list, model.SysRelayServerCmds...)
	list = append(list, res.ServerCmds...)
	res.ServerCmds = list
	response.Success(c, res)
}

func (r *Capylink) CmdDelete(c *gin.Context) {
	f := &model.ServerCmd{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	if f.Id == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}

	ex := service.AllService.ServerCmdService.Info(f.Id)
	if ex.Id == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}

	err := service.AllService.ServerCmdService.Delete(ex)
	if err != nil {
		response.Fail(c, 101, err.Error())
		return
	}
	response.Success(c, nil)
}
func (r *Capylink) CmdCreate(c *gin.Context) {
	f := &model.ServerCmd{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	f.Cmd = strings.TrimSpace(f.Cmd)
	if !validCapylinkCmd(f.Cmd, f.Target) {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	err := service.AllService.ServerCmdService.Create(f)
	if err != nil {
		response.Fail(c, 101, err.Error())
		return
	}
	response.Success(c, nil)
}

func (r *Capylink) CmdUpdate(c *gin.Context) {
	f := &model.ServerCmd{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	f.Cmd = strings.TrimSpace(f.Cmd)
	if !validCapylinkCmd(f.Cmd, f.Target) {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	ex := service.AllService.ServerCmdService.Info(f.Id)
	if ex.Id == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	err := service.AllService.ServerCmdService.Update(f)
	if err != nil {
		response.Fail(c, 101, err.Error())
		return
	}
	response.Success(c, nil)
}

func (r *Capylink) SendCmd(c *gin.Context) {
	rc := &CapylinkCmd{}
	if err := c.ShouldBindJSON(rc); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	rc.Cmd = strings.TrimSpace(rc.Cmd)
	if !validCapylinkCmd(rc.Cmd, rc.Target) {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}

	locationOption := rc.Option
	port := 0
	switch rc.Target {
	case model.ServerCmdTargetIdServer:
		port = global.Config.Admin.IdServerPort - 1
	case model.ServerCmdTargetRelayServer:
		option, ok := normalizeRelayAddressOption(rc.Cmd, rc.Option)
		if !ok {
			response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
			return
		}
		rc.Option = option
		port = global.Config.Admin.RelayServerPort
	}

	res, err := service.AllService.ServerCmdService.SendCmd(port, rc.Cmd, rc.Option)
	if err != nil {
		response.Fail(c, 101, err.Error())
		return
	}
	locations, err := service.AllService.ServerCmdService.CommandIPLocations(rc.Target, rc.Cmd, locationOption, res)
	if err != nil {
		response.Fail(c, 101, err.Error())
		return
	}
	response.Success(c, gin.H{"output": res, "ip_locations": locations})
}

func (r *Capylink) Status(c *gin.Context) {
	relayStatus := make(chan bool, 1)
	go func() {
		help, err := service.AllService.ServerCmdService.SendCmd(global.Config.Admin.RelayServerPort, "h", "")
		relayStatus <- err == nil && strings.HasPrefix(strings.TrimSpace(help), "blacklist-add(ba) ")
	}()
	idHelp, idErr := service.AllService.ServerCmdService.SendCmd(global.Config.Admin.IdServerPort-1, "h", "")
	response.Success(c, gin.H{
		"id_server":    idErr == nil && strings.HasPrefix(strings.TrimSpace(idHelp), "relay-servers(rs) "),
		"relay_server": <-relayStatus,
		"id_help":      idHelp,
	})
}
