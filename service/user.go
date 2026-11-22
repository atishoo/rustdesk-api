package service

import (
	"errors"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserService struct {
}

// InfoById 根据用户id取用户信息
func (us *UserService) InfoById(id uint) *model.User {
	u := &model.User{}
	DB.Where("id = ?", id).First(u)
	return u
}

// InfoByUsername 根据用户名取用户信息
func (us *UserService) InfoByUsername(un string) *model.User {
	u := &model.User{}
	DB.Where("username = ?", un).First(u)
	return u
}

// InfoByEmail 根据邮箱取用户信息
func (us *UserService) InfoByEmail(email string) *model.User {
	u := &model.User{}
	DB.Where("email = ?", email).First(u)
	return u
}

// InfoByOpenid 根据openid取用户信息
func (us *UserService) InfoByOpenid(openid string) *model.User {
	u := &model.User{}
	DB.Where("openid = ?", openid).First(u)
	return u
}

// InfoByUsernamePassword 根据用户名密码取用户信息
func (us *UserService) InfoByUsernamePassword(username, password string) *model.User {
	if Config.Ldap.Enable {
		u, err := AllService.LdapService.Authenticate(username, password)
		if err == nil {
			return u
		}
		Logger.Errorf("LDAP authentication failed, %v", err)
		if errors.Is(err, ErrLdapToLocalUserFailed) {
			return &model.User{}
		}
		Logger.Warn("Fallback to local database")
	}
	u := &model.User{}
	login := strings.ToLower(strings.TrimSpace(username))
	DB.Where("username = ? OR email = ?", username, login).First(u)
	if u.Id == 0 {
		return u
	}
	ok, newHash, err := utils.VerifyPassword(u.Password, password)
	if err != nil || !ok {
		return &model.User{}
	}
	if newHash != "" {
		DB.Model(u).Update("password", newHash)
		u.Password = newHash
	}
	return u
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
	return utils.Md5(u.Username + time.Now().String()), expiresAt
}

// Login 登录
func (us *UserService) Login(u *model.User, llog *model.LoginLog, deviceSession bool) (*model.UserToken, error) {
	token, expiresAt := us.GenerateToken(u)
	if token == "" || expiresAt <= time.Now().Unix() {
		return nil, errors.New("could not generate a usable session token")
	}
	session := model.UserToken{
		UserId:    u.Id,
		Token:     token,
		ExpiredAt: expiresAt,
	}
	details := *llog
	if deviceSession {
		session.DeviceId = details.DeviceId
		session.DeviceUuid = details.Uuid
		if details.Client == model.LoginLogClientWebAdmin {
			details.Client = model.LoginLogClientApp
		}
	}
	for attempt := 1; ; attempt++ {
		ut, loginLog := session, details
		err := DB.Transaction(func(tx *gorm.DB) error {
			// Match the device-first lock order used by reporting and logout.
			if ut.DeviceId != "" || ut.DeviceUuid != "" {
				if err := AllService.PeerService.bindDeviceUser(tx, ut.DeviceId, ut.DeviceUuid, u.Id); err != nil {
					return err
				}
			}
			if err := tx.Create(&ut).Error; err != nil {
				return err
			}
			loginLog.UserTokenId = ut.Id
			return tx.Create(&loginLog).Error
		})
		if err == nil {
			*llog = loginLog
			return &ut, nil
		}
		// MySQL can roll back one of two concurrent claims for a new device ID.
		var deadlock *mysql.MySQLError
		if attempt == 3 || session.DeviceId == "" || session.DeviceUuid == "" ||
			!errors.As(err, &deadlock) || deadlock.Number != 1213 {
			return nil, err
		}
	}
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

// ListIdAndNameByGroupId 根据组id取用户id和用户名列表
func (us *UserService) ListIdAndNameByGroupId(groupId uint) (res []*model.User) {
	DB.Model(&model.User{}).Where("group_id = ?", groupId).Select("id, username").Find(&res)
	return res
}

// CheckUserEnable 判断用户是否禁用
func (us *UserService) CheckUserEnable(u *model.User) bool {
	return u.Status == model.COMMON_STATUS_ENABLE
}

// Create 创建
func (us *UserService) Create(u *model.User) error {
	// The initial username should be formatted, and the username should be unique
	if us.IsUsernameExists(u.Username) {
		return errors.New("UsernameExists")
	}
	u.Username = us.formatUsername(u.Username)
	var err error
	u.Password, err = utils.EncryptPassword(u.Password)
	if err != nil {
		return err
	}
	res := DB.Create(u).Error
	return res
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
	err := DB.Transaction(func(tx *gorm.DB) error {
		last, err := lastEnabledAdmin(tx, u.Id)
		if err != nil {
			return err
		}
		if last {
			return errors.New("The last admin user cannot be deleted")
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
	// 删除关联的peer
	if err := AllService.PeerService.EraseUserId(u.Id); err != nil {
		Logger.Warn("User deleted successfully, but failed to unlink peer.")
		return nil
	}
	return nil
}

// Update 更新
func (us *UserService) Update(u *model.User, fields ...string) error {
	update := func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Select("id").Where("id = ?", u.Id).First(&current).Error; err != nil {
			return err
		}
		target := tx.Model(u)
		if len(fields) != 0 {
			target = target.Select(fields)
		}
		return target.Updates(u).Error
	}
	if (u.IsAdmin == nil || *u.IsAdmin) && u.Status != model.COMMON_STATUS_DISABLED {
		return update(DB)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		last, err := lastEnabledAdmin(tx, u.Id)
		if err != nil {
			return err
		}
		if last {
			return errors.New("The last admin user cannot be disabled or demoted")
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
	var err error
	u.Password, err = utils.EncryptPassword(password)
	if err != nil {
		return err
	}
	err = DB.Model(u).Update("password", u.Password).Error
	if err != nil {
		return err
	}
	err = us.FlushToken(u)
	return err
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
	email := oauthUser.Email
	// only email is not empty
	if email != "" {
		email = strings.ToLower(email)
		// update email to oauthUser, in case it contain upper case
		oauthUser.Email = email
		// call this, if find user by email, it will update the email to local database
		var ldapErr error
		user, ldapErr = AllService.LdapService.GetUserInfoByEmailLocal(email)
		// If we enable ldap, and the error is not ErrLdapUserNotFound, return the error because we could not sure if the user is not found in ldap
		if !(errors.Is(ldapErr, ErrLdapNotEnabled) || errors.Is(ldapErr, ErrLdapUserNotFound) || ldapErr == nil) {
			return ldapErr, user
		}
		if user.Id == 0 {
			// this means the user is not found in ldap, maybe ldao is not enabled
			user = us.InfoByEmail(email)
		}
	}
	if user == nil || user.Id == 0 {
		username := us.formatUsername(oauthUser.Username)
		user = &model.User{Username: us.GenerateUsernameByOauth(username), GroupId: model.DefaultUserGroupID}
		oauthUser.ToUser(user, false)
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
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
		if user.Id == 0 {
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
		}
		binding := &model.UserThird{}
		binding.FromOauthUser(user.Id, oauthUser, config.OauthType, op)
		return tx.Create(binding).Error
	})
	return err, user
}

// GenerateUsernameByOauth 生成用户名
func (us *UserService) GenerateUsernameByOauth(name string) string {
	for us.IsUsernameExists(name) {
		name += strconv.Itoa(rand.Intn(10)) // Append a random digit (0-9)
	}
	return name
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

// IsPasswordEmptyByUsername 根据用户id判断密码是否为空，主要用于第三方登录的自动注册
func (us *UserService) IsPasswordEmptyByUsername(username string) bool {
	u := &model.User{}
	if DB.Where("username = ?", username).First(u).Error != nil {
		return false
	}
	return u.Password == ""
}

// IsPasswordEmptyByUser 判断密码是否为空，主要用于第三方登录的自动注册
func (us *UserService) IsPasswordEmptyByUser(u *model.User) bool {
	return us.IsPasswordEmptyById(u.Id)
}

// Register 注册, 如果用户名已存在则返回nil
func (us *UserService) Register(username string, email string, password string, status model.StatusCode) *model.User {
	u := &model.User{
		Username: username,
		Email:    email,
		Password: password,
		GroupId:  model.DefaultUserGroupID,
		Status:   status,
	}
	err := us.Create(u)
	if err != nil {
		return nil
	}
	return u
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

// Helper functions, used for formatting username
func (us *UserService) formatUsername(username string) string {
	username = strings.ReplaceAll(username, " ", "")
	username = strings.ToLower(username)
	return username
}

// Helper functions, getUserCount
func (us *UserService) getUserCount() int64 {
	var count int64
	DB.Model(&model.User{}).Count(&count)
	return count
}

func lastEnabledAdmin(tx *gorm.DB, id uint) (bool, error) {
	if tx.Dialector.Name() == "sqlite" {
		// SQLite ignores FOR UPDATE. Reserve its write lock before taking the read snapshot.
		if err := tx.Model(&model.User{}).Where("1 = 0").UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return false, err
		}
	}
	var admins []uint
	err := tx.Model(&model.User{}).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("is_admin = ? AND status = ?", true, model.COMMON_STATUS_ENABLE).Order("id").Pluck("id", &admins).Error
	return len(admins) == 1 && admins[0] == id, err
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

// IsUsernameExists 判断用户名是否存在, it will check the internal database and LDAP(if enabled)
func (us *UserService) IsUsernameExists(username string) bool {
	return us.IsUsernameExistsLocal(username) || AllService.LdapService.IsUsernameExists(username)
}

func (us *UserService) IsUsernameExistsLocal(username string) bool {
	u := &model.User{}
	DB.Where("username = ?", username).First(u)
	return u.Id != 0
}

func (us *UserService) IsEmailExistsLdap(email string) bool {
	return AllService.LdapService.IsEmailExists(email)
}
