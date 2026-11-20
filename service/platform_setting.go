package service

import (
	"errors"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
)

const platformSettingID = 1

func RegistrationEnabled() bool {
	enabled, err := RegistrationState()
	if err != nil {
		Logger.Error("failed to read platform settings: ", err)
		return false
	}
	return enabled
}

func RegistrationState() (bool, error) {
	var setting model.PlatformSetting
	if err := DB.First(&setting, platformSettingID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return setting.RegisterEnabled, nil
}

func SetRegistrationEnabled(enabled bool) error {
	return DB.Save(&model.PlatformSetting{Id: platformSettingID, RegisterEnabled: enabled}).Error
}
