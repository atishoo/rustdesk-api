package model

import "time"

// ShareRecord 记录 Web Client 分享及其对应的地址簿条目。
type ShareRecord struct {
	IdModel
	UserId           uint              `json:"user_id" gorm:"default:0;not null;index"`
	PeerId           AddressBookTarget `json:"peer_id" gorm:"default:'';not null;comment:分享的完整连接目标"`
	AddressBookRowId uint              `json:"address_book_row_id" gorm:"default:0;not null;index;comment:分享的地址簿记录ID"`
	ShareToken       string            `json:"share_token" gorm:"default:'';not null;index"`
	PasswordType     string            `json:"password_type" gorm:"default:'';not null;"`
	Password         string            `json:"password" gorm:"default:'';not null;"`
	Expire           int64             `json:"expire" gorm:"default:0;not null;"`
	ExpiresAt        int64             `json:"expires_at" gorm:"-"` // Unix seconds, or zero for a permanent share.
	TimeModel
}

func (r *ShareRecord) ExpiryTime() time.Time {
	if r.Expire == 0 {
		return time.Time{}
	}
	return time.Time(r.CreatedAt).Add(time.Second * time.Duration(r.Expire))
}

// ShareRecordList 分享记录列表
type ShareRecordList struct {
	ShareRecords []*ShareRecord `json:"list,omitempty"`
	Pagination
}
