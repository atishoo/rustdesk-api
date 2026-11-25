package model

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const OIDC_DEFAULT_SCOPES = "openid,profile,email"

const (
	// make sure the value shouldbe lowercase
	OauthTypeGithub  string = "github"
	OauthTypeGoogle  string = "google"
	OauthTypeOidc    string = "oidc"
	OauthTypeWebauth string = "webauth"
	OauthTypeLinuxdo string = "linuxdo"
	PKCEMethodS256   string = "S256"
	PKCEMethodPlain  string = "plain"
)

func OauthProviderTypes() []string {
	return []string{OauthTypeGithub, OauthTypeGoogle, OauthTypeOidc, OauthTypeLinuxdo}
}

// Validate the type of a configured external OAuth provider.
func ValidateOauthType(oauthType string) error {
	if slices.Contains(OauthProviderTypes(), oauthType) {
		return nil
	}
	return errors.New("invalid Oauth type")
}

const (
	UserEndpointGithub  string = "https://api.github.com/user"
	UserEndpointLinuxdo string = "https://connect.linux.do/api/user"
	IssuerGoogle        string = "https://accounts.google.com"
)

type Oauth struct {
	IdModel
	Op           string `json:"op"`
	OauthType    string `json:"oauth_type"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	//RedirectUrl  string `json:"redirect_url"`
	AutoRegister *bool  `json:"auto_register"`
	Scopes       string `json:"scopes"`
	Issuer       string `json:"issuer"`
	ProxyURL     string `json:"proxy_url" gorm:"type:text;comment:OAuth提供商出站代理地址"`
	PkceEnable   *bool  `json:"pkce_enable"`
	PkceMethod   string `json:"pkce_method"`
	// 编辑时用于核对客户端凭据是否已变更。
	CredentialRevision string `json:"credential_revision,omitempty" gorm:"-"`
	TimeModel
}

func (oa *Oauth) CredentialsRevision() string {
	credentials, _ := json.Marshal([4]string{oa.OauthType, oa.Issuer, oa.ClientId, oa.ClientSecret})
	return fmt.Sprintf("%x", sha256.Sum256(credentials))
}

func ParseOauthProxyURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	proxy, err := url.Parse(value)
	if err != nil || (proxy.Scheme != "http" && proxy.Scheme != "https") || proxy.Hostname() == "" ||
		(proxy.Path != "" && proxy.Path != "/") || strings.ContainsAny(value, "?#") {
		return nil, errors.New("OAuth proxy URL must be an absolute HTTP or HTTPS URL without a non-root path, query, or fragment")
	}
	if port := proxy.Port(); port != "" {
		if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
			return nil, errors.New("OAuth proxy port must be between 1 and 65535")
		}
	}
	return proxy, nil
}

// Helper function to format oauth info, it's used in the update and create method
func (oa *Oauth) FormatOauthInfo() error {
	oauthType := strings.TrimSpace(oa.OauthType)
	err := ValidateOauthType(oa.OauthType)
	if err != nil {
		return err
	}
	if strings.TrimSpace(oa.ClientId) == "" || strings.TrimSpace(oa.ClientSecret) == "" {
		return errors.New("OAuth client ID and secret are required")
	}
	if strings.EqualFold(strings.TrimSpace(oa.Op), OauthTypeWebauth) {
		return errors.New("WebAuth is configured via app.web-sso")
	}
	switch oauthType {
	case OauthTypeGithub:
		oa.Op = OauthTypeGithub
	case OauthTypeGoogle:
		oa.Op = OauthTypeGoogle
	case OauthTypeLinuxdo:
		oa.Op = OauthTypeLinuxdo
	}
	// check if the op is empty, set the default value
	op := strings.TrimSpace(oa.Op)
	if op == "" && oauthType == OauthTypeOidc {
		oa.Op = OauthTypeOidc
	}
	// check the issuer, if the oauth type is google and the issuer is empty, set the issuer to the default value
	issuer := strings.TrimSpace(oa.Issuer)
	// If the oauth type is google and the issuer is empty, set the issuer to the default value
	if oauthType == OauthTypeGoogle && issuer == "" {
		issuer = IssuerGoogle
	}
	if issuer != "" || oauthType == OauthTypeOidc || oauthType == OauthTypeGoogle {
		issuerURL, err := url.Parse(issuer)
		if err != nil || (issuerURL.Scheme != "http" && issuerURL.Scheme != "https") ||
			issuerURL.Hostname() == "" || issuerURL.User != nil || strings.ContainsAny(issuer, "?#") {
			return errors.New("OIDC issuer must be an absolute HTTP or HTTPS URL without user information, a query, or a fragment")
		}
		oa.Issuer = issuer
	}
	if proxyURL, err := ParseOauthProxyURL(oa.ProxyURL); err != nil {
		return err
	} else if proxyURL != nil {
		oa.ProxyURL = proxyURL.String()
	} else {
		oa.ProxyURL = ""
	}
	if oa.PkceEnable == nil {
		oa.PkceEnable = new(bool)
		*oa.PkceEnable = false
	}
	if oa.PkceMethod == "" {
		oa.PkceMethod = PKCEMethodS256
	} else if oa.PkceMethod != PKCEMethodS256 && oa.PkceMethod != PKCEMethodPlain {
		return errors.New("PKCE method must be S256 or plain")
	}
	return nil
}

type OauthUser struct {
	OpenId        string `json:"open_id" gorm:"not null;index"`
	Name          string `json:"name"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email,omitempty"`
	Picture       string `json:"picture,omitempty"`
}

func (ou *OauthUser) ToUser(user *User) {
	user.Email = strings.ToLower(strings.TrimSpace(ou.Email))
	user.Nickname = strings.TrimSpace(ou.Name)
	if user.Nickname == "" {
		user.Nickname = strings.TrimSpace(ou.Username)
	}
	if user.Nickname == "" {
		user.Nickname = strings.SplitN(user.Email, "@", 2)[0]
	}
	user.Nickname = string([]rune(user.Nickname)[:min(len([]rune(user.Nickname)), 32)])
}

type OauthUserBase struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type OidcUser struct {
	OauthUserBase
	Sub               string `json:"sub"`
	VerifiedEmail     bool   `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
}

func (ou *OidcUser) ToOauthUser() *OauthUser {
	username := strings.TrimSpace(ou.PreferredUsername)
	if username == "" {
		email := strings.ToLower(strings.TrimSpace(ou.Email))
		username = strings.SplitN(email, "@", 2)[0]
	}

	return &OauthUser{
		OpenId:        ou.Sub,
		Name:          ou.Name,
		Username:      username,
		Email:         ou.Email,
		VerifiedEmail: ou.VerifiedEmail,
		Picture:       ou.Picture,
	}
}

type GithubUser struct {
	OauthUserBase
	Id            int    `json:"id"`
	Login         string `json:"login"`
	AvatarUrl     string `json:"avatar_url"`
	VerifiedEmail bool   `json:"verified_email"`
}

func (gu *GithubUser) ToOauthUser() *OauthUser {
	username := strings.ToLower(gu.Login)
	return &OauthUser{
		OpenId:        strconv.Itoa(gu.Id),
		Name:          gu.Name,
		Username:      username,
		Email:         gu.Email,
		VerifiedEmail: gu.VerifiedEmail,
		Picture:       gu.AvatarUrl,
	}
}

type LinuxdoUser struct {
	OauthUserBase
	Id       int    `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar_url"`
}

func (lu *LinuxdoUser) ToOauthUser() *OauthUser {
	return &OauthUser{
		OpenId:        strconv.Itoa(lu.Id),
		Name:          lu.Name,
		Username:      strings.ToLower(lu.Username),
		Email:         lu.Email,
		VerifiedEmail: true, // linux.do 用户邮箱默认已验证
		Picture:       lu.Avatar,
	}
}

type OauthList struct {
	Oauths      []*Oauth `json:"list"`
	RedirectURL string   `json:"redirect_url"`
	Pagination
}
