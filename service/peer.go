package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PeerService struct {
}

func (ps *PeerService) InfoByRowId(id uint) *model.Peer {
	p := &model.Peer{}
	DB.Where("row_id = ?", id).First(p)
	return p
}

func (ps *PeerService) bindDeviceUser(tx *gorm.DB, deviceId, uuid model.DeviceIdentity, userId uint) error {
	fullIdentity := userId != 0 && deviceId != "" && uuid != ""
	// PostgreSQL row locks cannot see another transaction's new UUID for this ID.
	if fullIdentity && tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", deviceId).Error; err != nil {
			return err
		}
	}
	if err := lockDeviceIdentity(tx, deviceId, uuid); err != nil {
		return err
	}
	if !fullIdentity {
		return tx.Model(&model.Peer{}).Where("id = ? AND uuid = ?", deviceId, uuid).Update("user_id", userId).Error
	}
	if tx.Dialector.Name() == "mysql" {
		// Take the shared ID range before locking the individual device row.
		var rows []uint
		if err := tx.Model(&model.Peer{}).Where("id = ?", deviceId).Order("row_id").
			Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Pluck("row_id", &rows).Error; err != nil {
			return err
		}
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}, {Name: "uuid"}},
		DoUpdates: clause.Assignments(map[string]any{"user_id": userId, "updated_at": tx.NowFunc()}),
	}).Create(&model.Peer{Id: deviceId, Uuid: uuid, UserId: userId}).Error; err != nil {
		return err
	}
	// Lock all candidates, including unconfirmed rows inserted by other sign-ins.
	var confirmed []bool
	if err := tx.Model(&model.Peer{}).Where("id = ?", deviceId).
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Pluck("identity_confirmed", &confirmed).Error; err != nil {
		return err
	}
	if slices.Contains(confirmed, true) {
		return nil
	}
	return tx.Model(&model.Peer{}).Where("id = ? AND uuid = ?", deviceId, uuid).
		UpdateColumn("identity_confirmed", true).Error
}

func lockDeviceIdentity(tx *gorm.DB, deviceID, uuid model.DeviceIdentity) error {
	// PostgreSQL row locks cannot reserve an identity before its first report.
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), hashtext(?))", deviceID, uuid).Error
}

// RecordHeartbeat updates the matching device at most once every 30 seconds.
func (ps *PeerService) RecordHeartbeat(deviceId, uuid model.DeviceIdentity, ip string, now int64) {
	DB.Model(&model.Peer{}).
		Where("id = ? AND uuid = ? AND last_online_time <= ?", deviceId, uuid, now-30).
		Updates(map[string]any{"last_online_time": now, "last_online_ip": ip})
}

// EraseUserId 清除用户id, 用于用户删除
func (ps *PeerService) EraseUserId(userId uint) error {
	return DB.Model(&model.Peer{}).Where("user_id = ?", userId).Update("user_id", 0).Error
}

// Prefer identities associated by authenticated login or an explicit admin registration.
// Preserve discovery of reported identities until the ID has any confirmed candidate.
func preferredPeerIdentity(table string) string {
	return fmt.Sprintf(`(%s.identity_confirmed = TRUE OR NOT EXISTS (
		SELECT 1 FROM peers AS confirmed_peer WHERE confirmed_peer.id = %s.id AND confirmed_peer.identity_confirmed = TRUE
	))`, table, table)
}

var peerIDAmbiguous = "EXISTS (SELECT 1 FROM peers AS matching_peer WHERE matching_peer.id = ? AND " +
	preferredPeerIdentity("matching_peer") + " LIMIT 1 OFFSET 1)"

var currentDeviceID = clause.Column{Table: clause.CurrentTable, Name: "id"}

func uniquePeerIDs(tx *gorm.DB) *gorm.DB {
	return tx.Where(preferredPeerIdentity("peers")).Where("NOT "+peerIDAmbiguous, currentDeviceID)
}

// ListGroupPeers returns accessible devices that can be identified by ID alone.
func (ps *PeerService) ListGroupPeers(userIds []uint, page, pageSize uint) ([]*model.Peer, int64, error) {
	visible := uniquePeerIDs(DB.Model(&model.Peer{}).Where("user_id IN ?", userIds))
	var total int64
	if err := visible.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var peers []*model.Peer
	err := visible.Select("peers.id, peers.user_id, peers.group_id, peers.hostname, peers.os, peers.username").
		Order("peers.row_id").Scopes(Paginate(page, pageSize)).Find(&peers).Error
	return peers, total, err
}

func (ps *PeerService) SimpleData(ids []string) *model.PeerList {
	res := &model.PeerList{Pagination: model.Pagination{Page: 1, PageSize: 99999}}
	tx := uniquePeerIDs(DB.Model(&model.Peer{}).
		Select("peers.id", "peers.version").Where("peers.id IN ?", ids))
	tx.Count(&res.Total)
	tx.Order("peers.row_id").Scopes(Paginate(1, 99999)).Find(&res.Peers)
	return res
}

func (ps *PeerService) List(page, pageSize uint, where func(tx *gorm.DB)) *model.PeerList {
	return ps.list(page, pageSize, where, false)
}

func (ps *PeerService) ListWithAmbiguity(page, pageSize uint, where func(tx *gorm.DB)) *model.PeerList {
	return ps.list(page, pageSize, where, true)
}

func (ps *PeerService) list(page, pageSize uint, where func(tx *gorm.DB), includeAmbiguity bool) (res *model.PeerList) {
	res = &model.PeerList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.Peer{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	if includeAmbiguity {
		tx.Select("peers.*, (NOT "+preferredPeerIdentity("peers")+" OR "+peerIDAmbiguous+") AS ambiguous_id", currentDeviceID)
	}
	tx.Order("peers.row_id").Scopes(Paginate(page, pageSize)).Find(&res.Peers)
	if includeAmbiguity {
		for _, peer := range res.Peers {
			peer.DirectTarget = isDirectAddressBookTarget(string(peer.Id))
		}
	}
	return
}

// ListFilterByUserId 根据用户id过滤Peer列表
func (ps *PeerService) ListFilterByUserId(page, pageSize uint, where func(tx *gorm.DB), userId uint) (res *model.PeerList) {
	userWhere := func(tx *gorm.DB) {
		tx.Where("user_id = ?", userId)
		// 如果还有额外的筛选条件，执行它
		if where != nil {
			where(tx)
		}
	}
	return ps.List(page, pageSize, userWhere)
}

// Create 创建
func (ps *PeerService) Create(u *model.Peer, fields ...string) error {
	u.IdentityConfirmed = true
	err := withDeviceGroup(u.GroupId, func(tx *gorm.DB) error {
		return tx.Create(u).Error
	})
	if err != nil {
		if translator, ok := DB.Dialector.(gorm.ErrorTranslator); ok && errors.Is(translator.Translate(err), gorm.ErrDuplicatedKey) {
			// Confirm outside the failed insert's transaction. Keep the data
			// already on the device for every field omitted by the administrator.
			return withDeviceGroup(u.GroupId, func(tx *gorm.DB) error {
				confirmed := tx.Model(&model.Peer{}).
					Where("id = ? AND uuid = ? AND identity_confirmed = FALSE", u.Id, u.Uuid).
					Select(append(fields, "identity_confirmed")).UpdateColumns(u)
				if confirmed.Error != nil || confirmed.RowsAffected > 0 {
					return confirmed.Error
				}
				return gorm.ErrDuplicatedKey
			})
		}
	}
	return err
}

// ReportSystemInfo updates the hardware fields for an exact device identity.
func (ps *PeerService) ReportSystemInfo(peer *model.Peer) error {
	updates := map[string]any{"updated_at": DB.NowFunc()}
	for field, value := range map[string]string{
		"cpu": peer.Cpu, "hostname": peer.Hostname, "memory": peer.Memory,
		"os": peer.Os, "username": peer.Username, "version": peer.Version,
	} {
		if value != "" {
			updates[field] = value
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockDeviceIdentity(tx, peer.Id, peer.Uuid); err != nil {
			return err
		}
		// Keep the upsert's device row lock while checking for active sessions.
		if err := tx.Omit("user_id", "identity_confirmed").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}, {Name: "uuid"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(peer).Error; err != nil {
			return err
		}
		device := tx.Model(&model.Peer{}).Where("id = ? AND uuid = ?", peer.Id, peer.Uuid)
		var current model.Peer
		if err := device.Session(&gorm.Session{}).Select("user_id").Take(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if current.UserId != 0 {
			return nil
		}
		// Logout could have started before the device row existed. Read the
		// sessions with a lock so we cannot reuse a token it is revoking.
		sessions := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate})
		userID, err := AllService.UserService.latestDeviceSessionUser(sessions, peer.Id, peer.Uuid, 0)
		if err != nil || userID == 0 {
			return err
		}
		return device.Where("user_id = 0").Update("user_id", userID).Error
	})
}

// Delete 删除, 同时也应该删除token
func (ps *PeerService) Delete(u *model.Peer) error {
	return ps.BatchDelete([]uint{u.RowId})
}

// BatchDelete 批量删除, 同时也应该删除token
func (ps *PeerService) BatchDelete(ids []uint) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		device := tx.Model(&model.Peer{}).Where("row_id IN ?", ids)
		// SQLite cannot lock a read row. Acquire its write lock before reading.
		if tx.Dialector.Name() == "sqlite" {
			if err := device.Session(&gorm.Session{}).UpdateColumn("user_id", gorm.Expr("user_id")).Error; err != nil {
				return err
			}
		}
		var peers []model.Peer
		if err := device.Session(&gorm.Session{}).Select("id", "uuid").Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Find(&peers).Error; err != nil {
			return err
		}
		if err := device.Delete(&model.Peer{}).Error; err != nil {
			return err
		}
		return deletePeerTokens(tx, peers...)
	})
}

// Update 更新
func (ps *PeerService) Update(u *model.Peer, fields ...string) error {
	updateID := slices.Contains(fields, "id")
	updateUUID := slices.Contains(fields, "uuid")
	update := func(tx *gorm.DB) error {
		if updateID || updateUUID {
			if tx.Dialector.Name() == "sqlite" {
				if err := tx.Model(&model.Peer{}).Where("row_id = ?", u.RowId).
					UpdateColumn("user_id", gorm.Expr("user_id")).Error; err != nil {
					return err
				}
			}
			var current model.Peer
			if err := tx.Select("id", "uuid").Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).First(&current, u.RowId).Error; err != nil {
				return err
			}
			identityChanged := (updateID && current.Id != u.Id) || (updateUUID && current.Uuid != u.Uuid)
			if identityChanged {
				if !updateID {
					u.Id = current.Id
				}
				if !updateUUID {
					u.Uuid = current.Uuid
				}
				u.IdentityConfirmed = u.HasIdentity()
				u.UserId = 0
				fields = append(fields, "identity_confirmed", "user_id")
			}
			if err := tx.Model(u).Select(fields).Updates(u).Error; err != nil {
				return err
			}
			if identityChanged {
				return deletePeerTokens(tx, current)
			}
			return nil
		}
		tx = tx.Model(u)
		if len(fields) != 0 {
			tx = tx.Select(fields)
		}
		return tx.Updates(u).Error
	}
	if (len(fields) == 0 || slices.Contains(fields, "group_id")) && u.GroupId != nil && *u.GroupId != 0 {
		return withDeviceGroup(u.GroupId, update)
	}
	if updateID || updateUUID {
		return DB.Transaction(update)
	}
	return update(DB)
}

// BatchUpdateTags replaces the account tags for a set of devices.
func (ps *PeerService) BatchUpdateTags(peers []*model.Peer, tags []string) error {
	if len(peers) == 0 {
		return gorm.ErrRecordNotFound
	}
	if tags == nil {
		tags = []string{}
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	ids := make([]uint, 0, len(peers))
	expectedOwners := make(map[uint]uint, len(peers))
	owners := make([]uint, 0, len(peers))
	for _, peer := range peers {
		if peer == nil || peer.RowId == 0 {
			return gorm.ErrRecordNotFound
		}
		if _, exists := expectedOwners[peer.RowId]; exists {
			return gorm.ErrDuplicatedKey
		}
		ids = append(ids, peer.RowId)
		expectedOwners[peer.RowId] = peer.UserId
		owners = append(owners, peer.UserId)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, owners...); err != nil {
			return err
		}
		var current []model.Peer
		if err := tx.Model(&model.Peer{}).Select("row_id", "user_id").Where("row_id IN ?", ids).
			Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Find(&current).Error; err != nil {
			return err
		}
		if len(current) != len(expectedOwners) {
			return gorm.ErrRecordNotFound
		}
		checked := make(map[uint]bool, len(owners))
		for _, peer := range current {
			if expectedOwners[peer.RowId] != peer.UserId {
				return gorm.ErrRecordNotFound
			}
			if !checked[peer.UserId] {
				if err := validateAccountTags(tx, peer.UserId, encoded); err != nil {
					return err
				}
				checked[peer.UserId] = true
			}
		}
		return tx.Model(&model.Peer{}).Where("row_id IN ?", ids).Update("tags", encoded).Error
	})
}

func deletePeerTokens(tx *gorm.DB, peers ...model.Peer) error {
	identities := make([][]any, 0, len(peers))
	for _, peer := range peers {
		if peer.Id != "" || peer.Uuid != "" {
			identities = append(identities, []any{peer.Id, peer.Uuid})
		}
	}
	if len(identities) == 0 {
		return nil
	}
	return tx.Where("(device_id, device_uuid) IN ?", identities).Delete(&model.UserToken{}).Error
}
