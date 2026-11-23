package service

import (
	"errors"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
)

var (
	ErrDefaultUserGroup = errors.New("DefaultUserGroupCannotDelete")
	ErrUserGroupInUse   = errors.New("UserGroupInUse")
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
	result := DB.Where("NOT EXISTS (?)", users).Delete(u)
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
