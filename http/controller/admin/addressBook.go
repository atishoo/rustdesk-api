package admin

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/lejianwen/rustdesk-api/v2/global"
	"github.com/lejianwen/rustdesk-api/v2/http/request/admin"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/service"
	"gorm.io/gorm"
)

type AddressBook struct {
}

// List 列表
// @Tags 地址簿
// @Summary 地址簿列表
// @Description 地址簿列表
// @Accept  json
// @Produce  json
// @Param page query int false "页码"
// @Param page_size query int false "页大小"
// @Param user_id query int false "用户id"
// @Param is_my query int false "是否是我的"
// @Success 200 {object} response.Response{data=model.AddressBookList}
// @Failure 500 {object} response.Response
// @Router /admin/address_book/list [get]
// @Security token
func (ct *AddressBook) List(c *gin.Context) {
	query := &admin.AddressBookQuery{}
	if err := c.ShouldBindQuery(query); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	res, err := service.AllService.AddressBookService.ListWithAmbiguity(query.Page, query.PageSize, func(tx *gorm.DB) {
		tx.Preload("Collection", func(txc *gorm.DB) *gorm.DB {
			return txc.Select("id,name")
		})
		if query.Id != "" {
			tx.Where("id like ?", "%"+query.Id+"%")
		}
		if query.UserId > 0 {
			tx.Where("user_id = ?", query.UserId)
		}
		if query.Username != "" {
			tx.Where("username like ?", "%"+query.Username+"%")
		}
		if query.Hostname != "" {
			tx.Where("hostname like ?", "%"+query.Hostname+"%")
		}
		if query.CollectionId != nil && *query.CollectionId >= 0 {
			tx.Where("collection_id = ?", query.CollectionId)
		}
	})

	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed"))
		return
	}
	response.Success(c, res)
}

// Delete 删除
// @Tags 地址簿
// @Summary 地址簿删除
// @Description 地址簿删除
// @Accept  json
// @Produce  json
// @Param body body admin.AddressBookForm true "地址簿信息"
// @Success 200 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /admin/address_book/delete [post]
// @Security token
func (ct *AddressBook) Delete(c *gin.Context) {
	f := &admin.AddressBookForm{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	id := f.RowId
	errList := global.Validator.ValidVar(c, id, "required,gt=0")
	if len(errList) > 0 {
		response.Fail(c, 101, errList[0])
		return
	}
	t := service.AllService.AddressBookService.InfoByRowId(f.RowId)
	if t.RowId == 0 || (f.Id != "" && (t.Id != f.Id || t.UserId != f.UserId || t.CollectionId != f.CollectionId)) {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	err := service.AllService.AddressBookService.Delete(t)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	if err == nil {
		response.Success(c, nil)
		return
	}
	response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed")+err.Error())
}

func (ct *AddressBook) BatchCreateFromPeers(c *gin.Context) {
	f := &admin.BatchCreateFromPeersForm{}
	if err := c.ShouldBindJSON(f); err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}

	if f.UserId == 0 || len(f.PeerIds) == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ParamsError"))
		return
	}
	if service.AllService.UserService.InfoById(f.UserId).Id == 0 {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}

	if f.CollectionId != 0 {
		collection := service.AllService.AddressBookService.CollectionInfoById(f.CollectionId)
		if collection.Id == 0 {
			response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
			return
		}
		if collection.UserId != f.UserId {
			response.Fail(c, 101, response.TranslateMsg(c, "NoAccess"))
			return
		}
	}

	pl := int64(len(f.PeerIds))
	peers := service.AllService.PeerService.List(1, uint(pl), func(tx *gorm.DB) {
		tx.Where("row_id in ?", f.PeerIds)
	})
	if pl != peers.Total || len(peers.Peers) != len(f.PeerIds) {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}

	err := service.AllService.AddressBookService.BatchCreateFromPeers(peers.Peers, f.UserId, f.CollectionId, f.Tags)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, 101, response.TranslateMsg(c, "ItemNotFound"))
		return
	}
	if errors.Is(err, service.ErrDuplicatePeerID) || errors.Is(err, service.ErrUnsupportedAddressBookPeerID) ||
		errors.Is(err, service.ErrInvalidAddressBookTags) {
		response.Fail(c, 101, response.TranslateMsg(c, err.Error()))
		return
	}
	if err != nil {
		response.Fail(c, 101, response.TranslateMsg(c, "OperationFailed")+err.Error())
		return
	}
	response.Success(c, nil)
}
