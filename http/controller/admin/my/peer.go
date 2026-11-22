package my

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/request/admin"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"gorm.io/gorm"
)

type Peer struct {
}

// List 列表
// @Tags 我的设备
// @Summary 设备列表
// @Description 设备列表
// @Accept  json
// @Produce  json
// @Param page query int false "页码"
// @Param page_size query int false "页大小"
// @Param time_ago query int false "时间"
// @Param id query string false "ID"
// @Param hostname query string false "主机名"
// @Param uuids query string false "uuids 用逗号分隔"
// @Success 200 {object} response.Response{data=model.PeerList}
// @Failure 500 {object} response.Response
// @Router /admin/my/peer/list [get]
// @Security token
func (ct *Peer) List(c *gin.Context) {
	query := &admin.PeerQuery{}
	if err := c.ShouldBindQuery(query); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	u := service.AllService.UserService.CurUser(c)
	res := service.AllService.PeerService.ListWithAmbiguity(query.Page, query.PageSize, func(tx *gorm.DB) {
		tx.Where("user_id = ?", u.Id)
		if query.TimeAgo > 0 {
			lt := time.Now().Unix() - int64(query.TimeAgo)
			tx.Where("last_online_time < ?", lt)
		}
		if query.TimeAgo < 0 {
			lt := time.Now().Unix() + int64(query.TimeAgo)
			tx.Where("last_online_time > ?", lt)
		}
		if query.Id != "" {
			tx.Where("id like ?", "%"+query.Id+"%")
		}
		if query.Hostname != "" {
			tx.Where("hostname like ?", "%"+query.Hostname+"%")
		}
		if query.Uuids != "" {
			tx.Where("uuid in (?)", query.Uuids)
		}
	})
	response.Success(c, res)
}

// BatchUpdateTags replaces the account tags for the selected devices.
// @Tags 我的设备
// @Summary 批量更新设备标签
// @Description 批量更新当前账号设备的标签
// @Accept json
// @Produce json
// @Param body body admin.BatchUpdateTagsForm true "设备标签"
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /admin/my/peer/batchUpdateTags [post]
// @Security token
func (ct *Peer) BatchUpdateTags(c *gin.Context) {
	f := &admin.BatchUpdateTagsForm{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	if len(f.RowIds) == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	u := service.AllService.UserService.CurUser(c)
	peers := service.AllService.PeerService.List(1, uint(len(f.RowIds)), func(tx *gorm.DB) {
		tx.Where("row_id IN ?", f.RowIds)
		tx.Where("user_id = ?", u.Id)
	})
	if len(peers.Peers) != len(f.RowIds) {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	if err := service.AllService.PeerService.BatchUpdateTags(peers.Peers, f.Tags); errors.Is(err, service.ErrInvalidAddressBookTags) {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
	} else if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed")+err.Error())
	} else {
		response.Success(c, nil)
	}
}

// ShareByWebClient 直接分享当前账号的设备。
func (ct *Peer) ShareByWebClient(c *gin.Context) {
	if global.Config.App.WebClient != 1 {
		response.Fail(c, 101, "Web Client is disabled")
		return
	}
	f := &admin.ShareByWebClientForm{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	if errList := global.Validator.ValidStruct(c, f); len(errList) > 0 {
		response.Fail(c, 101, errList[0])
		return
	}
	if !f.HasValidPassword() {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	u := service.AllService.UserService.CurUser(c)
	if _, err := service.AllService.AddressBookService.ShareablePeer(f.RowId, u.Id, f.Id); errors.Is(err, service.ErrWebClientExplicitServerTarget) || errors.Is(err, service.ErrWebClientDirectTarget) || errors.Is(err, service.ErrWebClientTargetWhitespace) || errors.Is(err, service.ErrWebClientEmptyTarget) {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
		return
	} else if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	m := f.ToShareRecord(model.ShareSourcePeer)
	m.UserId = u.Id
	if err := service.AllService.AddressBookService.ShareByWebClient(m); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed")+err.Error())
		return
	}
	response.Success(c, &gin.H{"share_token": m.ShareToken})
}
