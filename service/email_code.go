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
	"net/textproto"
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

var passwordResetLimiter = utils.NewLoginLimiter(utils.SecurityPolicy{
	CaptchaThreshold: -1,
	BanThreshold:     1,
	AttemptsWindow:   emailCodeInterval,
	BanDuration:      emailCodeInterval,
})

var errSMTPNotConfigured = errors.New("SMTPNotConfigured")

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
			if purpose == EmailCodePurposeReset {
				return nil
			}
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
	} else if !AllService.UserService.reservePasswordResetAttempt(email) {
		return nil
	}
	fail := func(step string, err error) error {
		if purpose != EmailCodePurposeReset {
			return err
		}
		// SMTP replies and database errors can include recipient or configuration details.
		var reply *textproto.Error
		switch {
		case errors.Is(err, errSMTPNotConfigured):
			Logger.Warn("password reset code email failed: SMTP is not configured")
		case errors.As(err, &reply):
			Logger.Warnf("password reset code email failed with SMTP status %d", reply.Code)
		default:
			Logger.Warnf("password reset code: %s failed (%T)", step, err)
		}
		return nil
	}
	code, err := randomEmailCode()
	if err != nil {
		return fail("generation", err)
	}
	item := &model.EmailCode{
		Email:     email,
		Purpose:   purpose,
		CodeHash:  hashEmailCode(email, purpose, code),
		ExpiresAt: time.Now().Add(emailCodeLifetime),
	}
	if err := DB.Where("email = ? AND purpose = ? AND used = ?", email, purpose, false).Delete(&model.EmailCode{}).Error; err != nil {
		return fail("previous code cleanup", err)
	}
	if err := DB.Create(item).Error; err != nil {
		return fail("storage", err)
	}
	if err := sendEmail(email, code); err != nil {
		if cleanupErr := DB.Delete(item).Error; cleanupErr != nil {
			fail("undelivered code cleanup", cleanupErr)
		}
		return fail("email delivery", err)
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

func (us *UserService) reservePasswordResetAttempt(email string) bool {
	if !validPasswordLoginEmail(email) {
		return false
	}
	user := us.InfoByEmail(email)
	if user.Id == 0 {
		return false
	}
	cfg, err := GetLdapSetting()
	if err != nil || us.checkPasswordResetAccount(user, cfg.Enable) != nil {
		return false
	}
	if !passwordResetLimiter.ReserveAttempt(strconv.FormatUint(uint64(user.Id), 10)) {
		return false
	}
	ldapExists, err := newLdapRequest().isEmailExists(&cfg, email)
	return err == nil && !ldapExists
}

func (us *UserService) checkPasswordResetAccount(user *model.User, ldapEnabled bool) error {
	if !us.CheckUserEnable(user) {
		return errors.New("UserDisabled")
	}
	if !allowsLocalPassword(ldapEnabled, user) {
		return errors.New("LocalPasswordResetUnavailable")
	}
	return nil
}

func (us *UserService) ResetPasswordByEmail(email, password, code string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validPasswordLoginEmail(email) {
		return errors.New("InvalidEmail")
	}
	cfg, err := GetLdapSetting()
	if err != nil {
		return errors.New("OperationFailed")
	}
	var ldapExists bool
	var lookupErr error
	if cfg.Enable {
		if _, err := findValidEmailCode(DB, email, EmailCodePurposeReset, code); err != nil {
			return err
		}
		// Verify the directory before locking the configuration and account rows.
		ldapExists, lookupErr = newLdapRequest().isEmailExists(&cfg, email)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthShare); err != nil {
			return err
		}
		if err := consumeEmailCodeTx(tx, email, EmailCodePurposeReset, code); err != nil {
			return err
		}
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where("email = ?", email).First(&user).Error; err != nil {
			return errors.New("ItemNotFound")
		}
		if err := us.checkPasswordResetAccount(&user, cfg.Enable); err != nil {
			return err
		}
		if ldapExists {
			return errors.New("LocalPasswordResetUnavailable")
		}
		if lookupErr != nil {
			return ErrLdapDirectoryUnavailable
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
	item, err := findValidEmailCode(tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}), email, purpose, code)
	if err != nil {
		return err
	}
	return tx.Model(item).Update("used", true).Error
}

func findValidEmailCode(db *gorm.DB, email, purpose, code string) (*model.EmailCode, error) {
	var item model.EmailCode
	if err := db.Where(
		"email = ? AND purpose = ? AND used = ? AND expires_at > ?", email, purpose, false, time.Now(),
	).Order("created_at DESC").First(&item).Error; err != nil {
		return nil, errors.New("EmailCodeInvalid")
	}
	if item.CodeHash != hashEmailCode(email, purpose, code) {
		return nil, errors.New("EmailCodeInvalid")
	}
	return &item, nil
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
		return errSMTPNotConfigured
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
