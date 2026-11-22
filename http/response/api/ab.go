package api

import "github.com/lejianwen/rustdesk-api/v2/model"

type AbList struct {
	Peers     []*model.AddressBook `json:"peers"`
	Tags      []string             `json:"tags"`
	TagColors string               `json:"tag_colors,omitempty"`
}
