package admin

import "github.com/lejianwen/rustdesk-api/v2/model"

type PeerForm struct {
	RowId    uint                  `json:"row_id" `
	Id       model.DeviceIdentity  `json:"id"`
	Cpu      *string               `json:"cpu"`
	Hostname *string               `json:"hostname"`
	Memory   *string               `json:"memory"`
	Os       *string               `json:"os"`
	Username *string               `json:"username"`
	Uuid     *model.DeviceIdentity `json:"uuid"`
	Version  *string               `json:"version"`
	GroupId  *uint                 `json:"group_id"`
	Alias    *string               `json:"alias"`
}

type PeerBatchDeleteForm struct {
	RowIds []uint `json:"row_ids" validate:"required"`
}

// ToPeer returns the device and the fields supplied for an update.
func (f *PeerForm) ToPeer() (*model.Peer, []string) {
	peer := &model.Peer{RowId: f.RowId, Id: f.Id, GroupId: f.GroupId, Alias: f.Alias}
	var fields []string
	if f.Id != "" {
		fields = append(fields, "id")
	}
	if f.GroupId != nil {
		fields = append(fields, "group_id")
	}
	if f.Alias != nil {
		fields = append(fields, "alias")
	}
	if f.Uuid != nil {
		peer.Uuid = *f.Uuid
		fields = append(fields, "uuid")
	}
	for _, field := range []struct {
		name   string
		input  *string
		output *string
	}{
		{"cpu", f.Cpu, &peer.Cpu},
		{"hostname", f.Hostname, &peer.Hostname},
		{"memory", f.Memory, &peer.Memory},
		{"os", f.Os, &peer.Os},
		{"username", f.Username, &peer.Username},
		{"version", f.Version, &peer.Version},
	} {
		if field.input != nil {
			*field.output = *field.input
			fields = append(fields, field.name)
		}
	}
	return peer, fields
}

type PeerQuery struct {
	PageQuery
	TimeAgo  int    `json:"time_ago" form:"time_ago"`
	Id       string `json:"id" form:"id"`
	Hostname string `json:"hostname" form:"hostname"`
	Uuids    string `json:"uuids" form:"uuids"`
	Ip       string `json:"ip" form:"ip"`
	Username string `json:"username" form:"username"`
	Alias    string `json:"alias" form:"alias"`
}

type SimpleDataQuery struct {
	Ids []string `json:"ids" form:"ids"`
}
