package admin

type PeerForm struct {
	RowId uint    `json:"row_id"`
	Alias *string `json:"alias"`
}

type PeerBatchDeleteForm struct {
	RowIds []uint `json:"row_ids" validate:"required"`
}

type PeerQuery struct {
	PageQuery
	CollectionId *uint  `json:"collection_id" form:"collection_id"`
	TimeAgo      int    `json:"time_ago" form:"time_ago"`
	Id           string `json:"id" form:"id"`
	Hostname     string `json:"hostname" form:"hostname"`
	Uuids        string `json:"uuids" form:"uuids"`
	Ip           string `json:"ip" form:"ip"`
	Username     string `json:"username" form:"username"`
	Alias        string `json:"alias" form:"alias"`
}

type SimpleDataQuery struct {
	Ids []string `json:"ids" form:"ids"`
}
