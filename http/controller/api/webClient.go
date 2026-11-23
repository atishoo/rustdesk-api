package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/http/response/api"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type WebClient struct {
}

// ServerConfig 服务配置
// @Tags WEBCLIENT
// @Summary 服务配置
// @Description 返回账号服务配置及浏览器信令入口 ws_host；保留原始 id_server，ws_host 为空时浏览器沿用 id_server。
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
			"id_server":    global.Config.Capylink.IdServer,
			"ws_host":      global.Config.Capylink.WsHost,
			"relay_server": global.Config.Capylink.RelayServer,
			"key":          global.Config.Capylink.Key,
			"peers":        peers,
		},
	)
}

// SharedPeer 分享的peer
// @Tags WEBCLIENT
// @Summary 分享的peer
// @Description 兑换固定分享设备及临时密码，并返回浏览器信令入口 ws_host；保留原始 id_server，ws_host 为空时浏览器沿用 id_server。
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

	var peerUsername, peerHostname string
	switch sr.Source {
	case model.ShareSourceAddressBook:
		entry, err := service.AllService.AddressBookService.ShareableEntry(sr.AddressBookRowId, sr.UserId, sr.PeerId)
		if err != nil {
			response.Fail(c, 101, "share source unavailable")
			return
		}
		peerUsername = entry.Username
		peerHostname = entry.Hostname
	case model.ShareSourcePeer:
		peer, err := service.AllService.AddressBookService.ShareablePeer(sr.AddressBookRowId, sr.UserId, sr.PeerId)
		if err != nil {
			response.Fail(c, 101, "share source unavailable")
			return
		}
		peerUsername = peer.Username
		peerHostname = peer.Hostname
	default:
		response.Fail(c, 101, "share source unavailable")
		return
	}
	if err := service.AllService.ShareRecordService.StartShare(sr); err != nil {
		response.Fail(c, 101, "share unavailable")
		return
	}
	if expiry := sr.ExpiryTime(); !expiry.IsZero() && expiry.Before(time.Now()) {
		response.Fail(c, 101, "share expired")
		return
	}
	pp := &api.WebClientPeerPayload{}
	pp.FromShareRecord(sr)
	pp.Info.Username = peerUsername
	pp.Info.Hostname = peerHostname
	response.Success(c, gin.H{
		"id_server":    global.Config.Capylink.IdServer,
		"ws_host":      global.Config.Capylink.WsHost,
		"relay_server": global.Config.Capylink.RelayServer,
		"key":          global.Config.Capylink.Key,
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
			"id_server": global.Config.Capylink.IdServer,
			"key":       global.Config.Capylink.Key,
		},
	)
}
