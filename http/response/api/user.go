package api

import "github.com/lejianwen/rustdesk-api/v2/model"

type UserPayload struct {
	Id       uint                   `json:"id,omitempty"`
	Nickname string                 `json:"nickname"`
	Avatar   string                 `json:"avatar"`
	Email    string                 `json:"email"`
	Note     string                 `json:"note"`
	IsAdmin  *bool                  `json:"is_admin"`
	Status   int                    `json:"status"`
	Info     map[string]interface{} `json:"info"`
}

func (up *UserPayload) FromUser(user *model.User) *UserPayload {
	up.Nickname = user.Nickname
	up.Avatar = user.Avatar
	up.Email = user.Email
	up.IsAdmin = user.IsAdmin
	up.Status = int(user.Status)
	up.Info = map[string]interface{}{}
	return up
}

func (up *UserPayload) FromCurrentUser(user *model.User) *UserPayload {
	up.Id = user.Id
	return up.FromUser(user)
}

/*
	class HttpType {
	  static const kAuthReqTypeAccount = "account";
	  static const kAuthReqTypeMobile = "mobile";
	  static const kAuthReqTypeSMSCode = "sms_code";
	  static const kAuthReqTypeEmailCode = "email_code";
	  static const kAuthReqTypeTfaCode = "tfa_code";

	  static const kAuthResTypeToken = "access_token";
	  static const kAuthResTypeEmailCheck = "email_check";
	  static const kAuthResTypeTfaCheck = "tfa_check";
	}
*/
type LoginRes struct {
	Type        string      `json:"type"`
	AccessToken string      `json:"access_token"`
	User        UserPayload `json:"user"`
	Secret      string      `json:"secret,omitempty"`
	TfaType     string      `json:"tfa_type,omitempty"`
}
