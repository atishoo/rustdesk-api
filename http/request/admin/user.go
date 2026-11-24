package admin

import (
	"github.com/lejianwen/rustdesk-api/v2/model"
)

type UserForm struct {
	Id       uint             `json:"id"`
	Email    string           `json:"email" validate:"required,email,lte=254"`
	Nickname string           `json:"nickname" validate:"required,lte=32"`
	GroupId  uint             `json:"group_id"`
	IsAdmin  *bool            `json:"is_admin" `
	Status   model.StatusCode `json:"status" validate:"required,oneof=1 2"`
	Remark   *string          `json:"remark"`
}

func (uf *UserForm) FromUser(user *model.User) *UserForm {
	uf.Id = user.Id
	uf.Nickname = user.Nickname
	uf.Email = user.Email
	uf.GroupId = user.GroupId
	uf.IsAdmin = user.IsAdmin
	uf.Status = user.Status
	uf.Remark = &user.Remark
	return uf
}
func (uf *UserForm) ToUser() *model.User {
	user := &model.User{}
	user.Id = uf.Id
	user.Nickname = uf.Nickname
	user.Email = uf.Email
	user.GroupId = uf.GroupId
	if user.Id == 0 && user.GroupId == 0 {
		user.GroupId = model.DefaultUserGroupID
	}
	user.IsAdmin = uf.IsAdmin
	user.Status = uf.Status
	if uf.Remark != nil {
		user.Remark = *uf.Remark
	}
	return user
}

type PageQuery struct {
	Page     uint `form:"page"`
	PageSize uint `form:"page_size"`
}

type UserQuery struct {
	PageQuery
	Email    string `form:"email"`
	Nickname string `form:"nickname"`
}
type UserStatusForm struct {
	Id     uint             `json:"id" validate:"required"`
	Status model.StatusCode `json:"status" validate:"required,oneof=1 2"`
}
type UserPasswordForm struct {
	Id       uint   `json:"id" validate:"required"`
	Password string `json:"password" validate:"required,gte=4,lte=32"`
}

type ChangeCurPasswordForm struct {
	OldPassword string `json:"old_password"` // 已设置本地密码时必填
	NewPassword string `json:"new_password" validate:"required,gte=4,lte=32"`
}
type GroupUsersQuery struct {
	IsMy   int  `json:"is_my"`
	UserId uint `json:"user_id"`
}

type RegisterForm struct {
	Email           string `json:"email" validate:"required,email"`
	Password        string `json:"password" validate:"required,gte=4,lte=32"`
	ConfirmPassword string `json:"confirm_password" validate:"required,gte=4,lte=32"`
	Code            string `json:"code" validate:"required,len=6,numeric"`
}

type EmailCodeForm struct {
	Email   string `json:"email" validate:"required,email"`
	Purpose string `json:"purpose" validate:"required,oneof=register reset_password"`
}

type ResetPasswordForm struct {
	Email           string `json:"email" validate:"required,email"`
	Password        string `json:"password" validate:"required,gte=4,lte=32"`
	ConfirmPassword string `json:"confirm_password" validate:"required,gte=4,lte=32"`
	Code            string `json:"code" validate:"required,len=6,numeric"`
}

type UserTokenBatchDeleteForm struct {
	Ids []uint `json:"ids" validate:"required"`
}
