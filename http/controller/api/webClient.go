package api

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/http/response/api"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type WebClient struct {
}

// ServerConfig 服务配置
// @Tags WEBCLIENT
// @Summary 服务配置
// @Description 服务配置,给webclient提供api-server
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /server-config [get]
// @Security token
func (i *WebClient) ServerConfig(c *gin.Context) {
	u := service.AllService.UserService.CurUser(c)

	peers := map[string]*api.WebClientPeerPayload{}
	abs := service.AllService.AddressBookService.ListByUserIdAndCollectionId(u.Id, 0, 1, 100)
	for _, ab := range abs.AddressBooks {
		pp := &api.WebClientPeerPayload{}
		pp.FromAddressBook(ab)
		peers[string(ab.Id)] = pp
	}
	response.Success(
		c,
		gin.H{
			"id_server":    global.Config.Rustdesk.IdServer,
			"relay_server": global.Config.Rustdesk.RelayServer,
			"key":          global.Config.Rustdesk.Key,
			"peers":        peers,
		},
	)
}

// SharedPeer 分享的peer
// @Tags WEBCLIENT
// @Summary 分享的peer
// @Description 分享的peer
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /shared-peer [post]
func (i *WebClient) SharedPeer(c *gin.Context) {
	var input struct {
		ShareToken string `json:"share_token"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.ShareToken == "" {
		response.Fail(c, 101, "share_token is required")
		return
	}
	sr := service.AllService.AddressBookService.SharedPeer(input.ShareToken)
	if sr == nil || sr.Id == 0 {
		response.Fail(c, 101, "share not found")
		return
	}
	if expiry := sr.ExpiryTime(); !expiry.IsZero() && expiry.Before(time.Now()) {
		response.Fail(c, 101, "share expired")
		return
	}

	ab, err := service.AllService.AddressBookService.ShareableEntry(sr.AddressBookRowId, sr.UserId, sr.PeerId)
	if errors.Is(err, service.ErrWebClientExplicitServerTarget) || errors.Is(err, service.ErrWebClientDirectTarget) ||
		errors.Is(err, service.ErrWebClientTargetWhitespace) || errors.Is(err, service.ErrWebClientEmptyTarget) {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
		return
	}
	if err != nil {
		response.Fail(c, 101, "peer not found")
		return
	}
	if err := service.AllService.ServerCmdService.CheckAnonymousWebClientShare(); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
		return
	}
	pp := &api.WebClientPeerPayload{}
	pp.FromShareRecord(sr)
	pp.Info.Username = ab.Username
	pp.Info.Hostname = ab.Hostname
	response.Success(c, gin.H{
		"id_server":    global.Config.Rustdesk.IdServer,
		"relay_server": global.Config.Rustdesk.RelayServer,
		"key":          global.Config.Rustdesk.Key,
		"peer":         pp,
	})
}

// ServerConfigV2 服务配置
// @Tags WEBCLIENT_V2
// @Summary 服务配置
// @Description 服务配置,给webclient提供api-server
// @Accept  json
// @Produce  json
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /server-config-v2 [get]
// @Security token
func (i *WebClient) ServerConfigV2(c *gin.Context) {
	response.Success(
		c,
		gin.H{
			"id_server": global.Config.Rustdesk.IdServer,
			"key":       global.Config.Rustdesk.Key,
		},
	)
}
