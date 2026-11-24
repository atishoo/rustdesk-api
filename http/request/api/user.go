package api

import "github.com/lejianwen/rustdesk-api/v2/model"

type DeviceInfoInLogin struct {
	Name string `json:"name" label:"name"`
	Os   string `json:"os" label:"os"`
	Type string `json:"type" label:"type"`
}

type LoginForm struct {
	AutoLogin  bool                 `json:"autoLogin" label:"自动登录"`
	DeviceInfo DeviceInfoInLogin    `json:"deviceInfo" label:"设备信息"`
	Id         model.DeviceIdentity `json:"id"  label:"id"`
	Type       string               `json:"type"  label:"type"`
	Uuid       model.DeviceIdentity `json:"uuid"  label:"uuid"`
	model.NativeLoginCredentials
}

type UserListQuery struct {
	Page       uint   `json:"page" form:"page" validate:"required" label:"页码"`
	PageSize   uint   `json:"pageSize" form:"pageSize" validate:"required" label:"每页数量"`
	Status     int    `json:"status" form:"status" label:"状态"`
	Accessible string `json:"accessible" form:"accessible"`
}

type PeerListQuery struct {
	Page       uint   `json:"page" form:"page" validate:"required" label:"页码"`
	Current    uint   `form:"current"`
	PageSize   uint   `json:"pageSize" form:"pageSize" validate:"required" label:"每页数量"`
	Status     int    `json:"status" form:"status" label:"状态"`
	Accessible string `json:"accessible" form:"accessible"`
}
