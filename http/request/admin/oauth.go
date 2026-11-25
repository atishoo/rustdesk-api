package admin

import (
	"github.com/lejianwen/rustdesk-api/v2/model"
)

type BindOauthForm struct {
	Op          string `json:"op" binding:"required"`
	RedirectURL string `json:"redirect_url"`
}

type OauthConfirmForm struct {
	Code string `json:"code" binding:"required"`
}
type UnBindOauthForm struct {
	Op           string `json:"op" binding:"required"`
	LdapPassword string `json:"ldap_password"`
}
type OauthForm struct {
	Id           uint    `json:"id"`
	Op           string  `json:"op" validate:"omitempty"`
	OauthType    string  `json:"oauth_type"`
	Issuer       *string `json:"issuer"`
	ProxyURL     *string `json:"proxy_url"`
	Scopes       *string `json:"scopes" validate:"omitempty"`
	ClientId     *string `json:"client_id"`
	ClientSecret *string `json:"client_secret"`
	AutoRegister *bool   `json:"auto_register"`
	PkceEnable   *bool   `json:"pkce_enable"`
	PkceMethod   *string `json:"pkce_method"`
	// 修改客户端凭据时提交列表或详情返回的凭据修订号。
	CredentialRevision string `json:"credential_revision"`
}

func (of *OauthForm) ToOauth() *model.Oauth {
	oa := &model.Oauth{}
	oa.Id = of.Id
	of.Apply(oa)
	return oa
}

func (of *OauthForm) Apply(oa *model.Oauth) {
	if of.Op != "" {
		oa.Op = of.Op
	}
	if of.OauthType != "" {
		oa.OauthType = of.OauthType
	}
	if of.Issuer != nil {
		oa.Issuer = *of.Issuer
	}
	if of.Scopes != nil {
		oa.Scopes = *of.Scopes
	}
	if of.ProxyURL != nil {
		oa.ProxyURL = *of.ProxyURL
	}
	if of.ClientId != nil {
		oa.ClientId = *of.ClientId
	}
	if of.ClientSecret != nil {
		oa.ClientSecret = *of.ClientSecret
	}
	if of.AutoRegister != nil {
		oa.AutoRegister = of.AutoRegister
	}
	if of.PkceEnable != nil {
		oa.PkceEnable = of.PkceEnable
	}
	if of.PkceMethod != nil {
		oa.PkceMethod = *of.PkceMethod
	}
}
