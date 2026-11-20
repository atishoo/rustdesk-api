package model

import "time"

// EmailCode stores a one-time code used for email registration and password reset.
type EmailCode struct {
	IdModel
	Email     string    `json:"email" gorm:"not null;index;comment:验证码目标邮箱"`
	Purpose   string    `json:"purpose" gorm:"not null;index;comment:验证码用途"`
	CodeHash  string    `json:"-" gorm:"not null;comment:验证码哈希"`
	ExpiresAt time.Time `json:"-" gorm:"not null;comment:验证码过期时间"`
	Used      bool      `json:"-" gorm:"not null;default:false;comment:验证码是否已使用"`
	TimeModel
}
