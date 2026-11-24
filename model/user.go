package model

type NativeLoginCredentials struct {
	Email    string `json:"email" validate:"required,email,lte=254" label:"邮箱"`
	Password string `json:"password,omitempty" validate:"required" label:"密码"`
}

type User struct {
	IdModel
	Email      string     `json:"email" gorm:"size:254;not null;uniqueIndex;comment:账号唯一登录邮箱"`
	Password   string     `json:"-" gorm:"default:'';not null;"`
	Nickname   string     `json:"nickname" gorm:"size:32;not null;comment:用户显示昵称"`
	Avatar     string     `json:"avatar" gorm:"default:'';not null;comment:本地上传头像公开路径，空值使用文字头像"`
	GroupId    uint       `json:"group_id" gorm:"default:0;not null;index"`
	LdapSynced bool       `json:"-" gorm:"default:false;not null;comment:是否由 LDAP 目录映射"`
	IsAdmin    *bool      `json:"is_admin" gorm:"default:0;not null;"`
	Status     StatusCode `json:"status" gorm:"default:1;not null;"`
	Remark     string     `json:"remark" gorm:"default:'';not null;"`
	TimeModel
}

type UserList struct {
	Users []*User `json:"list,omitempty"`
	Pagination
}

var UserRouteNames = []string{
	"MyTagList", "MyAddressBookList", "MyInfo", "MyAddressBookCollection", "MyPeer", "MyShareRecordList", "MyLoginLog",
}
var AdminRouteNames = []string{"*"}
