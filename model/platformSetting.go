package model

// PlatformSetting stores administrator-controlled platform switches.
type PlatformSetting struct {
	Id              uint `gorm:"primaryKey;comment:平台设置主键" json:"id"`
	RegisterEnabled bool `gorm:"not null;comment:是否允许邮箱验证码注册" json:"register"`
	TimeModel
}
