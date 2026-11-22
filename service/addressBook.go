package service

import (
	"encoding/json"
	"errors"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDuplicatePeerID               = errors.New("DuplicatePeerID")
	ErrWebClientExplicitServerTarget = errors.New("WebClientExplicitServerTarget")
	ErrWebClientDirectTarget         = errors.New("WebClientDirectTarget")
	ErrWebClientTargetWhitespace     = errors.New("WebClientTargetWhitespace")
	ErrWebClientEmptyTarget          = errors.New("WebClientEmptyTarget")
	ErrUnsupportedAddressBookPeerID  = errors.New("UnsupportedAddressBookPeerID")
)

func hasAddressBookRelaySuffix(id string) bool {
	return strings.HasSuffix(id, "/r") || strings.HasSuffix(id, `\r`)
}

// Keep these patterns aligned with the native client's is_ipv4_str,
// is_ipv6_str, and is_domain_port_str routing checks.
const (
	directIPv4Octet = `(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)`
	directIPv6Host  = `(?:[a-fA-F0-9]{1,4}:{1,2})+[a-fA-F0-9]{1,4}`
)

var (
	directAddressBookIPv4   = regexp.MustCompile(`^` + directIPv4Octet + `\.` + directIPv4Octet + `\.` + directIPv4Octet + `\.` + directIPv4Octet + `(:\p{Nd}+)?$`)
	directAddressBookIPv6   = regexp.MustCompile(`^(?:` + directIPv6Host + `|\[` + directIPv6Host + `\]:\p{Nd}+)$`)
	directAddressBookDomain = regexp.MustCompile(`(?i)^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z-]{0,61}[a-z]:\p{Nd}{1,5}$`)
)

func isDirectAddressBookTarget(id string) bool {
	if directAddressBookIPv4.MatchString(id) || directAddressBookIPv6.MatchString(id) || directAddressBookDomain.MatchString(id) {
		return true
	}
	host := id
	if strings.HasPrefix(id, "[") && strings.HasSuffix(id, "]") {
		host = id[1 : len(id)-1]
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() && ip.Zone() == "" {
		return true
	}
	if endpoint, err := netip.ParseAddrPort(id); err == nil && endpoint.Addr().Is6() {
		if endpoint.Addr().Zone() == "" {
			return true
		}
		// Rust's SocketAddrV6 accepts numeric scope IDs.
		_, err := strconv.ParseUint(endpoint.Addr().Zone(), 10, 32)
		return err == nil
	}
	return false
}

func localAddressBookDeviceID(target model.AddressBookTarget) (model.DeviceIdentity, bool) {
	id, serverKey, explicitServer := strings.Cut(string(target), "@")
	if explicitServer {
		server, _, _ := strings.Cut(serverKey, "?")
		configured := Config.Capylink.IdServer
		if server == "" || server == "public" || configured == "" || addressBookServerKey(server) != addressBookServerKey(configured) {
			return "", false
		}
	}
	if hasAddressBookRelaySuffix(id) {
		id = id[:len(id)-2]
	}
	if !explicitServer && isDirectAddressBookTarget(id) {
		return "", true
	}
	return model.DeviceIdentity(id), false
}

// addressBookServerKey is used only to compare server addresses.
func addressBookServerKey(server string) string {
	if strings.Contains(server, "://") {
		endpoint, err := url.Parse(server)
		if err != nil || endpoint.Hostname() == "" || endpoint.User != nil {
			return server
		}
		port := endpoint.Port()
		switch endpoint.Scheme {
		case "ws":
			if port == "" {
				port = "80"
			}
		case "wss":
			if port == "" {
				port = "443"
			}
		default:
			return server
		}
		endpoint.Host = net.JoinHostPort(addressBookServerHost(endpoint.Hostname()), port)
		if endpoint.Path == "" {
			endpoint.Path = "/"
		}
		return endpoint.String()
	}

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host = server
		if strings.Contains(host, ":") {
			if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
				host = host[1 : len(host)-1]
			}
			if _, err := netip.ParseAddr(host); err != nil {
				return server
			}
		}
		port = strconv.Itoa(config.DefaultIdServerPort)
	}
	return net.JoinHostPort(addressBookServerHost(host), port)
}

func addressBookServerHost(host string) string {
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() {
		return ip.String()
	}
	return strings.ToLower(host)
}

type AddressBookService struct {
}

func (s *AddressBookService) Info(id string) *model.AddressBook {
	p := &model.AddressBook{}
	DB.Where("id = ?", id).First(p)
	return p
}

func (s *AddressBookService) InfoByUserIdAndId(userid uint, id model.AddressBookTarget) *model.AddressBook {
	p := &model.AddressBook{}
	DB.Where("user_id = ? and id = ?", userid, id).First(p)
	return p
}

func (s *AddressBookService) InfoByUserIdAndIdAndCid(userid uint, id model.AddressBookTarget, cid uint) *model.AddressBook {
	p := &model.AddressBook{}
	addressBookEntries(DB, userid, id, cid).First(p)
	return p
}

func addressBookEntries(db *gorm.DB, userID uint, deviceID model.AddressBookTarget, collectionID uint) *gorm.DB {
	return db.Model(&model.AddressBook{}).Where("user_id = ? AND id = ? AND collection_id = ?", userID, deviceID, collectionID)
}

func lockAddressBookOwners(tx *gorm.DB, userIDs ...uint) error {
	// Lock all owners in the same order before establishing a read snapshot.
	slices.Sort(userIDs)
	userIDs = slices.Compact(userIDs)
	for _, id := range userIDs {
		if err := tx.Model(&model.User{}).Where("id = ?", id).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
	}
	var count int64
	if err := tx.Model(&model.User{}).Where("id IN ?", userIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(userIDs)) {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *AddressBookService) InfoByRowId(id uint) *model.AddressBook {
	p := &model.AddressBook{}
	DB.Where("row_id = ?", id).First(p)
	return p
}
func (s *AddressBookService) ListByUserId(userId, page, pageSize uint) (res *model.AddressBookList) {
	res = s.List(page, pageSize, func(tx *gorm.DB) {
		tx.Where("user_id = ?", userId)
	})
	return
}
func (s *AddressBookService) ListByUserIds(userIds []uint, page, pageSize uint) (res *model.AddressBookList) {
	res = s.List(page, pageSize, func(tx *gorm.DB) {
		tx.Where("user_id in (?)", userIds)
	})
	return
}

// AddAddressBook adds an entry once even when the native client retries.
func (s *AddressBookService) AddAddressBook(ab *model.AddressBook) error {
	err := s.createAddressBook(ab, true)
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil
	}
	return err
}

func (s *AddressBookService) createAddressBook(ab *model.AddressBook, fillMetadata bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, ab.UserId); err != nil {
			return err
		}
		inserted, err := s.insertAddressBook(tx, ab, fillMetadata)
		if err == nil && !inserted {
			return gorm.ErrDuplicatedKey
		}
		return err
	})
}

// insertAddressBook returns false without an error when the target already exists.
func (s *AddressBookService) insertAddressBook(tx *gorm.DB, ab *model.AddressBook, fillMetadata bool) (bool, error) {
	var existing model.AddressBook
	err := addressBookEntries(tx, ab.UserId, ab.Id, ab.CollectionId).Select("row_id").Take(&existing).Error
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if err := validateAccountTags(tx, ab.UserId, ab.Tags); err != nil {
		return false, err
	}
	if fillMetadata {
		s.fillPeerMetadata(tx, ab)
	}
	err = tx.Create(ab).Error
	return err == nil, err
}

func (s *AddressBookService) fillPeerMetadata(db *gorm.DB, ab *model.AddressBook) {
	if ab.Platform != "" && ab.Username != "" && ab.Hostname != "" {
		return
	}
	deviceID, _ := localAddressBookDeviceID(ab.Id)
	if deviceID == "" {
		return
	}
	var peers []model.Peer
	if err := db.Select("os", "username", "hostname").Where("id = ?", deviceID).
		Where(preferredPeerIdentity("peers")).Limit(2).Find(&peers).Error; err != nil || len(peers) != 1 {
		return
	}
	peer := peers[0]
	if ab.Platform == "" {
		ab.Platform = s.PlatformFromOs(peer.Os)
	}
	if ab.Username == "" {
		ab.Username = peer.Username
	}
	if ab.Hostname == "" {
		ab.Hostname = peer.Hostname
	}
}

// UpdateAddressBook replaces the personal address book and its tags together.
func (s *AddressBookService) UpdateAddressBook(abs []*model.AddressBook, userId uint, tagNames []string, tagColors map[string]uint) error {
	tags := make(map[string]struct{}, len(tagNames))
	for _, name := range tagNames {
		tags[name] = struct{}{}
	}
	entries := make(map[model.AddressBookTarget]*model.AddressBook, len(abs))
	for _, ab := range abs {
		if ab == nil {
			return errors.New("invalid address book entry")
		}
		peerTags, err := decodeAddressBookTags(ab.Tags)
		if err != nil {
			return err
		}
		for _, name := range peerTags {
			if _, exists := tags[name]; !exists {
				return ErrInvalidAddressBookTags
			}
		}
		entries[ab.Id] = ab
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, userId); err != nil {
			return err
		}
		var storedEntries []*model.AddressBook
		if err := tx.Where("user_id = ? AND collection_id = 0", userId).Find(&storedEntries).Error; err != nil {
			return err
		}
		stored := make(map[model.AddressBookTarget]*model.AddressBook, len(storedEntries))
		for _, ab := range storedEntries {
			stored[ab.Id] = ab
			if _, keep := entries[ab.Id]; !keep {
				if err := tx.Delete(ab).Error; err != nil {
					return err
				}
			}
		}
		for id, ab := range entries {
			entry := *ab
			entry.RowId = 0
			entry.UserId = userId
			entry.CollectionId = 0
			entry.Collection = nil
			if len(entry.Tags) == 0 {
				entry.Tags = []byte("[]")
			}
			if existing := stored[id]; existing != nil {
				if err := tx.Model(existing).Updates(&entry).Error; err != nil {
					return err
				}
				continue
			}
			s.fillPeerMetadata(tx, &entry)
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		return updatePersonalAddressBookTags(tx, userId, tags, tagColors)
	})
}

func (s *AddressBookService) List(page, pageSize uint, where func(tx *gorm.DB)) *model.AddressBookList {
	res, _ := s.list(page, pageSize, where)
	return res
}

func (s *AddressBookService) ListWithAmbiguity(page, pageSize uint, where func(tx *gorm.DB)) (*model.AddressBookList, error) {
	res, err := s.list(page, pageSize, where)
	if err != nil {
		return nil, err
	}
	return res, markAddressBookAmbiguity(res.AddressBooks)
}

func (s *AddressBookService) list(page, pageSize uint, where func(tx *gorm.DB)) (*model.AddressBookList, error) {
	res := &model.AddressBookList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.AddressBook{})
	if where != nil {
		where(tx)
	}
	if err := tx.Count(&res.Total).Error; err != nil {
		return res, err
	}
	err := tx.Scopes(Paginate(page, pageSize)).Find(&res.AddressBooks).Error
	return res, err
}

func markAddressBookAmbiguity(entries []*model.AddressBook) error {
	byDeviceID := make(map[model.DeviceIdentity][]*model.AddressBook)
	for _, entry := range entries {
		entry.AmbiguousID = new(bool)
		deviceID, direct := localAddressBookDeviceID(entry.Id)
		entry.DirectTarget = direct
		if deviceID != "" {
			byDeviceID[deviceID] = append(byDeviceID[deviceID], entry)
		}
	}
	ambiguousIDs, err := ambiguousPeerIDs(DB, slices.Collect(maps.Keys(byDeviceID)))
	if err != nil {
		return err
	}
	for _, id := range ambiguousIDs {
		for _, entry := range byDeviceID[id] {
			*entry.AmbiguousID = true
		}
	}
	return nil
}

func ambiguousPeerIDs(db *gorm.DB, ids []model.DeviceIdentity) ([]model.DeviceIdentity, error) {
	var ambiguous []model.DeviceIdentity
	for batch := range slices.Chunk(ids, 500) {
		var matches []model.DeviceIdentity
		if err := db.Model(&model.Peer{}).Where("id IN ?", batch).
			Where(preferredPeerIdentity("peers")).
			Group("id").Having("COUNT(*) > 1").Pluck("id", &matches).Error; err != nil {
			return nil, err
		}
		ambiguous = append(ambiguous, matches...)
	}
	return ambiguous, nil
}

func (s *AddressBookService) FromPeer(peer *model.Peer) (a *model.AddressBook) {
	a = &model.AddressBook{}
	a.Id = model.AddressBookTarget(peer.Id)
	a.Username = peer.Username
	a.Hostname = peer.Hostname
	a.UserId = peer.UserId
	a.Platform = s.PlatformFromOs(peer.Os)
	return a
}

func (s *AddressBookService) BatchCreateFromPeers(peers []*model.Peer, userID, collectionID uint, tags []string) error {
	ids := make([]model.DeviceIdentity, 0, len(peers))
	rowIDs := make([]uint, 0, len(peers))
	selected := make(map[uint]*model.Peer, len(peers))
	pending := make(map[model.AddressBookTarget]struct{}, len(peers))
	for _, peer := range peers {
		rawID := string(peer.Id)
		if strings.Contains(rawID, "@") || hasAddressBookRelaySuffix(rawID) || isDirectAddressBookTarget(rawID) {
			return ErrUnsupportedAddressBookPeerID
		}
		id := model.AddressBookTarget(peer.Id)
		if _, found := pending[id]; found {
			return ErrDuplicatePeerID
		}
		pending[id] = struct{}{}
		ids = append(ids, peer.Id)
		rowIDs = append(rowIDs, peer.RowId)
		selected[peer.RowId] = peer
	}
	slices.Sort(rowIDs)
	encodedTags, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, userID); err != nil {
			return err
		}
		if err := validateAccountTags(tx, userID, encodedTags); err != nil {
			return err
		}
		for batch := range slices.Chunk(rowIDs, 500) {
			var current []model.Peer
			if err := tx.Model(&model.Peer{}).Where("row_id IN ?", batch).
				Select("row_id, id, uuid, user_id, (NOT "+preferredPeerIdentity("peers")+" OR "+peerIDAmbiguous+") AS ambiguous_id", currentDeviceID).
				Order("row_id").Clauses(clause.Locking{Strength: clause.LockingStrengthShare}).Find(&current).Error; err != nil {
				return err
			}
			if len(current) != len(batch) {
				return gorm.ErrRecordNotFound
			}
			for _, peer := range current {
				original := selected[peer.RowId]
				if peer.Id != original.Id || peer.Uuid != original.Uuid || peer.UserId != original.UserId {
					return gorm.ErrRecordNotFound
				}
				if peer.AmbiguousID != nil && *peer.AmbiguousID {
					return ErrDuplicatePeerID
				}
			}
		}
		var existing []model.AddressBookTarget
		if err := tx.Model(&model.AddressBook{}).
			Where("user_id = ? AND collection_id = ? AND id IN ?", userID, collectionID, ids).
			Pluck("id", &existing).Error; err != nil {
			return err
		}
		for _, id := range existing {
			delete(pending, id)
		}
		entries := make([]*model.AddressBook, 0, len(pending))
		for _, peer := range peers {
			if _, found := pending[model.AddressBookTarget(peer.Id)]; found {
				entry := s.FromPeer(peer)
				entry.UserId = userID
				entry.CollectionId = collectionID
				entry.Tags = encodedTags
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return nil
		}
		return tx.CreateInBatches(&entries, 100).Error
	})
}

// Create 创建
func (s *AddressBookService) Create(u *model.AddressBook) error {
	return s.createAddressBook(u, false)
}

func (s *AddressBookService) BatchCreate(entries []*model.AddressBook) error {
	ownerIDs := make([]uint, 0, len(entries))
	for _, entry := range entries {
		if entry.UserId != 0 {
			ownerIDs = append(ownerIDs, entry.UserId)
		}
	}
	if len(ownerIDs) == 0 {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, ownerIDs...); err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.UserId == 0 {
				continue
			}
			if _, err := s.insertAddressBook(tx, entry, false); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *AddressBookService) Delete(u *model.AddressBook) error {
	return DB.Delete(u).Error
}

// Update 更新
func (s *AddressBookService) Update(u *model.AddressBook) error {
	return DB.Model(u).Updates(u).Error
}

func updateAddressBookEntry(tx *gorm.DB, current *model.AddressBook, values any, fields []string) error {
	query := addressBookEntries(tx, current.UserId, current.Id, current.CollectionId).Where("row_id = ?", current.RowId)
	update := query.Session(&gorm.Session{})
	if len(fields) > 0 {
		update = update.Select(fields)
	}
	result := update.Updates(values)
	if result.Error != nil || result.RowsAffected != 0 {
		return result.Error
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateByMap 更新
func (s *AddressBookService) UpdateByMap(u *model.AddressBook, data map[string]interface{}) error {
	encoded, ok := data["tags"].([]byte)
	if !ok {
		return updateAddressBookEntry(DB, u, data, nil)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, u.UserId); err != nil {
			return err
		}
		if err := validateAccountTags(tx, u.UserId, encoded); err != nil {
			return err
		}
		return updateAddressBookEntry(tx, u, data, nil)
	})
}

// UpdateFields changes the supplied columns while the entry still has the checked identity.
func (s *AddressBookService) UpdateFields(current, patch *model.AddressBook, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	moving := current.UserId != patch.UserId || current.CollectionId != patch.CollectionId
	targetChanged := slices.Contains(fields, "id") && current.Id != patch.Id
	identityChanged := moving || targetChanged
	tagsChanged := slices.Contains(fields, "tags")
	if !identityChanged && !tagsChanged {
		return updateAddressBookEntry(DB, current, patch, fields)
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, current.UserId, patch.UserId); err != nil {
			return err
		}
		if identityChanged {
			var duplicate model.AddressBook
			err := addressBookEntries(tx, patch.UserId, patch.Id, patch.CollectionId).
				Where("row_id <> ?", current.RowId).Select("row_id").Take(&duplicate).Error
			if err == nil {
				return gorm.ErrDuplicatedKey
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if moving || tagsChanged {
			tags := patch.Tags
			if !tagsChanged {
				var stored model.AddressBook
				if err := addressBookEntries(tx, current.UserId, current.Id, current.CollectionId).
					Where("row_id = ?", current.RowId).Select("tags").Take(&stored).Error; err != nil {
					return err
				}
				tags = stored.Tags
			}
			if err := validateAccountTags(tx, patch.UserId, tags); err != nil {
				return err
			}
		}
		if targetChanged {
			if !slices.Contains(fields, "hash") {
				patch.Hash = ""
				fields = append(fields, "hash")
			}
			if !slices.Contains(fields, "password") {
				patch.Password = ""
				fields = append(fields, "password")
			}
		}
		return updateAddressBookEntry(tx, current, patch, fields)
	})
}

func shareableWebClientDeviceID(target model.AddressBookTarget) (model.DeviceIdentity, error) {
	id := string(target)
	if strings.Contains(id, "@") {
		return "", ErrWebClientExplicitServerTarget
	}
	// The bundled Web Client trims the input and removes ASCII spaces before connecting.
	if strings.ContainsRune(id, ' ') || strings.TrimFunc(id, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' }) != id {
		return "", ErrWebClientTargetWhitespace
	}
	deviceID, direct := localAddressBookDeviceID(target)
	if direct {
		return "", ErrWebClientDirectTarget
	}
	if deviceID == "" {
		return "", ErrWebClientEmptyTarget
	}
	return deviceID, nil
}

// ShareableEntry returns the matching address book row when its ID can be shared.
func (s *AddressBookService) ShareableEntry(rowID, userID uint, target model.AddressBookTarget) (*model.AddressBook, error) {
	deviceID, err := shareableWebClientDeviceID(target)
	if err != nil {
		return nil, err
	}
	entry := &model.AddressBook{}
	query := DB.Where("row_id = ? AND user_id = ? AND id = ?", rowID, userID, target).
		Where("NOT "+peerIDAmbiguous, deviceID)
	if err := query.First(entry).Error; err != nil {
		return nil, err
	}
	return entry, nil
}

// ShareablePeer returns a current-account device row when it can be shared directly.
func (s *AddressBookService) ShareablePeer(rowID, userID uint, target model.AddressBookTarget) (*model.Peer, error) {
	deviceID, err := shareableWebClientDeviceID(target)
	if err != nil {
		return nil, err
	}
	peer := &model.Peer{}
	query := DB.Where("row_id = ? AND user_id = ? AND id = ?", rowID, userID, target).
		Where("NOT "+peerIDAmbiguous, deviceID)
	if err := query.First(peer).Error; err != nil {
		return nil, err
	}
	return peer, nil
}

// ShareByWebClient 分享
func (s *AddressBookService) ShareByWebClient(m *model.ShareRecord) error {
	m.ShareToken = uuid.New().String()
	return DB.Create(m).Error
}

// SharedPeer
func (s *AddressBookService) SharedPeer(shareToken string) *model.ShareRecord {
	m := &model.ShareRecord{}
	DB.Where("share_token = ?", shareToken).First(m)
	return m
}

// PlatformFromOs
func (s *AddressBookService) PlatformFromOs(os string) string {
	if strings.Contains(os, "Android") || strings.Contains(os, "android") {
		return "Android"
	}
	if strings.Contains(os, "Windows") || strings.Contains(os, "windows") {
		return "Windows"
	}
	if strings.Contains(os, "Linux") || strings.Contains(os, "linux") {
		return "Linux"
	}
	if strings.Contains(os, "mac") || strings.Contains(os, "Mac") {
		return "Mac OS"
	}
	return ""
}
func (s *AddressBookService) ListByUserIdAndCollectionId(userId, cid, page, pageSize uint) (res *model.AddressBookList) {
	res = s.List(page, pageSize, func(tx *gorm.DB) {
		tx.Where("user_id = ? and collection_id = ?", userId, cid)
	})
	return
}
func (s *AddressBookService) ListCollection(page, pageSize uint, where func(tx *gorm.DB)) (res *model.AddressBookCollectionList) {
	res = &model.AddressBookCollectionList{}
	res.Page = int64(page)
	res.PageSize = int64(pageSize)
	tx := DB.Model(&model.AddressBookCollection{})
	if where != nil {
		where(tx)
	}
	tx.Count(&res.Total)
	tx.Scopes(Paginate(page, pageSize))
	tx.Find(&res.AddressBookCollection)
	return
}
func (s *AddressBookService) CollectionInfoById(id uint) *model.AddressBookCollection {
	p := &model.AddressBookCollection{}
	DB.Where("id = ?", id).First(p)
	return p
}

func (s *AddressBookService) CreateCollection(t *model.AddressBookCollection) error {
	return DB.Create(t).Error
}

func (s *AddressBookService) UpdateCollection(t *model.AddressBookCollection) error {
	return DB.Model(t).Update("name", t.Name).Error
}

func (s *AddressBookService) DeleteCollection(t *model.AddressBookCollection) error {
	//删除集合下的地址簿，再删除集合
	tx := DB.Begin()
	tx.Where("collection_id = ?", t.Id).Delete(&model.AddressBook{})
	tx.Delete(t)
	return tx.Commit().Error
}

// CheckCollectionOwner 检查Collection的所有者
func (s *AddressBookService) CheckCollectionOwner(uid uint, cid uint) bool {
	p := s.CollectionInfoById(cid)
	return p.UserId == uid
}

func (s *AddressBookService) BatchUpdateTags(abs []*model.AddressBook, tags []string) error {
	ids := make([]uint, 0, len(abs))
	owners := make([]uint, 0, len(abs))
	original := make(map[uint]*model.AddressBook, len(abs))
	for _, ab := range abs {
		ids = append(ids, ab.RowId)
		owners = append(owners, ab.UserId)
		original[ab.RowId] = ab
	}
	tagsv, _ := json.Marshal(tags)
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAddressBookOwners(tx, owners...); err != nil {
			return err
		}
		var entries []model.AddressBook
		if err := tx.Select("row_id", "user_id", "collection_id").Where("row_id IN ?", ids).Find(&entries).Error; err != nil {
			return err
		}
		if len(entries) != len(original) {
			return gorm.ErrRecordNotFound
		}
		checked := make(map[[2]uint]bool)
		for _, entry := range entries {
			expected := original[entry.RowId]
			if expected.UserId != entry.UserId || expected.CollectionId != entry.CollectionId {
				return gorm.ErrRecordNotFound
			}
			scope := [2]uint{entry.UserId, entry.CollectionId}
			if !checked[scope] {
				if err := validateAccountTags(tx, entry.UserId, tagsv); err != nil {
					return err
				}
				checked[scope] = true
			}
		}
		return tx.Model(&model.AddressBook{}).Where("row_id IN ?", ids).Update("tags", tagsv).Error
	})
}
