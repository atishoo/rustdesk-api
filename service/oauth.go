package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"github.com/lejianwen/rustdesk-api/v2/utils"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	// "golang.org/x/oauth2/google"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	// "io"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type OauthService struct {
}

var (
	ErrLastLoginMethod              = errors.New("cannot remove the last login method")
	ErrLdapUnbindPasswordRequired   = errors.New("LDAP password is required to unbind")
	ErrLdapUnbindVerificationFailed = errors.New("unable to verify LDAP login for unbinding")
	ErrLdapUnbindRateLimited        = errors.New("LDAP verification attempts temporarily limited")
	ErrOauthIdentityBound           = errors.New("OAuth provider identity cannot be changed while accounts are linked")
	ErrOauthIdentityChanged         = errors.New("OAuth provider identity has changed since authorization")
)

var ldapUnbindLimiter = utils.NewLoginLimiter(utils.SecurityPolicy{
	CaptchaThreshold: -1,
	BanThreshold:     5,
	AttemptsWindow:   5 * time.Minute,
	BanDuration:      5 * time.Minute,
})

// Define a struct to parse the .well-known/openid-configuration response
type OidcEndpoint struct {
	Issuer   string `json:"issuer"`
	AuthURL  string `json:"authorization_endpoint"`
	TokenURL string `json:"token_endpoint"`
	UserInfo string `json:"userinfo_endpoint"`
}

type OauthCacheItem struct {
	UserId           uint                  `json:"user_id"`
	Id               string                `json:"id"` //卡皮远控的设备ID
	Op               string                `json:"op"`
	Action           string                `json:"action"`
	Uuid             string                `json:"uuid"`
	DeviceName       string                `json:"device_name"`
	DeviceOs         string                `json:"device_os"`
	DeviceType       string                `json:"device_type"`
	OpenId           string                `json:"open_id"`
	Username         string                `json:"username"`
	Name             string                `json:"name"`
	Email            string                `json:"email"`
	Verifier         string                `json:"verifier"` // used for oauth pkce
	Nonce            string                `json:"nonce"`
	RedirectURL      string                `json:"redirect_url"`
	Ticket           string                `json:"-"`
	Binding          bool                  `json:"-"`
	Redeeming        bool                  `json:"-"`
	ProviderIdentity OauthProviderIdentity `json:"-"`
}

type OauthProviderIdentity struct {
	oauthType, issuer, clientID string
}

func oauthProviderIdentity(config *model.Oauth) OauthProviderIdentity {
	identity := OauthProviderIdentity{oauthType: config.OauthType}
	if config.OauthType == model.OauthTypeOidc || config.OauthType == model.OauthTypeGoogle {
		identity.issuer = config.Issuer
		identity.clientID = config.ClientId
	}
	return identity
}

// lockVerifiedOauthProvider serializes binding writes with provider identity updates.
func lockVerifiedOauthProvider(tx *gorm.DB, op string, identity OauthProviderIdentity) (*model.Oauth, error) {
	var config model.Oauth
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("op = ?", op).First(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && oauthProviderIdentity(&config) != identity) {
		return nil, ErrOauthIdentityChanged
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (oci *OauthCacheItem) ToOauthUser() *model.OauthUser {
	return &model.OauthUser{
		OpenId:   oci.OpenId,
		Username: oci.Username,
		Name:     oci.Name,
		Email:    oci.Email,
	}
}

var OauthCache = &sync.Map{}

const (
	OauthActionTypeLogin = "login"
	OauthActionTypeBind  = "bind"
)

func (oci *OauthCacheItem) UpdateFromOauthUser(oauthUser *model.OauthUser) {
	oci.OpenId = oauthUser.OpenId
	oci.Username = oauthUser.Username
	oci.Name = oauthUser.Name
	oci.Email = oauthUser.Email
}

func (os *OauthService) GetOauthCache(key string) *OauthCacheItem {
	v, ok := OauthCache.Load(key)
	if !ok {
		return nil
	}
	return v.(*OauthCacheItem)
}

func (os *OauthService) ConsumeOauthCache(key string, claim *OauthCacheItem) {
	OauthCache.CompareAndDelete(key, claim)
}

func (os *OauthService) ReplaceOauthCache(key string, current, next *OauthCacheItem) bool {
	return OauthCache.CompareAndSwap(key, current, next)
}

func (os *OauthService) ConfirmWebauth(key string, userId uint) *OauthCacheItem {
	for {
		v := os.GetOauthCache(key)
		if v == nil || v.Action != OauthActionTypeLogin || v.Op != model.OauthTypeWebauth || v.UserId != 0 || (v.DeviceType == model.LoginLogClientWebAdmin && v.Ticket == "") {
			return nil
		}
		confirmed := *v
		confirmed.UserId = userId
		if os.ReplaceOauthCache(key, v, &confirmed) {
			return &confirmed
		}
	}
}

func (os *OauthService) SetOauthCache(key string, item *OauthCacheItem, expire uint) {
	OauthCache.Store(key, item)
	if expire > 0 {
		time.AfterFunc(time.Duration(expire)*time.Second, func() {
			os.DeleteOauthCache(key)
		})
	}
}

func (os *OauthService) DeleteOauthCache(key string) {
	OauthCache.Delete(key)
}

func (os *OauthService) BeginAuth(op string) (error error, state, verifier, nonce, url string) {
	state = utils.RandomString(10) + strconv.FormatInt(time.Now().Unix(), 10)
	verifier = ""
	nonce = ""
	if strings.EqualFold(op, model.OauthTypeWebauth) {
		if op != model.OauthTypeWebauth || !Config.App.WebSso {
			return errors.New("ConfigNotFound"), "", "", "", ""
		}
		url = Config.Capylink.ApiServer + "/_admin/#/oauth/" + state
		//url = "http://localhost:8888/_admin/#/oauth/" + code
		return nil, state, verifier, nonce, url
	}
	err, oauthInfo, oauthConfig, _ := os.GetOauthConfig(op)
	if err == nil {
		extras := make([]oauth2.AuthCodeOption, 0, 3)

		nonce = utils.RandomString(10)
		extras = append(extras, oauth2.SetAuthURLParam("nonce", nonce))

		if oauthInfo.PkceEnable != nil && *oauthInfo.PkceEnable {
			extras = append(extras, oauth2.AccessTypeOffline)
			verifier = oauth2.GenerateVerifier()
			switch oauthInfo.PkceMethod {
			case model.PKCEMethodS256:
				extras = append(extras, oauth2.S256ChallengeOption(verifier))
			case model.PKCEMethodPlain:
				// oauth2 does not have a plain challenge option, so we add it manually
				extras = append(extras, oauth2.SetAuthURLParam("code_challenge_method", "plain"), oauth2.SetAuthURLParam("code_challenge", verifier))
			}
		}

		return err, state, verifier, nonce, oauthConfig.AuthCodeURL(state, extras...)
	}

	return err, state, verifier, nonce, ""
}

func (os *OauthService) FetchOidcProvider(issuer string) (error, *oidc.Provider) {

	// Get the HTTP client (with or without proxy based on configuration)
	client := getHTTPClientWithProxy()

	ctx := oidc.ClientContext(context.Background(), client)

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return err, nil
	}

	return nil, provider
}

func (os *OauthService) GithubProvider() *oidc.Provider {
	return (&oidc.ProviderConfig{
		IssuerURL:     "",
		AuthURL:       github.Endpoint.AuthURL,
		TokenURL:      github.Endpoint.TokenURL,
		DeviceAuthURL: github.Endpoint.DeviceAuthURL,
		UserInfoURL:   model.UserEndpointGithub,
		JWKSURL:       "",
		Algorithms:    nil,
	}).NewProvider(context.Background())
}

func (os *OauthService) LinuxdoProvider() *oidc.Provider {
	return (&oidc.ProviderConfig{
		IssuerURL:     "",
		AuthURL:       "https://connect.linux.do/oauth2/authorize",
		TokenURL:      "https://connect.linux.do/oauth2/token",
		DeviceAuthURL: "",
		UserInfoURL:   model.UserEndpointLinuxdo,
		JWKSURL:       "",
		Algorithms:    nil,
	}).NewProvider(context.Background())
}

// GetOauthConfig retrieves the OAuth2 configuration based on the provider name
func (os *OauthService) GetOauthConfig(op string) (err error, oauthInfo *model.Oauth, oauthConfig *oauth2.Config, provider *oidc.Provider) {
	//err, oauthInfo, oauthConfig = os.getOauthConfigGeneral(op)
	oauthInfo = os.InfoByOp(op)
	if oauthInfo.Id == 0 || oauthInfo.ClientId == "" || oauthInfo.ClientSecret == "" {
		return errors.New("ConfigNotFound"), nil, nil, nil
	}
	oauthConfig = &oauth2.Config{
		ClientID:     oauthInfo.ClientId,
		ClientSecret: oauthInfo.ClientSecret,
		RedirectURL:  Config.Capylink.ApiServer + "/api/oidc/callback",
	}

	// Maybe should validate the oauthConfig here
	oauthType := oauthInfo.OauthType
	err = model.ValidateOauthType(oauthType)
	if err != nil {
		return err, nil, nil, nil
	}
	switch oauthType {
	case model.OauthTypeGithub:
		oauthConfig.Endpoint = github.Endpoint
		oauthConfig.Scopes = []string{"read:user", "user:email"}
		provider = os.GithubProvider()
	case model.OauthTypeLinuxdo:
		provider = os.LinuxdoProvider()
		oauthConfig.Endpoint = provider.Endpoint()
		oauthConfig.Scopes = []string{"profile"}
	//case model.OauthTypeGoogle: //google单独出来，可以少一次FetchOidcEndpoint请求
	//	oauthConfig.Endpoint = google.Endpoint
	//	oauthConfig.Scopes = os.constructScopes(oauthInfo.Scopes)
	case model.OauthTypeOidc, model.OauthTypeGoogle:
		err, provider = os.FetchOidcProvider(oauthInfo.Issuer)
		if err != nil {
			return err, nil, nil, nil
		}
		oauthConfig.Endpoint = provider.Endpoint()
		oauthConfig.Scopes = os.constructScopes(oauthInfo.Scopes)
	default:
		return errors.New("unsupported OAuth type"), nil, nil, nil
	}
	return nil, oauthInfo, oauthConfig, provider
}

func getHTTPClientWithProxy() *http.Client {
	//add timeout 30s
	timeout := time.Duration(60) * time.Second
	if Config.Proxy.Enable {
		if Config.Proxy.Host == "" {
			Logger.Warn("Proxy is enabled but proxy host is empty.")
			return http.DefaultClient
		}
		proxyURL, err := url.Parse(Config.Proxy.Host)
		if err != nil {
			Logger.Warn("Invalid proxy URL: ", err)
			return http.DefaultClient
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
		return &http.Client{Transport: transport, Timeout: timeout}
	}
	return http.DefaultClient
}
func (os *OauthService) callbackBase(oauthConfig *oauth2.Config, provider *oidc.Provider, code string, verifier string, nonce string, userData interface{}) (err error, client *http.Client) {

	// 设置代理客户端
	httpClient := getHTTPClientWithProxy()
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)

	exchangeOpts := make([]oauth2.AuthCodeOption, 0, 1)
	if verifier != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(verifier))
	}

	token, err := oauthConfig.Exchange(ctx, code, exchangeOpts...)

	if err != nil {
		Logger.Warn("oauthConfig.Exchange() failed: ", err)
		return errors.New("GetOauthTokenError"), nil
	}

	// 获取 ID Token， github没有id_token
	rawIDToken, ok := token.Extra("id_token").(string)
	if ok && rawIDToken != "" {
		// 验证 ID Token
		v := provider.Verifier(&oidc.Config{ClientID: oauthConfig.ClientID})
		idToken, err2 := v.Verify(ctx, rawIDToken)
		if err2 != nil {
			Logger.Warn("IdTokenVerifyError: ", err2)
			return errors.New("IdTokenVerifyError"), nil
		}
		if nonce != "" {
			// 验证 nonce
			var claims struct {
				Nonce string `json:"nonce"`
			}
			if err2 = idToken.Claims(&claims); err2 != nil {
				Logger.Warn("Failed to parse ID Token claims: ", err)
				return errors.New("IDTokenClaimsError"), nil
			}

			if claims.Nonce != nonce {
				Logger.Warn("Nonce does not match")
				return errors.New("NonceDoesNotMatch"), nil
			}
		}
	}

	// 获取用户信息
	client = oauthConfig.Client(ctx, token)
	resp, err := client.Get(provider.UserInfoEndpoint())
	if err != nil {
		Logger.Warn("failed getting user info: ", err)
		return errors.New("GetOauthUserInfoError"), nil
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			Logger.Warn("failed closing response body: ", closeErr)
		}
	}()

	// 解析用户信息
	if err = json.NewDecoder(resp.Body).Decode(userData); err != nil {
		Logger.Warn("failed decoding user info: ", err)
		return errors.New("DecodeOauthUserInfoError"), nil
	}

	return nil, client
}

// githubCallback github回调
func (os *OauthService) githubCallback(oauthConfig *oauth2.Config, provider *oidc.Provider, code, verifier, nonce string) (error, *model.OauthUser) {
	var user = &model.GithubUser{}
	err, client := os.callbackBase(oauthConfig, provider, code, verifier, nonce, user)
	if err != nil {
		return err, nil
	}
	err = os.getGithubPrimaryEmail(client, user)
	if err != nil {
		return err, nil
	}
	return nil, user.ToOauthUser()
}

// linuxdoCallback linux.do回调
func (os *OauthService) linuxdoCallback(oauthConfig *oauth2.Config, provider *oidc.Provider, code, verifier, nonce string) (error, *model.OauthUser) {
	var user = &model.LinuxdoUser{}
	err, _ := os.callbackBase(oauthConfig, provider, code, verifier, nonce, user)
	if err != nil {
		return err, nil
	}
	return nil, user.ToOauthUser()
}

// oidcCallback oidc回调, 通过code获取用户信息
func (os *OauthService) oidcCallback(oauthConfig *oauth2.Config, provider *oidc.Provider, code, verifier, nonce string) (error, *model.OauthUser) {
	var user = &model.OidcUser{}
	if err, _ := os.callbackBase(oauthConfig, provider, code, verifier, nonce, user); err != nil {
		return err, nil
	}
	return nil, user.ToOauthUser()
}

// Callback: Get user information by code and op(Oauth provider)
func (os *OauthService) Callback(code, verifier, op, nonce string) (err error, oauthUser *model.OauthUser, identity OauthProviderIdentity) {
	err, oauthInfo, oauthConfig, provider := os.GetOauthConfig(op)
	// oauthType is already validated in GetOauthConfig
	if err != nil {
		return err, nil, identity
	}
	oauthType := oauthInfo.OauthType
	switch oauthType {
	case model.OauthTypeGithub:
		err, oauthUser = os.githubCallback(oauthConfig, provider, code, verifier, nonce)
	case model.OauthTypeLinuxdo:
		err, oauthUser = os.linuxdoCallback(oauthConfig, provider, code, verifier, nonce)
	case model.OauthTypeOidc, model.OauthTypeGoogle:
		err, oauthUser = os.oidcCallback(oauthConfig, provider, code, verifier, nonce)
	default:
		return errors.New("unsupported OAuth type"), nil, identity
	}
	return err, oauthUser, oauthProviderIdentity(oauthInfo)
}

func (os *OauthService) UserThirdInfo(op string, openId string) *model.UserThird {
	ut := &model.UserThird{}
	DB.Where("open_id = ? and op = ?", openId, op).First(ut)
	return ut
}

// BindOauthUser: Bind third party account
func (os *OauthService) BindOauthUser(userId uint, oauthUser *model.OauthUser, op string, identity OauthProviderIdentity) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, userId).Error; err != nil {
			return err
		}
		config, err := lockVerifiedOauthProvider(tx, op, identity)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.UserThird{}).Where("op = ? AND open_id = ?", op, oauthUser.OpenId).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("OauthHasBindOtherUser")
		}
		utr := &model.UserThird{}
		utr.FromOauthUser(userId, oauthUser, config.OauthType, op)
		return tx.Create(utr).Error
	})
}

// UnBindOauthUser: Unbind third party account
func (os *OauthService) UnBindOauthUser(userId uint, op, loginPassword string, loginBanned bool) error {
	var email, directoryEmail string
	var directoryValues *model.User
	var useLocalPassword, localAdminFallback bool
	unbind := func(tx *gorm.DB) error {
		var user model.User
		// Serialize unbinding even when two requests remove different providers.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "email", "password", "status", "is_admin", "ldap_synced").First(&user, userId).Error; err != nil {
			return err
		}
		localAdminFallback = user.Password != "" && !user.LdapSynced && user.IsAdmin != nil && *user.IsAdmin
		result := tx.Where("user_id = ? AND op = ?", userId, op).Delete(&model.UserThird{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if user.Email != "" && user.Password != "" && AllService.UserService.CheckUserEnable(&user) {
			if useLocalPassword && user.Email == email {
				valid, _, err := utils.VerifyPassword(user.Password, loginPassword)
				if err == nil && valid {
					return nil
				}
			} else if !Config.Ldap.Enable || !Config.Ldap.User.Sync {
				emptyPassword, _, err := utils.VerifyPassword(user.Password, "")
				if err == nil && !emptyPassword {
					return nil
				}
			}
		}
		// WebAuth requires an existing session, so it cannot replace a login method.
		providers := configuredOauthProviders(tx).Select("op")
		var remaining int64
		if err := tx.Model(&model.UserThird{}).
			Where("user_id = ? AND open_id <> '' AND op IN (?)", userId, providers).Count(&remaining).Error; err != nil {
			return err
		}
		if remaining > 0 {
			return nil
		}
		if Config.Ldap.Enable && directoryEmail != "" && user.Email == email {
			// LDAP login maps the returned directory email to the local account.
			var mapped model.User
			err := tx.Select("id", "status").Where("email = ?", directoryEmail).First(&mapped).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if mapped.Id == userId {
				if directoryValues != nil {
					// Verify the login write without changing the account's profile or role.
					if err := tx.Exec("SAVEPOINT ldap_sync_check").Error; err != nil {
						return ErrLdapUnbindVerificationFailed
					}
					if err := syncLdapUser(tx, &mapped, directoryValues); err != nil {
						return ErrLdapUnbindVerificationFailed
					}
					if err := tx.Exec("ROLLBACK TO SAVEPOINT ldap_sync_check").Error; err != nil {
						return ErrLdapUnbindVerificationFailed
					}
				}
				if !AllService.UserService.CheckUserEnable(&mapped) {
					return ErrLdapUnbindVerificationFailed
				}
				return nil
			}
		}
		email = user.Email
		return ErrLastLoginMethod
	}
	err := DB.Transaction(unbind)
	if !errors.Is(err, ErrLastLoginMethod) || email == "" || !Config.Ldap.Enable {
		return err
	}
	if loginPassword == "" {
		return ErrLdapUnbindPasswordRequired
	}
	if loginBanned || !ldapUnbindLimiter.ReserveAttempt(strconv.FormatUint(uint64(userId), 10)) {
		return ErrLdapUnbindRateLimited
	}
	// Query the directory after rollback so a slow directory cannot hold database locks.
	directory := &LdapService{deadline: time.Now().Add(5 * time.Second)}
	ldapUser, ldapErr := directory.loginUser(email, loginPassword)
	if ldapErr != nil {
		unavailable := errors.Is(ldapErr, ErrLdapConnectFailed) ||
			errors.Is(ldapErr, ErrLdapBindService) ||
			errors.Is(ldapErr, ErrLdapSearchFailed) ||
			errors.Is(ldapErr, ErrLdapTlsFailed)
		independentLocalAdmin := errors.Is(ldapErr, ErrLdapUserNotFound) && localAdminFallback
		if !localAdminFallback || (!unavailable && !independentLocalAdmin) {
			return ErrLdapUnbindVerificationFailed
		}
		// Only an unavailable directory may fall back to the local password.
		useLocalPassword = true
	} else {
		if ldapUser.Email == "" || AllService.UserService.InfoByEmail(ldapUser.Email).Id != userId {
			return ErrLdapUnbindVerificationFailed
		}
		if Config.Ldap.User.Sync {
			directoryValues, err = directory.localUserValues(&Config.Ldap, ldapUser)
			if err != nil {
				return ErrLdapUnbindVerificationFailed
			}
		}
		directoryEmail = strings.ToLower(strings.TrimSpace(ldapUser.Email))
	}

	err = DB.Transaction(unbind)
	if errors.Is(err, ErrLastLoginMethod) {
		return ErrLdapUnbindVerificationFailed
	}
	return err
}

// DeleteUserByUserId: When user is deleted, delete all third party bindings
func (os *OauthService) DeleteUserByUserId(userId uint) error {
	return DB.Where("user_id = ?", userId).Delete(&model.UserThird{}).Error
}

// InfoById 根据id获取Oauth信息
func (os *OauthService) InfoById(id uint) *model.Oauth {
	oauthInfo := &model.Oauth{}
	DB.Where("id = ?", id).First(oauthInfo)
	return oauthInfo
}

// InfoByOp 根据op获取Oauth信息
func (os *OauthService) InfoByOp(op string) *model.Oauth {
	oauthInfo := &model.Oauth{}
	DB.Where("op = ?", op).First(oauthInfo)
	return oauthInfo
}

// Helper function to get scopes by operation
func (os *OauthService) getScopesByOp(op string) []string {
	scopes := os.InfoByOp(op).Scopes
	return os.constructScopes(scopes)
}

// Helper function to construct scopes
func (os *OauthService) constructScopes(scopes string) []string {
	values := strings.FieldsFunc(scopes, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	if len(values) == 0 {
		return strings.Split(model.OIDC_DEFAULT_SCOPES, ",")
	}
	if !slices.Contains(values, oidc.ScopeOpenID) {
		return append([]string{oidc.ScopeOpenID}, values...)
	}
	return values
}

func (os *OauthService) List(page, pageSize uint, where func(tx *gorm.DB)) (res *model.OauthList) {
	res = &model.OauthList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.Oauth{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.Oauths)
	return
}

// ValidateOauthProvider 验证Oauth提供者是否正确
func (os *OauthService) ValidateOauthProvider(op string) error {
	if !os.IsOauthProviderExist(op) {
		return fmt.Errorf("OAuth provider with op '%s' not found", op)
	}
	return nil
}

// IsOauthProviderExist 验证Oauth提供者是否存在
func (os *OauthService) IsOauthProviderExist(op string) bool {
	oauthInfo := &model.Oauth{}
	// 使用 Gorm 的 Take 方法查找符合条件的记录
	if err := DB.Where("op = ?", op).Take(oauthInfo).Error; err != nil {
		return false
	}
	return true
}

// Create 创建
func (os *OauthService) Create(oauthInfo *model.Oauth) error {
	err := oauthInfo.FormatOauthInfo()
	if err != nil {
		return err
	}
	res := DB.Create(oauthInfo).Error
	return res
}
func (os *OauthService) Delete(oauthInfo *model.Oauth) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var current model.Oauth
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("op").First(&current, oauthInfo.Id).Error; err != nil {
			return err
		}
		result := tx.Where("NOT EXISTS (?)", oauthProviderBindings(tx, current.Op)).Delete(oauthInfo)
		if result.Error == nil && result.RowsAffected == 0 {
			return ErrOauthIdentityBound
		}
		return result.Error
	})
}

// Update 更新
func (os *OauthService) Update(oauthInfo *model.Oauth, updateScopes bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var current model.Oauth
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("op", "oauth_type", "issuer", "client_id").First(&current, oauthInfo.Id).Error; err != nil {
			return err
		}
		if oauthInfo.Op == "" {
			oauthInfo.Op = current.Op
		}
		suppliedOp := oauthInfo.Op
		if err := oauthInfo.FormatOauthInfo(); err != nil {
			return err
		}
		if suppliedOp != current.Op || oauthInfo.Op != current.Op {
			return errors.New("OAuth provider identifier cannot be changed")
		}
		fields := []string{"oauth_type", "client_id", "client_secret", "pkce_enable", "pkce_method"}
		if oauthInfo.Issuer != "" {
			fields = append(fields, "issuer")
		}
		if oauthInfo.AutoRegister != nil {
			fields = append(fields, "auto_register")
		}
		if updateScopes {
			fields = append(fields, "scopes")
		}
		identityChanged := oauthProviderIdentity(oauthInfo) != oauthProviderIdentity(&current)
		update := tx.Model(oauthInfo).Select(fields)
		if identityChanged {
			update = update.Where("NOT EXISTS (?)", oauthProviderBindings(tx, current.Op))
		}
		result := update.Updates(oauthInfo)
		if result.Error == nil && identityChanged && result.RowsAffected == 0 {
			return ErrOauthIdentityBound
		}
		return result.Error
	})
}

func oauthProviderBindings(db *gorm.DB, op string) *gorm.DB {
	return db.Model(&model.UserThird{}).Select("1").Where("op = ?", op)
}

func configuredOauthProviders(db *gorm.DB) *gorm.DB {
	return db.Model(&model.Oauth{}).
		Where("op <> '' AND LOWER(op) <> ? AND oauth_type IN ? AND client_id <> '' AND client_secret <> ''", model.OauthTypeWebauth, model.OauthProviderTypes()).
		Where("(oauth_type NOT IN ? OR issuer <> '')", []string{model.OauthTypeGoogle, model.OauthTypeOidc})
}

// GetOauthProviders 返回可用于登录的外部提供商。
func (os *OauthService) GetOauthProviders() []string {
	var res []string
	configuredOauthProviders(DB).Pluck("op", &res)
	return res
}

// getGithubPrimaryEmail: Get the primary email of the user from Github
func (os *OauthService) getGithubPrimaryEmail(client *http.Client, githubUser *model.GithubUser) error {
	// the client is already set with the token
	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		return fmt.Errorf("failed to fetch emails: %w", err)
	}
	defer resp.Body.Close()

	// check the response status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch emails: %s", resp.Status)
	}

	// decode the response
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// find the primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			githubUser.Email = e.Email
			githubUser.VerifiedEmail = e.Verified
			return nil
		}
	}

	return fmt.Errorf("no primary verified email found")
}
