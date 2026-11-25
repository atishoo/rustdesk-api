package model

import "strings"

const LdapSettingID = 1

// LdapSetting stores the single LDAP directory configuration used by the API.
type LdapSetting struct {
	Id           uint            `gorm:"primaryKey;comment:LDAP配置主键" json:"id"`
	Revision     uint64          `gorm:"not null;default:0;comment:LDAP配置修订号" json:"revision"`
	Enable       bool            `gorm:"not null;comment:是否启用LDAP" json:"enable"`
	Url          string          `gorm:"type:text;comment:LDAP服务器地址" json:"url"`
	TlsCaFile    string          `gorm:"type:text;comment:LDAP TLS CA文件" json:"tls_ca_file"`
	TlsVerify    bool            `gorm:"not null;comment:是否校验LDAP TLS证书" json:"tls_verify"`
	BaseDn       string          `gorm:"type:text;comment:LDAP基础DN" json:"base_dn"`
	BindDn       string          `gorm:"type:text;comment:LDAP绑定DN" json:"bind_dn"`
	BindPassword string          `gorm:"type:text;comment:LDAP绑定密码" json:"-"`
	User         LdapUserSetting `gorm:"embedded;embeddedPrefix:user_" json:"user"`
}

// LdapAccountVerification tracks pending directory verification for a first administrator.
type LdapAccountVerification struct {
	Email    string `gorm:"primaryKey;size:254;comment:待复核 LDAP 账号的主邮箱"`
	Revision string `gorm:"not null;size:36;comment:LDAP 账号复核的随机修订标识"`
}

// LdapUserSetting stores LDAP user attribute and group mapping options.
type LdapUserSetting struct {
	BaseDn          string `gorm:"type:text;comment:LDAP用户搜索基础DN" json:"base_dn"`
	EnableAttr      string `gorm:"type:text;comment:LDAP用户启用属性" json:"enable_attr"`
	EnableAttrValue string `gorm:"type:text;comment:LDAP用户启用属性值" json:"enable_attr_value"`
	Filter          string `gorm:"type:text;comment:LDAP用户过滤器" json:"filter"`
	Email           string `gorm:"type:text;comment:LDAP邮箱属性" json:"email"`
	FirstName       string `gorm:"type:text;comment:LDAP名属性" json:"first_name"`
	LastName        string `gorm:"type:text;comment:LDAP姓属性" json:"last_name"`
	Sync            bool   `gorm:"not null;comment:是否同步LDAP用户资料和权限" json:"sync"`
	AdminGroup      string `gorm:"type:text;comment:LDAP管理员组" json:"admin_group"`
	AllowGroup      string `gorm:"type:text;comment:LDAP允许登录组" json:"allow_group"`
}

func (s LdapUserSetting) UsesADAccountControl() bool {
	return strings.EqualFold(s.EnableAttr, "userAccountControl")
}
