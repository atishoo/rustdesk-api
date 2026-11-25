package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	EmailCodePurposeRegister = "register"
	EmailCodePurposeReset    = "reset_password"
	emailCodeLifetime        = 10 * time.Minute
	emailCodeInterval        = time.Minute
)

func ValidateEmailAddress(value string) bool {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 254 {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func SendEmailCode(email, purpose string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidateEmailAddress(email) {
		return errors.New("InvalidEmail")
	}
	if purpose != EmailCodePurposeRegister && purpose != EmailCodePurposeReset {
		return errors.New("InvalidEmailCodePurpose")
	}
	if purpose == EmailCodePurposeRegister {
		enabled, err := RegistrationState()
		if err != nil {
			return errors.New("OperationFailed")
		}
		if !enabled {
			return errors.New("RegisterClosed")
		}
	}
	var latest model.EmailCode
	if err := DB.Where("email = ? AND purpose = ?", email, purpose).Order("created_at DESC").First(&latest).Error; err == nil {
		if time.Since(time.Time(latest.CreatedAt)) < emailCodeInterval {
			return errors.New("EmailCodeTooFrequent")
		}
	}
	if purpose == EmailCodePurposeRegister {
		ldapExists, err := AllService.UserService.IsEmailExistsLdap(email)
		if err != nil {
			return errors.New("OperationFailed")
		}
		if AllService.UserService.InfoByEmail(email).Id > 0 || ldapExists {
			return errors.New("EmailExists")
		}
	}
	if purpose == EmailCodePurposeReset && AllService.UserService.InfoByEmail(email).Id == 0 {
		return nil
	}

	code, err := randomEmailCode()
	if err != nil {
		return err
	}
	item := &model.EmailCode{
		Email:     email,
		Purpose:   purpose,
		CodeHash:  hashEmailCode(email, purpose, code),
		ExpiresAt: time.Now().Add(emailCodeLifetime),
	}
	if err := DB.Where("email = ? AND purpose = ? AND used = ?", email, purpose, false).Delete(&model.EmailCode{}).Error; err != nil {
		return err
	}
	if err := DB.Create(item).Error; err != nil {
		return err
	}
	if err := sendEmail(email, code); err != nil {
		DB.Delete(item)
		return err
	}
	return nil
}

func ConsumeEmailCode(email, purpose, code string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	return DB.Transaction(func(tx *gorm.DB) error {
		return consumeEmailCodeTx(tx, email, purpose, code)
	})
}

func (us *UserService) RegisterByEmail(email, password, code string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidateEmailAddress(email) {
		return nil, errors.New("InvalidEmail")
	}
	Lock.Lock("registerByEmail")
	defer Lock.UnLock("registerByEmail")
	cfg, err := GetLdapSetting()
	if err != nil {
		return nil, errors.New("OperationFailed")
	}
	ldapExists, err := AllService.LdapService.isEmailExists(&cfg, email)
	if err != nil {
		return nil, errors.New("OperationFailed")
	}
	if us.InfoByEmail(email).Id > 0 || ldapExists {
		return nil, errors.New("EmailExists")
	}

	var user *model.User
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthUpdate); err != nil {
			return err
		}
		enabled, err := registrationStateTx(tx)
		if err != nil {
			return errors.New("OperationFailed")
		}
		if !enabled {
			return errors.New("RegisterClosed")
		}
		if err := consumeEmailCodeTx(tx, email, EmailCodePurposeRegister, code); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			var existing model.User
			if err := tx.Where("email = ?", email).First(&existing).Error; err == nil {
				return errors.New("EmailExists")
			}
		}
		isAdmin := count == 0
		hashed, err := encryptPassword(password)
		if err != nil {
			return err
		}
		nickname := []rune(strings.SplitN(email, "@", 2)[0])
		user = &model.User{
			Email:    email,
			Password: hashed,
			Nickname: string(nickname[:min(len(nickname), 32)]),
			GroupId:  model.DefaultUserGroupID,
			IsAdmin:  &isAdmin,
			Status:   model.COMMON_STATUS_ENABLE,
		}
		return tx.Create(user).Error
	})
	return user, err
}

func registrationStateTx(tx *gorm.DB) (bool, error) {
	var setting model.PlatformSetting
	if err := tx.First(&setting, platformSettingID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return setting.RegisterEnabled, nil
}

func (us *UserService) ResetPasswordByEmail(email, password, code string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := consumeEmailCodeTx(tx, email, EmailCodePurposeReset, code); err != nil {
			return err
		}
		var user model.User
		if err := tx.Where("email = ?", email).First(&user).Error; err != nil {
			return errors.New("ItemNotFound")
		}
		hashed, err := encryptPassword(password)
		if err != nil {
			return err
		}
		if err := tx.Model(&user).Update("password", hashed).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", user.Id).Delete(&model.UserToken{}).Error
	})
}

func consumeEmailCodeTx(tx *gorm.DB, email, purpose, code string) error {
	var item model.EmailCode
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"email = ? AND purpose = ? AND used = ? AND expires_at > ?", email, purpose, false, time.Now(),
	).Order("created_at DESC").First(&item).Error; err != nil {
		return errors.New("EmailCodeInvalid")
	}
	if item.CodeHash != hashEmailCode(email, purpose, code) {
		return errors.New("EmailCodeInvalid")
	}
	return tx.Model(&item).Update("used", true).Error
}

func randomEmailCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	value := (uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])) % 1000000
	return fmt.Sprintf("%06d", value), nil
}

func hashEmailCode(email, purpose, code string) string {
	hash := sha256.Sum256([]byte(email + "\x00" + purpose + "\x00" + code))
	return base64.RawStdEncoding.EncodeToString(hash[:])
}

func encryptPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password is required")
	}
	return utils.EncryptPassword(password)
}

func sendEmail(to, code string) error {
	if Config.SMTP.Host == "" || Config.SMTP.User == "" || Config.SMTP.Password == "" {
		return errors.New("SMTPNotConfigured")
	}
	port := Config.SMTP.Port
	if port == 0 {
		port = 465
	}
	address := net.JoinHostPort(Config.SMTP.Host, strconv.Itoa(port))
	var client *smtp.Client
	var err error
	if strings.EqualFold(Config.SMTP.Crypto, "ssl") || port == 465 {
		conn, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", address, &tls.Config{ServerName: Config.SMTP.Host, MinVersion: tls.VersionTLS12})
		if dialErr != nil {
			return dialErr
		}
		client, err = smtp.NewClient(conn, Config.SMTP.Host)
	} else {
		client, err = smtp.Dial(address)
	}
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("AUTH"); ok {
		if err := client.Auth(smtp.PlainAuth("", Config.SMTP.User, Config.SMTP.Password, Config.SMTP.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(Config.SMTP.User); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: capylink verification code\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour capylink verification code is %s. It expires in 10 minutes.\r\n", Config.SMTP.User, to, code)
	if err := writer.Close(); err != nil {
		return err
	}
	return writeErr
}
