package model

import (
	"github.com/lejianwen/rustdesk-api/v2/lib/ipregion"
	"gorm.io/gorm"
)

type LoginLog struct {
	IdModel
	UserId      uint           `json:"user_id" gorm:"default:0;not null;"`
	Client      string         `json:"client"` //webadmin,webclient,app,
	DeviceId    DeviceIdentity `json:"device_id" gorm:"index:idx_login_log_device,priority:1;comment:登录时的设备 ID"`
	Uuid        DeviceIdentity `json:"uuid" gorm:"index:idx_login_log_device,priority:2;comment:登录时的设备 UUID"`
	Ip          string         `json:"ip"`
	IpLocation  string         `json:"ip_location" gorm:"default:'';not null;comment:登录来源IP写入时的归属地"` // 登录来源 IP 写入时的归属地。
	Type        string         `json:"type"`                                                         //account,oauth
	Platform    string         `json:"platform"`                                                     //windows,linux,mac,android,ios
	UserTokenId uint           `json:"user_token_id" gorm:"default:0;not null;"`
	IsDeleted   uint           `json:"is_deleted" gorm:"default:0;not null;"`
	TimeModel
}

func (l *LoginLog) BeforeCreate(_ *gorm.DB) error {
	l.IpLocation = ipregion.Lookup(l.Ip)
	return nil
}

const (
	LoginLogClientWebAdmin = "webadmin"
	LoginLogClientWeb      = "webclient"
	LoginLogClientApp      = "app"
)

const (
	LoginLogTypeAccount = "account"
	LoginLogTypeOauth   = "oauth"
)

const (
	IsDeletedNo  = 0
	IsDeletedYes = 1
)

type LoginLogList struct {
	LoginLogs []*LoginLog `json:"list"`
	Pagination
}
