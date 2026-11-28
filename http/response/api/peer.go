package api

import "github.com/lejianwen/rustdesk-api/v2/model"

type GroupPeerPayload struct {
	Id           string           `json:"id"`
	RowId        uint             `json:"row_id"`
	Alias        string           `json:"alias"`
	CanEditAlias bool             `json:"can_edit_alias"`
	Info         *PeerPayloadInfo `json:"info"`
	Status       int              `json:"status"`
	User         string           `json:"user"`
	UserEmail    string           `json:"user_email"`
	Note         string           `json:"note"`
}
type PeerPayloadInfo struct {
	DeviceName string `json:"device_name"`
	Os         string `json:"os"`
	Username   string `json:"username"`
}

func (gpp *GroupPeerPayload) FromAddressBook(a *model.AddressBook, email string) {
	gpp.Id = string(a.Id)
	os := a.Platform
	if a.Platform == "Mac OS" {
		os = "MacOS"
	}
	gpp.Info = &PeerPayloadInfo{
		DeviceName: a.Hostname,
		Os:         os,
		Username:   a.Username,
	}
	gpp.UserEmail = email
}

func (gpp *GroupPeerPayload) FromPeer(p *model.Peer, email string) {
	gpp.Id = string(p.Id)
	gpp.RowId = p.RowId
	if p.Alias != nil {
		gpp.Alias = *p.Alias
	}
	gpp.Info = &PeerPayloadInfo{
		DeviceName: p.Hostname,
		Os:         p.Os,
		Username:   p.Username,
	}
	gpp.Note = ""
	gpp.UserEmail = email
}
