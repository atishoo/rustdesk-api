package model

type UserToken struct {
	IdModel
	UserId     uint           `json:"user_id" gorm:"default:0;not null;index"`
	DeviceUuid DeviceIdentity `json:"device_uuid" gorm:"default:'';index:idx_user_token_device,priority:2;comment:令牌绑定的设备 UUID"`
	DeviceId   DeviceIdentity `json:"device_id" gorm:"default:'';index:idx_user_token_device,priority:1;comment:令牌绑定的设备 ID"`
	Token      string         `json:"token" gorm:"default:'';not null;index"`
	ExpiredAt  int64          `json:"expired_at" gorm:"default:0;not null;"`
	TimeModel
}

type UserTokenList struct {
	UserTokens []UserToken `json:"list"`
	Pagination
}
