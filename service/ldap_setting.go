package service

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/go-playground/validator/v10"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var passwordLoginValidator = validator.New()

func validPasswordLoginEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	return ValidateEmailAddress(email) &&
		passwordLoginValidator.StructPartial(model.NativeLoginCredentials{Email: email}, "Email") == nil
}

var (
	ErrLdapSettingInvalid          = errors.New("LdapSettingInvalid")
	ErrLdapBindingPasswordRequired = errors.New("LdapBindingPasswordRequired")
	ErrLdapEnableAttrPairRequired  = errors.New("LdapEnableAttrPairRequired")
	ErrLdapSettingsTableMissing    = errors.New("LdapSettingsTableMissing")
	ErrLdapLocalAdminRequired      = errors.New("LdapLocalAdminRequired")
	ErrLdapLastLocalAdmin          = errors.New("LdapLastLocalAdmin")
	ErrLdapSettingsChanged         = errors.New("LdapSettingsChanged")
	ErrLdapDirectoryUnavailable    = errors.New("LdapDirectoryUnavailable")
)

func GetLdapSetting() (model.LdapSetting, error) {
	return readLdapSetting(DB)
}

func readLdapSetting(db *gorm.DB) (model.LdapSetting, error) {
	var setting model.LdapSetting
	err := db.First(&setting, model.LdapSettingID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrLdapSettingsTableMissing
	}
	return setting, err
}

// Check the snapshot while holding the settings row until the write commits.
func lockLdapSetting(tx *gorm.DB, strength string) (model.LdapSetting, error) {
	if tx.Dialector.Name() == "sqlite" {
		// SQLite has no row locks. Reserve its write lock before taking the read snapshot.
		if err := tx.Model(&model.LdapSetting{}).Where("1 = 0").UpdateColumn("revision", gorm.Expr("revision")).Error; err != nil {
			return model.LdapSetting{}, err
		}
	}
	return readLdapSetting(tx.Clauses(clause.Locking{Strength: strength}))
}

func lockLdapRevision(tx *gorm.DB, revision uint64, strength string) error {
	current, err := lockLdapSetting(tx, strength)
	if err != nil {
		return err
	}
	if current.Revision != revision {
		return ErrLdapSettingsChanged
	}
	return nil
}

func SaveLdapSetting(setting model.LdapSetting, bindPassword *string, admin *model.User, adminPassword string) error {
	setting.Id = model.LdapSettingID
	setting.Url = strings.TrimSpace(setting.Url)
	setting.BindDn = strings.TrimSpace(setting.BindDn)
	setting.User.EnableAttr = strings.TrimSpace(setting.User.EnableAttr)
	if setting.User.UsesADAccountControl() {
		setting.User.EnableAttrValue = ""
	} else if (setting.User.EnableAttr == "") != (strings.TrimSpace(setting.User.EnableAttrValue) == "") {
		return ErrLdapEnableAttrPairRequired
	} else if setting.User.EnableAttr == "" {
		setting.User.EnableAttrValue = ""
	}
	if setting.Url != "" || setting.Enable {
		address, err := url.Parse(setting.Url)
		if err != nil || !validLdapAddress(address) {
			return ErrLdapSettingInvalid
		}
	}
	if setting.User.Filter != "" {
		if _, err := ldap.CompileFilter(setting.User.Filter); err != nil {
			return ErrLdapSettingInvalid
		}
	}

	current, err := GetLdapSetting()
	if err != nil {
		return err
	}
	if current.Revision != setting.Revision {
		return ErrLdapSettingsChanged
	}
	if bindPassword != nil {
		setting.BindPassword = *bindPassword
	} else if current.BindPassword != "" {
		if setting.Url != current.Url || setting.BindDn != current.BindDn {
			return ErrLdapBindingPasswordRequired
		}
		setting.BindPassword = current.BindPassword
	}
	if setting.Enable && (setting.BindDn == "" || setting.BindPassword == "") {
		return ErrLdapBindingPasswordRequired
	}
	verifyAdmin := setting.Enable && setting != current
	if verifyAdmin {
		if admin == nil || !validPasswordLoginEmail(admin.Email) || admin.Id == 0 || admin.IsAdmin == nil || !*admin.IsAdmin ||
			admin.Status != model.COMMON_STATUS_ENABLE || admin.LdapSynced || admin.Password == "" || adminPassword == "" {
			return ErrLdapLocalAdminRequired
		}
		valid, _, err := utils.VerifyPassword(admin.Password, adminPassword)
		if err != nil || !valid {
			return ErrLdapLocalAdminRequired
		}
		directory := newLdapRequest()
		matches, err := directory.emailSearchResult(&setting, admin.Email)
		if err != nil {
			return ErrLdapDirectoryUnavailable
		}
		if len(matches.Entries) != 0 {
			return ErrLdapLocalAdminRequired
		}
	}

	if setting == current {
		return nil
	}
	setting.Revision++
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, current.Revision, clause.LockingStrengthUpdate); err != nil {
			return err
		}
		if verifyAdmin {
			var verified model.User
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").
				Where("id = ? AND email = ? AND password = ? AND is_admin = ? AND ldap_synced = ? AND status = ?",
					admin.Id, admin.Email, admin.Password, true, false, model.COMMON_STATUS_ENABLE).
				First(&verified).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLdapLocalAdminRequired
			}
			if err != nil {
				return err
			}
		}
		result := tx.Model(&setting).Where("revision = ?", current.Revision).Select("*").Updates(&setting)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLdapSettingsChanged
		}
		return nil
	})
}

func validLdapAddress(address *url.URL) bool {
	if address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" || address.Opaque != "" {
		return false
	}
	switch address.Scheme {
	case "ldapi":
		return address.Host == ""
	case "ldap", "ldaps":
		if port := address.Port(); port != "" {
			if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
				return false
			}
		}
		return address.Hostname() != "" && (address.Path == "" || address.Path == "/")
	default:
		return false
	}
}
