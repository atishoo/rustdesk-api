package admin

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/lejianwen/rustdesk-api/v2/model"
)

type AddressBookForm struct {
	RowId   uint                    `json:"row_id"`
	Id      model.AddressBookTarget `json:"id" validate:"required"`
	UserId  uint                    `json:"user_id"`
	UserIds []uint                  `json:"user_ids"`
	AddressBookFields
}

type AddressBookFields struct {
	Username         string   `json:"username" `
	Password         string   `json:"password" `
	Hostname         string   `json:"hostname" `
	Alias            string   `json:"alias" `
	Platform         string   `json:"platform" `
	Tags             []string `json:"tags"`
	Hash             string   `json:"hash"`
	ForceAlwaysRelay bool     `json:"forceAlwaysRelay"`
	RdpPort          string   `json:"rdpPort"`
	RdpUsername      string   `json:"rdpUsername"`
	Online           bool     `json:"online"`
	LoginName        string   `json:"loginName" `
	SameServer       bool     `json:"sameServer"`
	CollectionId     uint     `json:"collection_id"`
}

type AddressBookUpdateForm struct {
	RowId  uint                    `json:"row_id" validate:"required"`
	Id     model.AddressBookTarget `json:"id"`
	UserId uint                    `json:"user_id"`
	AddressBookFields
	supplied map[string]json.RawMessage
}

// PersonalAddressBookUpdateForm documents the owner required for personal updates.
type PersonalAddressBookUpdateForm struct {
	AddressBookUpdateForm
	UserId uint `json:"user_id" validate:"required"`
}

func (f *AddressBookUpdateForm) UnmarshalJSON(data []byte) error {
	type decoded AddressBookUpdateForm
	if err := json.Unmarshal(data, (*decoded)(f)); err != nil {
		return err
	}
	return json.Unmarshal(data, &f.supplied)
}

func (f *AddressBookUpdateForm) includes(field string) bool {
	value, found := f.supplied[field]
	return found && string(value) != "null"
}

func (f *AddressBookUpdateForm) ToUpdate(current *model.AddressBook) (*model.AddressBook, []string) {
	patch := (AddressBookForm{
		RowId: f.RowId, Id: f.Id, UserId: f.UserId, AddressBookFields: f.AddressBookFields,
	}).ToAddressBook()
	if !f.includes("id") {
		patch.Id = current.Id
	}
	if !f.includes("user_id") {
		patch.UserId = current.UserId
	}
	if !f.includes("collection_id") {
		patch.CollectionId = current.CollectionId
	}
	var fields []string
	for key, column := range map[string]string{
		"id": "id", "username": "username", "password": "password", "hostname": "hostname",
		"alias": "alias", "platform": "platform", "tags": "tags", "hash": "hash",
		"user_id": "user_id", "collection_id": "collection_id", "online": "online",
		"forceAlwaysRelay": "force_always_relay", "rdpPort": "rdp_port",
		"rdpUsername": "rdp_username", "loginName": "login_name", "sameServer": "same_server",
	} {
		if f.includes(key) {
			fields = append(fields, column)
		}
	}
	return patch, fields
}

func (a AddressBookForm) ToAddressBook() *model.AddressBook {
	//tags转换
	tags, _ := json.Marshal(a.Tags)

	return &model.AddressBook{
		RowId:            a.RowId,
		Id:               a.Id,
		Username:         a.Username,
		Password:         a.Password,
		Hostname:         a.Hostname,
		Alias:            a.Alias,
		Platform:         a.Platform,
		Tags:             tags,
		Hash:             a.Hash,
		UserId:           a.UserId,
		ForceAlwaysRelay: a.ForceAlwaysRelay,
		RdpPort:          a.RdpPort,
		RdpUsername:      a.RdpUsername,
		Online:           a.Online,
		LoginName:        a.LoginName,
		SameServer:       a.SameServer,
		CollectionId:     a.CollectionId,
	}

}
func (a AddressBookForm) ToAddressBooks() []*model.AddressBook {
	//tags转换
	tags, _ := json.Marshal(a.Tags)

	abs := make([]*model.AddressBook, 0, len(a.UserIds))
	for _, userId := range a.UserIds {
		abs = append(abs, &model.AddressBook{
			RowId:            a.RowId,
			Id:               a.Id,
			Username:         a.Username,
			Password:         a.Password,
			Hostname:         a.Hostname,
			Alias:            a.Alias,
			Platform:         a.Platform,
			Tags:             tags,
			Hash:             a.Hash,
			UserId:           userId,
			ForceAlwaysRelay: a.ForceAlwaysRelay,
			RdpPort:          a.RdpPort,
			RdpUsername:      a.RdpUsername,
			Online:           a.Online,
			LoginName:        a.LoginName,
			SameServer:       a.SameServer,
			CollectionId:     a.CollectionId,
		})
	}
	return abs
}

type AddressBookQuery struct {
	UserId       int    `form:"user_id"`
	CollectionId *int   `form:"collection_id"`
	IsMy         int    `form:"is_my"`
	Username     string `form:"username"`
	Hostname     string `form:"hostname"`
	Id           string `form:"id"`
	PageQuery
}

type ShareByWebClientForm struct {
	Id       model.AddressBookTarget `json:"id" validate:"required"`
	RowId    uint                    `json:"row_id" validate:"required,gt=0" minimum:"1"`
	Password string                  `json:"password" validate:"required"`
	Expire   int64                   `json:"expire" validate:"oneof=0 300 1800 3600 86400"`
}

func (sbwcf ShareByWebClientForm) HasValidPassword() bool {
	// Match browser atob, which ignores ASCII whitespace and accepts omitted padding.
	encoded := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\f', '\r':
			return -1
		}
		return r
	}, sbwcf.Password)
	encoding := base64.StdEncoding
	if len(encoded)%4 != 0 {
		encoding = base64.RawStdEncoding
	}
	decoded, err := encoding.DecodeString(encoded)
	return err == nil && len(decoded) != 0 && utf8.Valid(decoded)
}

func (sbwcf ShareByWebClientForm) ToShareRecord(source model.ShareSource) *model.ShareRecord {
	return &model.ShareRecord{
		UserId:           0,
		Source:           source,
		PeerId:           sbwcf.Id,
		AddressBookRowId: sbwcf.RowId,
		Password:         sbwcf.Password,
		Expire:           sbwcf.Expire,
	}
}

type AddressBookCollectionQuery struct {
	UserId int `form:"user_id"`
	IsMy   int `form:"is_my"`
	PageQuery
}

type AddressBookCollectionSimpleListQuery struct {
	UserIds []uint `form:"user_ids"`
}
type BatchCreateFromPeersForm struct {
	CollectionId uint     `json:"collection_id"`
	PeerIds      []uint   `json:"peer_ids"`
	Tags         []string `json:"tags"`
	UserId       uint     `json:"user_id"`
}
type BatchUpdateTagsForm struct {
	RowIds []uint   `json:"row_ids"`
	Tags   []string `json:"tags"`
}
