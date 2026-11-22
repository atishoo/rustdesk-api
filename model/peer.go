package model

import (
	"fmt"

	"github.com/lejianwen/rustdesk-api/v2/model/custom_types"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// DeviceIdentity 设备标识在 MySQL 中按原始字节比较。
type DeviceIdentity string

// 设备 ID 最长 253 字节，客户端以 Base64 上报机器标识。每项预留 256 字节，
// 使组合索引在 MySQL 的 4 KiB InnoDB 页上仍可使用完整的两个值。
const deviceIdentityMaxBytes = 256

func (DeviceIdentity) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return fmt.Sprintf("varbinary(%d)", deviceIdentityMaxBytes)
	}
	return ""
}

func (id *DeviceIdentity) UnmarshalText(text []byte) error {
	if len(text) > deviceIdentityMaxBytes {
		return fmt.Errorf("device ID or UUID exceeds %d bytes", deviceIdentityMaxBytes)
	}
	*id = DeviceIdentity(text)
	return nil
}

type Peer struct {
	RowId             uint                  `json:"row_id" gorm:"primaryKey;"`
	Id                DeviceIdentity        `json:"id"  gorm:"default:'';not null;index;uniqueIndex:idx_peer_identity,priority:1;comment:设备 ID"`
	Cpu               string                `json:"cpu"  gorm:"default:'';not null;"`
	Hostname          string                `json:"hostname"  gorm:"default:'';not null;"`
	Memory            string                `json:"memory"  gorm:"default:'';not null;"`
	Os                string                `json:"os"  gorm:"default:'';not null;"`
	Username          string                `json:"username"  gorm:"default:'';not null;"`
	Uuid              DeviceIdentity        `json:"uuid"  gorm:"default:'';not null;index;uniqueIndex:idx_peer_identity,priority:2;comment:设备 UUID"`
	Version           string                `json:"version"  gorm:"default:'';not null;"`
	UserId            uint                  `json:"user_id"  gorm:"default:0;not null;index"`
	User              *User                 `json:"user,omitempty"`
	LastOnlineTime    int64                 `json:"last_online_time"  gorm:"default:0;not null;"`
	LastOnlineIp      string                `json:"last_online_ip"  gorm:"default:'';not null;"`
	GroupId           *uint                 `json:"group_id"  gorm:"default:0;not null;index"`
	Alias             *string               `json:"alias" gorm:"default:'';not null;index"`
	Tags              custom_types.AutoJson `json:"tags" gorm:"not null;default:'[]';comment:设备个性化标签" swaggertype:"array,string"`
	IdentityConfirmed bool                  `json:"-" gorm:"default:false;not null;comment:设备标识曾由认证登录关联或由管理员明确登记"`
	AmbiguousID       *bool                 `json:"ambiguous_id,omitempty" gorm:"->;-:migration"` // 仅凭 ID 无法定位到此设备，仅在设备列表返回
	DirectTarget      bool                  `json:"direct_target,omitempty" gorm:"-"`             // 裸 IP 或域名端口直连目标，仅在控制台列表返回。
	TimeModel
}

func (p *Peer) HasIdentity() bool {
	return p.Id != "" && p.Uuid != ""
}

type PeerList struct {
	Peers []*Peer `json:"list"`
	Pagination
}
