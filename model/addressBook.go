package model

import (
	"fmt"

	"github.com/lejianwen/rustdesk-api/v2/model/custom_types"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// AddressBookTarget can include a server and its key in addition to a device ID.
type AddressBookTarget string

const addressBookTargetMaxBytes = 1024

func (AddressBookTarget) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return fmt.Sprintf("varbinary(%d)", addressBookTargetMaxBytes)
	}
	return ""
}

func (id *AddressBookTarget) UnmarshalText(text []byte) error {
	if len(text) > addressBookTargetMaxBytes {
		return fmt.Errorf("address book target exceeds %d bytes", addressBookTargetMaxBytes)
	}
	*id = AddressBookTarget(text)
	return nil
}

// final String id;
// String hash; // personal ab hash password
// String password; // shared ab password
// String username; // pc username
// String hostname;
// String platform;
// String alias;
// List<dynamic> tags;
// bool forceAlwaysRelay = false;
// String rdpPort;
// String rdpUsername;
// bool online = false;
// String loginName; //login username
// bool? sameServer;

// AddressBook 有些字段是Personal才会上传的
type AddressBook struct {
	RowId            uint                   `gorm:"primaryKey" json:"row_id"`
	Id               AddressBookTarget      `json:"id" gorm:"default:0;not null;index:,length:256;comment:地址簿中的完整连接目标"`
	Username         string                 `json:"username" gorm:"default:'';not null;"`
	Password         string                 `json:"password" gorm:"default:'';not null;"`
	Hostname         string                 `json:"hostname" gorm:"default:'';not null;"`
	Alias            string                 `json:"alias" gorm:"default:'';not null;"`
	Platform         string                 `json:"platform" gorm:"default:'';not null;"`
	Tags             custom_types.AutoJson  `json:"tags" gorm:"not null;" swaggertype:"array,string"`
	Hash             string                 `json:"hash" gorm:"default:'';not null;"`
	UserId           uint                   `json:"user_id" gorm:"default:0;not null;index"`
	ForceAlwaysRelay bool                   `json:"forceAlwaysRelay" gorm:"default:0;not null;"`
	RdpPort          string                 `json:"rdpPort" gorm:"default:'';not null;"`
	RdpUsername      string                 `json:"rdpUsername" gorm:"default:'';not null;"`
	Online           bool                   `json:"online" gorm:"default:0;not null;"`
	LoginName        string                 `json:"loginName" gorm:"default:'';not null;"`
	SameServer       bool                   `json:"sameServer" gorm:"default:0;not null;"`
	CollectionId     uint                   `json:"collection_id" gorm:"default:0;not null;index"`
	Collection       *AddressBookCollection `json:"collection,omitempty"`
	AmbiguousID      *bool                  `json:"ambiguous_id,omitempty" gorm:"->;-:migration"` // 优先使用经登录关联或管理员登记的设备。实际目标仍有多个候选时为 true。
	DirectTarget     bool                   `json:"direct_target,omitempty" gorm:"-"`             // 裸 IP 或域名端口直连目标，仅在控制台列表返回。
	TimeModel
}

type AddressBookList struct {
	AddressBooks []*AddressBook `json:"list"`
	Pagination
}

type AddressBookCollection struct {
	IdModel
	UserId uint   `json:"user_id" gorm:"default:0;not null;index"`
	Name   string `json:"name" gorm:"default:'';not null;" validate:"required"`
	TimeModel
}
type AddressBookCollectionList struct {
	AddressBookCollection []*AddressBookCollection `json:"list"`
	Pagination
}
