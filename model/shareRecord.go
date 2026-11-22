package model

import "time"

type ShareSource uint8

const (
	ShareSourceAddressBook ShareSource = 1
	ShareSourcePeer        ShareSource = 2
)

// ShareRecord 记录 Web Client 分享及其对应的地址簿条目或设备。
type ShareRecord struct {
	IdModel
	UserId           uint              `json:"user_id" gorm:"default:0;not null;index"`
	Source           ShareSource       `json:"-" gorm:"default:0;not null;comment:分享来源类型"`
	PeerId           AddressBookTarget `json:"peer_id" gorm:"default:'';not null;comment:分享的完整连接目标"`
	AddressBookRowId uint              `json:"address_book_row_id" gorm:"default:0;not null;index;comment:分享源的地址簿或设备记录ID"`
	ShareToken       string            `json:"-" gorm:"default:'';not null;index"`
	Password         string            `json:"-" gorm:"default:'';not null;"`
	Expire           int64             `json:"expire" gorm:"default:0;not null;"`
	StartedAt        int64             `json:"started_at" gorm:"default:0;not null;comment:首次使用分享链接的时间"`
	ExpiresAt        int64             `json:"expires_at" gorm:"-"` // Unix seconds, or zero for a permanent share.
	TimeModel
}

func (r *ShareRecord) ExpiryTime() time.Time {
	if r.Expire == 0 {
		return time.Time{}
	}
	if r.StartedAt == 0 {
		return time.Time{}
	}
	return time.Unix(r.StartedAt, 0).Add(time.Second * time.Duration(r.Expire))
}

// ShareRecordList 分享记录列表
type ShareRecordList struct {
	ShareRecords []*ShareRecord `json:"list,omitempty"`
	Pagination
}
