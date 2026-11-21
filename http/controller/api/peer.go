package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	requstform "github.com/lejianwen/rustdesk-api/v2/http/request/api"
	"github.com/lejianwen/rustdesk-api/v2/http/response"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/service"
)

type Peer struct {
}

func (p *Peer) DeviceCredential(c *gin.Context) {
	var input struct {
		Id   model.DeviceIdentity `json:"id"`
		Uuid model.DeviceIdentity `json:"uuid"`
		service.DeviceCredential
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Id == "" || input.Uuid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "complete device identity is required"})
		return
	}
	token, _ := c.Get("token")
	var err error
	if c.Request.Method == http.MethodDelete {
		err = service.AllService.PeerService.ClearDeviceCredential(token.(string), input.Id, input.Uuid)
	} else {
		err = service.AllService.PeerService.StoreDeviceCredential(token.(string), input.Id, input.Uuid, input.DeviceCredential)
	}
	if err != nil {
		deviceCredentialError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (p *Peer) ResolveDeviceCredential(c *gin.Context) {
	var input struct {
		Id      model.DeviceIdentity `json:"id"`
		Salt    string               `json:"salt"`
		PeerKey string               `json:"peer_key"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device credential request"})
		return
	}
	token, _ := c.Get("token")
	credential, err := service.AllService.PeerService.ResolveDeviceCredential(token.(string), input.Id, input.Salt, input.PeerKey)
	if err != nil {
		deviceCredentialError(c, err)
		return
	}
	c.JSON(http.StatusOK, credential)
}

func deviceCredentialError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "could not access device credential"
	switch {
	case errors.Is(err, service.ErrDeviceCredentialDisabled):
		status, message = http.StatusServiceUnavailable, err.Error()
	case errors.Is(err, service.ErrDeviceCredentialForbidden):
		status, message = http.StatusForbidden, err.Error()
	case errors.Is(err, service.ErrDeviceCredentialNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrDeviceCredentialInvalid):
		status, message = http.StatusBadRequest, err.Error()
	}
	c.JSON(status, gin.H{"error": message})
}

// SysInfo
// @Tags System
// @Summary 提交系统信息
// @Description 设备 ID 和 UUID 均必填，每项最多 256 字节。
// @Accept  json
// @Produce  json
// @Param body body requstform.PeerForm true "系统信息表单"
// @Success 200 {string} string "SYSINFO_UPDATED,ID_NOT_FOUND"
// @Failure 500 {object} response.ErrorResponse
// @Router /sysinfo [post]
func (p *Peer) SysInfo(c *gin.Context) {
	f := &requstform.PeerForm{}
	err := c.ShouldBindBodyWith(f, binding.JSON)
	if err != nil {
		response.Error(c, response.TranslateMsg(c, "ParamsError")+err.Error())
		return
	}
	peer := f.ToPeer()
	if !peer.HasIdentity() {
		response.Error(c, response.TranslateMsg(c, "ParamsError"))
		return
	}
	if err := service.AllService.PeerService.ReportSystemInfo(peer); err != nil {
		response.Error(c, response.TranslateMsg(c, "OperationFailed")+err.Error())
		return
	}
	//SYSINFO_UPDATED 上传成功
	//ID_NOT_FOUND 下次心跳会上传
	//直接响应文本
	c.String(http.StatusOK, "SYSINFO_UPDATED")
}

// SysInfoVer
// @Tags System
// @Summary 获取系统版本信息
// @Description 获取系统版本信息
// @Accept  json
// @Produce  json
// @Success 200 {string} string ""
// @Failure 500 {object} response.ErrorResponse
// @Router /sysinfo_ver [post]
func (p *Peer) SysInfoVer(c *gin.Context) {
	//读取resources/version文件
	v := service.AllService.AppService.GetAppVersion()
	// 加上启动时间，方便client上传信息
	v = fmt.Sprintf("%s\n%s", v, service.AllService.AppService.GetStartTime())
	c.String(http.StatusOK, v)
}
