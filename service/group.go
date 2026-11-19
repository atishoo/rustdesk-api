package service

import (
	"errors"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDefaultUserGroup    = errors.New("DefaultUserGroupCannotDelete")
	ErrUserGroupInUse      = errors.New("UserGroupInUse")
	errDeviceGroupNotFound = errors.New("device group not found")
)

type GroupService struct {
}

// InfoById 根据用户id取用户信息
func (us *GroupService) InfoById(id uint) *model.Group {
	u := &model.Group{}
	DB.Where("id = ?", id).First(u)
	return u
}

func (us *GroupService) List(page, pageSize uint, where func(tx *gorm.DB)) (res *model.GroupList) {
	res = &model.GroupList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.Group{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.Groups)
	return
}

// Create 创建
func (us *GroupService) Create(u *model.Group) error {
	res := DB.Create(u).Error
	return res
}
func (us *GroupService) Delete(u *model.Group) error {
	if u.Id == model.DefaultUserGroupID {
		return ErrDefaultUserGroup
	}
	users := DB.Model(&model.User{}).Select("1").Where("group_id = ?", u.Id)
	rules := DB.Model(&model.AddressBookCollectionRule{}).Select("1").
		Where("type = ? AND to_id = ?", model.ShareAddressBookRuleTypeGroup, u.Id)
	result := DB.Where("NOT EXISTS (?) AND NOT EXISTS (?)", users, rules).Delete(u)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserGroupInUse
	}
	return nil
}

// Update 更新
func (us *GroupService) Update(u *model.Group) error {
	return DB.Model(u).Updates(u).Error
}

// DeviceGroupInfoById 根据用户id取用户信息
func (us *GroupService) DeviceGroupInfoById(id uint) *model.DeviceGroup {
	u := &model.DeviceGroup{}
	DB.Where("id = ?", id).First(u)
	return u
}

func (us *GroupService) DeviceGroupList(page, pageSize uint, where func(tx *gorm.DB)) (res *model.DeviceGroupList) {
	res = &model.DeviceGroupList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.DeviceGroup{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.DeviceGroups)
	return
}

func (us *GroupService) DeviceGroupCreate(u *model.DeviceGroup) error {
	res := DB.Create(u).Error
	return res
}
func (us *GroupService) DeviceGroupDelete(u *model.DeviceGroup) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDeviceGroup(tx, u.Id); err != nil {
			return err
		}
		if err := tx.Model(&model.Peer{}).Where("group_id = ?", u.Id).Update("group_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(u).Error
	})
}

func (us *GroupService) DeviceGroupUpdate(u *model.DeviceGroup) error {
	return DB.Model(u).Updates(u).Error
}

func lockDeviceGroup(tx *gorm.DB, id uint) error {
	group := tx.Where("id = ?", id)
	// SQLite needs a write before the lookup to serialize assignment and deletion.
	if tx.Dialector.Name() == "sqlite" {
		if err := group.Session(&gorm.Session{}).Model(&model.DeviceGroup{}).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
	}
	var current model.DeviceGroup
	err := group.Select("id").Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errDeviceGroupNotFound
	}
	return err
}

func withDeviceGroup(id *uint, write func(tx *gorm.DB) error) error {
	if id == nil || *id == 0 {
		return write(DB)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDeviceGroup(tx, *id); err != nil {
			return err
		}
		return write(tx)
	})
}
