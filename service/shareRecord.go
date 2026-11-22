package service

import (
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
)

type ShareRecordService struct {
}

// InfoById 根据用户id取用户信息
func (srs *ShareRecordService) InfoById(id uint) *model.ShareRecord {
	u := &model.ShareRecord{}
	DB.Where("id = ?", id).First(u)
	return u
}

func (srs *ShareRecordService) List(page, pageSize uint, where func(tx *gorm.DB)) (res *model.ShareRecordList) {
	res = &model.ShareRecordList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.ShareRecord{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.ShareRecords)
	for _, record := range res.ShareRecords {
		if expiry := record.ExpiryTime(); !expiry.IsZero() {
			record.ExpiresAt = expiry.Unix()
		}
	}
	return
}

// StartShare begins the validity window on the first successful share redemption.
func (srs *ShareRecordService) StartShare(record *model.ShareRecord) error {
	if record.StartedAt == 0 {
		startedAt := time.Now().Unix()
		if err := DB.Model(&model.ShareRecord{}).
			Where("id = ? AND started_at = 0", record.Id).
			Update("started_at", startedAt).Error; err != nil {
			return err
		}
	}
	return DB.Where("id = ?", record.Id).First(record).Error
}

// Create 创建
func (srs *ShareRecordService) Create(u *model.ShareRecord) error {
	res := DB.Create(u).Error
	return res
}
func (srs *ShareRecordService) Delete(u *model.ShareRecord) error {
	return DB.Delete(u).Error
}

// Update 更新
func (srs *ShareRecordService) Update(u *model.ShareRecord) error {
	return DB.Model(u).Updates(u).Error
}

func (srs *ShareRecordService) BatchDelete(ids []uint) error {
	return DB.Where("id in (?)", ids).Delete(&model.ShareRecord{}).Error
}
