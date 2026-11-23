package model

import "github.com/lejianwen/rustdesk-api/v2/model/custom_types"

// IPLocation 保存运维命令中明确 IP 的归属地快照，不作为离线查询缓存。
type IPLocation struct {
	IP        string                `json:"ip" gorm:"primaryKey;size:45;not null;comment:规范化的纯IP地址，不含端口或IPv6区域标识"`
	Location  string                `json:"location" gorm:"type:text;not null;comment:运维命令执行时计算的IP归属地快照"`
	UpdatedAt custom_types.AutoTime `json:"updated_at" gorm:"type:timestamp;comment:IP归属地快照最近更新时间"`
}
