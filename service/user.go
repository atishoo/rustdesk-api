package service

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/lejianwen/rustdesk-api/v2/lib/upload"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserService struct {
}

type PasswordLogin struct {
	settings *model.LdapSetting
	ldapUser *LdapUser
}

// InfoById 根据用户id取用户信息
func (us *UserService) InfoById(id uint) *model.User {
	u := &model.User{}
	DB.Where("id = ?", id).First(u)
	return u
}

// InfoByEmail 根据邮箱取用户信息
func (us *UserService) InfoByEmail(email string) *model.User {
	u := &model.User{}
	DB.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(u)
	return u
}

// InfoByOpenid 根据openid取用户信息
func (us *UserService) InfoByOpenid(openid string) *model.User {
	u := &model.User{}
	DB.Where("openid = ?", openid).First(u)
	return u
}

// InfoByEmailPassword verifies the credentials and prepares the user for Login.
func (us *UserService) InfoByEmailPassword(email, password string) (*model.User, *PasswordLogin) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidateEmailAddress(email) || password == "" {
		return nil, nil
	}
	cfg, err := GetLdapSetting()
	if err != nil {
		Logger.Errorf("LDAP settings lookup failed, %v", err)
		return nil, nil
	}
	if cfg.Enable {
		directory := newLdapRequest()
		ldapUser, err := directory.loginUser(&cfg, email, password)
		if err == nil {
			user, err := directory.prepareLdapUser(&cfg, ldapUser)
			if err != nil {
				Logger.Errorf("LDAP user preparation failed, %v", err)
				return nil, nil
			}
			if !cfg.User.Sync {
				if err := revokeLdapAccess(&cfg, ldapUser); err != nil {
					Logger.Errorf("LDAP revocation failed, %v", err)
					return nil, nil
				}
			}
			return user, &PasswordLogin{settings: &cfg, ldapUser: ldapUser}
		}
		Logger.Errorf("LDAP authentication failed, %v", err)
		// Keep an independent local administrator usable when the directory is
		// unavailable; explicit LDAP credential or policy failures must not fall
		// back to a local password.
		if !ldapAllowsLocalAdminFallback(err) {
			return nil, nil
		}
	}
	u := &model.User{}
	DB.Where("email = ?", email).First(u)
	if u.Id == 0 {
		return nil, nil
	}
	if cfg.Enable && (u.LdapSynced || u.IsAdmin == nil || !*u.IsAdmin) {
		return nil, nil
	}
	ok, newHash, err := utils.VerifyPassword(u.Password, password)
	if err != nil || !ok {
		return nil, nil
	}
	if newHash != "" {
		DB.Model(u).Update("password", newHash)
		u.Password = newHash
	}
	return u, &PasswordLogin{settings: &cfg}
}

// InfoByAccesstoken 根据accesstoken取用户信息
func (us *UserService) InfoByAccessToken(token string) (*model.User, *model.UserToken) {
	u := &model.User{}
	ut := &model.UserToken{}
	var userID uint
	if len(Jwt.Key) > 0 {
		verifiedID, err := us.VerifyJWT(token)
		if err != nil || verifiedID == 0 {
			return u, ut
		}
		userID = verifiedID
	}
	DB.Where("token = ?", token).First(ut)
	if ut.Id == 0 || ut.ExpiredAt < time.Now().Unix() || (userID != 0 && userID != ut.UserId) {
		return u, ut
	}
	DB.Where("id = ?", ut.UserId).First(u)
	return u, ut
}

// GenerateToken 生成token
func (us *UserService) GenerateToken(u *model.User) (string, int64) {
	expiresAt := us.UserTokenExpireTimestamp()
	if len(Jwt.Key) > 0 {
		return Jwt.GenerateTokenBefore(u.Id, time.Unix(expiresAt, 0))
	}
	return utils.Md5(u.Email + time.Now().String()), expiresAt
}

// Login 登录
func (us *UserService) Login(u *model.User, llog *model.LoginLog, deviceSession bool, password *PasswordLogin) (*model.UserToken, error) {
	var session model.UserToken
	details := *llog
	if deviceSession {
		session.DeviceId = details.DeviceId
		session.DeviceUuid = details.Uuid
		if details.Client == model.LoginLogClientWebAdmin {
			details.Client = model.LoginLogClientApp
		}
	}
	var settings *model.LdapSetting
	var directoryUser *LdapUser
	if password != nil {
		settings, directoryUser = password.settings, password.ldapUser
	}
	retryContention := directoryUser != nil || (session.DeviceId != "" && session.DeviceUuid != "")
	for attempt := 1; ; attempt++ {
		ut, loginLog := session, details
		loginUser := u
		err := withLdapRecheck(settings, directoryUser, u, func(checked *ldapRecheckRequired) error {
			return DB.Transaction(func(tx *gorm.DB) error {
				if password != nil {
					if err := lockLdapRevision(tx, password.settings.Revision, clause.LockingStrengthShare); err != nil {
						return err
					}
					if password.ldapUser != nil {
						var err error
						loginUser, err = mapLdapUser(tx, password.settings, u, checked)
						if err != nil {
							return errors.Join(ErrLdapToLocalUserFailed, err)
						}
						if !us.CheckUserEnable(loginUser) {
							return ErrLdapUserDisabled
						}
					}
				}
				ut.UserId = loginUser.Id
				ut.Token, ut.ExpiredAt = us.GenerateToken(loginUser)
				if ut.Token == "" || ut.ExpiredAt <= time.Now().Unix() {
					return errors.New("could not generate a usable session token")
				}
				// Match the device-before-session lock order used by reporting and logout.
				if ut.DeviceId != "" || ut.DeviceUuid != "" {
					if err := AllService.PeerService.bindDeviceUser(tx, ut.DeviceId, ut.DeviceUuid, loginUser.Id); err != nil {
						return err
					}
				}
				if err := tx.Create(&ut).Error; err != nil {
					return err
				}
				loginLog.UserId = loginUser.Id
				loginLog.UserTokenId = ut.Id
				return tx.Create(&loginLog).Error
			})
		})
		if err == nil {
			if loginUser != u {
				*u = *loginUser
			}
			*llog = loginLog
			return &ut, nil
		}
		// Device claims and LDAP account creation can contend on unique index gaps.
		if attempt == 3 || !retryContention || !isMySQLDeadlock(err) {
			return nil, err
		}
	}
}

func isMySQLDeadlock(err error) bool {
	var deadlock *mysql.MySQLError
	return errors.As(err, &deadlock) && deadlock.Number == 1213
}

// CurUser 获取当前用户
func (us *UserService) CurUser(c *gin.Context) *model.User {
	user, _ := c.Get("curUser")
	u, ok := user.(*model.User)
	if !ok {
		return nil
	}
	return u
}

func (us *UserService) List(page, pageSize uint, where func(tx *gorm.DB)) (res *model.UserList) {
	res = &model.UserList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.User{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.Users)
	return
}

func (us *UserService) ListByIds(ids []uint) (res []*model.User) {
	DB.Where("id in ?", ids).Find(&res)
	return res
}

// ListByGroupId 根据组id取用户列表
func (us *UserService) ListByGroupId(groupId, page, pageSize uint) (res *model.UserList) {
	res = us.List(page, pageSize, func(tx *gorm.DB) {
		tx.Where("group_id = ?", groupId)
	})
	return
}

// ListIdsByGroupId 根据组id取用户id列表
func (us *UserService) ListIdsByGroupId(groupId uint) (ids []uint) {
	DB.Model(&model.User{}).Where("group_id = ?", groupId).Pluck("id", &ids)
	return ids

}

// ListIdentitiesByGroupId 根据组 ID 取用户身份及展示信息。
func (us *UserService) ListIdentitiesByGroupId(groupId uint) (res []*model.User) {
	DB.Model(&model.User{}).Where("group_id = ?", groupId).Select("id, email, nickname, avatar").Find(&res)
	return res
}

// CheckUserEnable 判断用户是否禁用
func (us *UserService) CheckUserEnable(u *model.User) bool {
	return u.Status == model.COMMON_STATUS_ENABLE
}

// Create 创建
func (us *UserService) Create(u *model.User) error {
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.Nickname = strings.TrimSpace(u.Nickname)
	if !ValidateEmailAddress(u.Email) {
		return errors.New("InvalidEmail")
	}
	if u.Nickname == "" {
		return errors.New("nickname is required")
	}
	cfg, err := GetLdapSetting()
	if err != nil {
		return err
	}
	ldapExists, err := AllService.LdapService.isEmailExists(&cfg, u.Email)
	if err != nil {
		return err
	}
	if us.InfoByEmail(u.Email).Id > 0 || ldapExists {
		return errors.New("EmailExists")
	}
	if u.Password != "" {
		u.Password, err = encryptPassword(u.Password)
		if err != nil {
			return err
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthUpdate); err != nil {
			return err
		}
		return tx.Create(u).Error
	})
}

// Logout 撤销当前令牌，并恢复相同设备上最新有效会话的归属。
func (us *UserService) Logout(u *model.User, token string) error {
	var session model.UserToken
	if err := DB.Where("user_id = ? AND token = ?", u.Id, token).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if session.DeviceId == "" && session.DeviceUuid == "" {
			return tx.Delete(&session).Error
		}
		if err := lockDeviceIdentity(tx, session.DeviceId, session.DeviceUuid); err != nil {
			return err
		}
		device := tx.Model(&model.Peer{}).
			Where("id = ? AND uuid = ?", session.DeviceId, session.DeviceUuid)
		// 先锁定设备行，使同一设备的并发登出依次检查剩余会话。
		if err := device.Session(&gorm.Session{}).UpdateColumn("user_id", gorm.Expr("user_id")).Error; err != nil {
			return err
		}
		if err := tx.Delete(&session).Error; err != nil {
			return err
		}
		sessions := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
		userID, err := us.latestDeviceSessionUser(sessions, session.DeviceId, session.DeviceUuid, 0)
		if err != nil || userID == u.Id {
			return err
		}
		return device.Where("user_id = ?", u.Id).Update("user_id", userID).Error
	})
}

// latestDeviceSessionUser finds the newest usable session for an exact pair.
// Pass zero for userID to consider sessions from every enabled account.
func (us *UserService) latestDeviceSessionUser(tx *gorm.DB, deviceID, uuid model.DeviceIdentity, userID uint) (uint, error) {
	query := tx.Model(&model.UserToken{}).
		Where("device_id = ? AND device_uuid = ? AND expired_at >= ?", deviceID, uuid, time.Now().Unix()).
		Order("id DESC")
	if userID == 0 {
		query = query.Where("EXISTS (SELECT 1 FROM users WHERE users.id = user_tokens.user_id AND users.status = ?)", model.COMMON_STATUS_ENABLE)
	} else {
		query = query.Where("user_id = ?", userID)
	}
	useJWT := len(Jwt.Key) > 0
	if useJWT {
		query = query.Select("user_id", "token")
	} else {
		query = query.Select("user_id").Limit(1)
	}
	var sessions []model.UserToken
	if err := query.Find(&sessions).Error; err != nil {
		return 0, err
	}
	for _, session := range sessions {
		if !useJWT {
			return session.UserId, nil
		}
		if uid, err := us.VerifyJWT(session.Token); err == nil && uid == session.UserId {
			return session.UserId, nil
		}
	}
	return 0, nil
}

// Delete 删除用户和oauth信息
func (us *UserService) Delete(u *model.User) error {
	err := withAdminRemoval(u.Id, nil, "The last admin user cannot be deleted", func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "avatar").First(u, u.Id).Error; err != nil {
			return err
		}
		if err := tx.Delete(u).Error; err != nil {
			return err
		}
		for _, related := range []any{&model.UserThird{}, &model.AddressBook{}, &model.AddressBookCollection{}} {
			if err := tx.Where("user_id = ?", u.Id).Delete(related).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := upload.RemoveAvatar(u.Avatar); err != nil {
		Logger.Warn("User deleted successfully, but failed to remove avatar.")
	}
	// 删除关联的peer
	if err := AllService.PeerService.EraseUserId(u.Id); err != nil {
		Logger.Warn("User deleted successfully, but failed to unlink peer.")
		return nil
	}
	return nil
}

// Update 更新
func (us *UserService) Update(u *model.User, fields ...string) error {
	var previous *model.User
	var ldapSettings *model.LdapSetting
	for _, field := range fields {
		switch field {
		case "email":
			u.Email = strings.ToLower(strings.TrimSpace(u.Email))
			if !ValidateEmailAddress(u.Email) {
				return errors.New("InvalidEmail")
			}
			previous = us.InfoById(u.Id)
			cfg, err := GetLdapSetting()
			if err != nil {
				return err
			}
			ldapSettings = &cfg
			if previous.Id > 0 && !strings.EqualFold(previous.Email, u.Email) {
				if previous.LdapSynced {
					return errors.New("LdapSyncedEmailCannotChange")
				}
				ldapExists, err := AllService.LdapService.isEmailExists(&cfg, previous.Email)
				if err != nil {
					return err
				}
				if ldapExists {
					return errors.New("LdapSyncedEmailCannotChange")
				}
				ldapExists, err = AllService.LdapService.isEmailExists(&cfg, u.Email)
				if err != nil {
					return err
				}
				if ldapExists {
					return errors.New("EmailExists")
				}
			}
		case "nickname":
			u.Nickname = strings.TrimSpace(u.Nickname)
			if u.Nickname == "" {
				return errors.New("nickname is required")
			}
		}
	}
	update := func(tx *gorm.DB) error {
		var current model.User
		query := tx.Select("id")
		if previous != nil {
			query = tx.Select("id", "email", "ldap_synced").Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
		}
		if err := query.Where("id = ?", u.Id).First(&current).Error; err != nil {
			return err
		}
		if previous != nil && (previous.Email != current.Email || previous.LdapSynced != current.LdapSynced) {
			return errors.New("UserEmailChanged")
		}
		target := tx.Model(u)
		if len(fields) != 0 {
			target = target.Select(fields)
		}
		return target.Updates(u).Error
	}
	if (u.IsAdmin != nil && !*u.IsAdmin) || u.Status == model.COMMON_STATUS_DISABLED {
		return withAdminRemoval(u.Id, ldapSettings, "The last admin user cannot be disabled or demoted", update)
	}
	if ldapSettings == nil {
		return update(DB)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, ldapSettings.Revision, clause.LockingStrengthUpdate); err != nil {
			return err
		}
		return update(tx)
	})
}

// FlushToken 清空token
func (us *UserService) FlushToken(u *model.User) error {
	return DB.Where("user_id = ?", u.Id).Delete(&model.UserToken{}).Error
}

// UpdatePassword 更新密码
func (us *UserService) UpdatePassword(u *model.User, password string) error {
	hash, err := encryptPassword(password)
	if err != nil {
		return err
	}
	if err := DB.Model(u).Update("password", hash).Error; err != nil {
		return err
	}
	u.Password = hash
	return us.FlushToken(u)
}

// IsAdmin 是否管理员
func (us *UserService) IsAdmin(u *model.User) bool {
	return u != nil && *u.IsAdmin
}

// RouteNames
func (us *UserService) RouteNames(u *model.User) []string {
	if us.IsAdmin(u) {
		return model.AdminRouteNames
	}
	return model.UserRouteNames
}

// InfoByOauthId 根据oauth的name和openId取用户信息
func (us *UserService) InfoByOauthId(op, openId string, identity OauthProviderIdentity) (user *model.User, autoRegister bool, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		config, err := lockVerifiedOauthProvider(tx, op, identity)
		if err != nil {
			return err
		}
		autoRegister = config.AutoRegister != nil && *config.AutoRegister
		user, err = userByOauthBinding(tx, op, openId)
		return err
	})
	return
}

func userByOauthBinding(tx *gorm.DB, op, openId string) (*model.User, error) {
	var binding model.UserThird
	err := tx.Where("op = ? AND open_id = ?", op, openId).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var user model.User
	if err := tx.First(&user, binding.UserId).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// RegisterByOauth 注册
func (us *UserService) RegisterByOauth(oauthUser *model.OauthUser, op string, identity OauthProviderIdentity) (error, *model.User) {
	Lock.Lock("registerByOauth")
	defer Lock.UnLock("registerByOauth")
	user, _, err := us.InfoByOauthId(op, oauthUser.OpenId, identity)
	if err != nil || user != nil {
		return err, user
	}
	//check if this email has been registered
	email := strings.ToLower(strings.TrimSpace(oauthUser.Email))
	if !ValidateEmailAddress(email) {
		return errors.New("InvalidEmail"), nil
	}
	if !oauthUser.VerifiedEmail {
		return errors.New("OauthEmailUnverified"), nil
	}
	oauthUser.Email = email
	cfg, err := GetLdapSetting()
	if err != nil {
		return err, nil
	}
	directory := newLdapRequest()
	ldapUser, err := directory.getUserInfoByEmailLdap(&cfg, email)
	var prepared *model.User
	if err == nil {
		prepared, err = directory.prepareLdapUser(&cfg, ldapUser)
		if revokeErr := revokeLdapAccess(&cfg, ldapUser); revokeErr != nil {
			return revokeErr, nil
		}
		if err != nil {
			return err, nil
		}
	} else if !errors.Is(err, ErrLdapNotEnabled) && !errors.Is(err, ErrLdapUserNotFound) {
		return err, nil
	}

	commit := func(checked *ldapRecheckRequired) error {
		return DB.Transaction(func(tx *gorm.DB) error {
			strength := clause.LockingStrengthUpdate
			if prepared != nil {
				strength = clause.LockingStrengthShare
			}
			if err := lockLdapRevision(tx, cfg.Revision, strength); err != nil {
				return err
			}
			config, err := lockVerifiedOauthProvider(tx, op, identity)
			if err != nil {
				return err
			}
			bound, err := userByOauthBinding(tx, op, oauthUser.OpenId)
			if err != nil {
				return err
			}
			if bound != nil {
				user = bound
				return nil
			}
			if prepared != nil {
				user, err = mapLdapUser(tx, &cfg, prepared, checked)
			} else {
				user = &model.User{}
				err = tx.Where("email = ?", email).First(user).Error
			}
			if prepared == nil && errors.Is(err, gorm.ErrRecordNotFound) {
				user = &model.User{GroupId: model.DefaultUserGroupID}
				oauthUser.ToUser(user)
				var count int64
				if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
					return err
				}
				isAdmin := count == 0
				user.IsAdmin = &isAdmin
				user.Status = model.COMMON_STATUS_ENABLE
				if err := tx.Create(user).Error; err != nil {
					return errors.New("OauthRegisterFailed")
				}
			} else if err != nil {
				return err
			}
			binding := &model.UserThird{}
			binding.FromOauthUser(user.Id, oauthUser, config.OauthType, op)
			return tx.Create(binding).Error
		})
	}
	for attempt := 1; ; attempt++ {
		user = nil
		err = withLdapRecheck(&cfg, ldapUser, prepared, commit)
		if err == nil || attempt == 3 || prepared == nil || !isMySQLDeadlock(err) {
			return err, user
		}
	}
}

// UserThirdsByUserId
func (us *UserService) UserThirdsByUserId(userId uint) (res []*model.UserThird) {
	DB.Where("user_id = ?", userId).Find(&res)
	return res
}

func (us *UserService) UserThirdInfo(userId uint, op string) *model.UserThird {
	ut := &model.UserThird{}
	DB.Where("user_id = ? and op = ?", userId, op).First(ut)
	return ut
}

// IsPasswordEmptyById 根据用户id判断密码是否为空，主要用于第三方登录的自动注册
func (us *UserService) IsPasswordEmptyById(id uint) bool {
	u := &model.User{}
	if DB.Where("id = ?", id).First(u).Error != nil {
		return false
	}
	return u.Password == ""
}

// IsPasswordEmptyByUser 判断密码是否为空，主要用于第三方登录的自动注册
func (us *UserService) IsPasswordEmptyByUser(u *model.User) bool {
	return us.IsPasswordEmptyById(u.Id)
}

func (us *UserService) TokenList(page uint, size uint, f func(tx *gorm.DB)) *model.UserTokenList {
	res := &model.UserTokenList{}
	res.Page = int64(page)
	res.PageSize = int64(size)
	tx := DB.Model(&model.UserToken{})
	if f != nil {
		f(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, size))
	tx.Find(&res.UserTokens)
	return res
}

func (us *UserService) TokenInfoById(id uint) *model.UserToken {
	ut := &model.UserToken{}
	DB.Where("id = ?", id).First(ut)
	return ut
}

func (us *UserService) DeleteToken(l *model.UserToken) error {
	return DB.Delete(l).Error
}

// Helper functions, getUserCount
func (us *UserService) getUserCount() int64 {
	var count int64
	DB.Model(&model.User{}).Count(&count)
	return count
}

func withAdminRemoval(id uint, cfg *model.LdapSetting, lastAdminMessage string, remove func(*gorm.DB) error) error {
	if cfg == nil {
		settings, err := GetLdapSetting()
		if err != nil {
			return err
		}
		cfg = &settings
	}
	findAdmins := func(db *gorm.DB) ([]model.User, error) {
		var admins []model.User
		err := db.Select("id", "email", "password", "ldap_synced").
			Where("is_admin = ? AND status = ?", true, model.COMMON_STATUS_ENABLE).Order("id").Find(&admins).Error
		return admins, err
	}
	removesLocalAdmin := func(admin model.User) bool {
		return admin.Id == id && !admin.LdapSynced && admin.Password != ""
	}
	var replacement model.User
	if cfg.Enable {
		admins, err := findAdmins(DB)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(admins, removesLocalAdmin) {
			directory := newLdapRequest()
			for _, admin := range admins {
				if admin.Id == id || admin.LdapSynced || admin.Password == "" || !validPasswordLoginEmail(admin.Email) {
					continue
				}
				exists, err := directory.isEmailExists(cfg, admin.Email)
				if err != nil {
					return ErrLdapDirectoryUnavailable
				}
				if !exists {
					replacement = admin
					break
				}
			}
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthUpdate); err != nil {
			return err
		}
		admins, err := findAdmins(tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}))
		if err != nil {
			return err
		}
		if len(admins) == 1 && admins[0].Id == id {
			return errors.New(lastAdminMessage)
		}
		if cfg.Enable && slices.ContainsFunc(admins, removesLocalAdmin) &&
			!slices.ContainsFunc(admins, func(admin model.User) bool {
				return admin.Id == replacement.Id && admin.Email == replacement.Email &&
					admin.Password == replacement.Password && !admin.LdapSynced
			}) {
			return ErrLdapLastLocalAdmin
		}
		return remove(tx)
	})
}

// UserTokenExpireTimestamp 生成用户token过期时间
func (us *UserService) UserTokenExpireTimestamp() int64 {
	exp := Config.App.TokenExpire
	if exp == 0 {
		exp = 7 * 24 * time.Hour
	}
	return time.Now().Add(exp).Unix()
}

func (us *UserService) RefreshAccessToken(ut *model.UserToken) {
	if len(Jwt.Key) > 0 {
		return
	}
	ut.ExpiredAt = us.UserTokenExpireTimestamp()
	DB.Model(ut).Update("expired_at", ut.ExpiredAt)
}

func (us *UserService) AutoRefreshAccessToken(ut *model.UserToken) {
	if ut.ExpiredAt-time.Now().Unix() < Config.App.TokenExpire.Milliseconds()/3000 {
		us.RefreshAccessToken(ut)
	}
}

func (us *UserService) BatchDeleteUserToken(ids []uint) error {
	return DB.Where("id in ?", ids).Delete(&model.UserToken{}).Error
}

func (us *UserService) VerifyJWT(token string) (uint, error) {
	return Jwt.ParseToken(token)
}

func (us *UserService) IsEmailExistsLdap(email string) (bool, error) {
	return AllService.LdapService.IsEmailExists(email)
}
