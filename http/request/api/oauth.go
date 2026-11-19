package api

import "github.com/lejianwen/rustdesk-api/v2/model"

type OidcAuthRequest struct {
	DeviceInfo  DeviceInfoInLogin    `json:"deviceInfo" label:"设备信息"`
	Id          model.DeviceIdentity `json:"id"  label:"id"`
	Op          string               `json:"op" label:"op"`
	Uuid        model.DeviceIdentity `json:"uuid"  label:"uuid"`
	RedirectURL string               `json:"redirect_url" label:"控制台地址"`
}

type OidcAuthQuery struct {
	Code   string `json:"code" form:"code" label:"code"`
	Ticket string `json:"ticket" form:"-" label:"ticket"`
	Id     string `json:"id" form:"id" label:"id"`
	Uuid   string `json:"uuid" form:"uuid" label:"uuid"`
}
