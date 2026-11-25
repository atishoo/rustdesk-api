package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/google/uuid"

	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxLdapCAFileSize = 4 << 20
	ldapEntryUUID     = "entryUUID"
	ldapObjectGUID    = "objectGUID"
)

var (
	ErrUrlParseFailed        = errors.New("UrlParseFailed")
	ErrFileReadFailed        = errors.New("FileReadFailed")
	ErrLdapNotEnabled        = errors.New("LdapNotEnabled")
	ErrLdapUserDisabled      = errors.New("UserDisabledAtLdap")
	ErrLdapUserNotFound      = errors.New("UserNotFound")
	ErrLdapMailNotMatch      = errors.New("MailNotMatch")
	ErrLdapConnectFailed     = errors.New("LdapConnectFailed")
	ErrLdapSearchFailed      = errors.New("LdapSearchRequestFailed")
	ErrLdapTlsFailed         = errors.New("LdapStartTLSFailed")
	ErrLdapBindService       = errors.New("LdapBindServiceFailed")
	ErrLdapBindFailed        = errors.New("LdapBindFailed")
	ErrLdapToLocalUserFailed = errors.New("LdapToLocalUserFailed")
	ErrLdapCreateUserFailed  = errors.New("LdapCreateUserFailed")
	ErrLdapPasswordNotMatch  = errors.New("PasswordNotMatch")
)

// LdapService is responsible for LDAP authentication and user synchronization.
type LdapService struct {
	deadline time.Time
}

func newLdapRequest() *LdapService {
	return &LdapService{deadline: time.Now().Add(5 * time.Second)}
}

func ldapAllowsLocalAdminFallback(err error) bool {
	return errors.Is(err, ErrLdapSearchFailed) || errors.Is(err, ErrLdapUserNotFound)
}

// LdapUser represents the user attributes retrieved from LDAP.
type LdapUser struct {
	Dn              string
	Email           string
	FirstName       string
	LastName        string
	MemberOf        []string
	EnableAttrValue string
	Enabled         bool

	passwordVerified    bool
	identity            ldapEntryIdentity
	memberOfRefreshed   bool
	adminGroupMember    *bool
	adminDenialRecorded bool // The current membership denial was already saved.
	disabledConfirmed   bool
}

type ldapEntryIdentity struct {
	entryUUID  uuid.UUID
	objectGUID uuid.UUID
}

func ldapIdentity(entry *ldap.Entry) ldapEntryIdentity {
	var identity ldapEntryIdentity
	if values := entry.GetEqualFoldAttributeValues(ldapEntryUUID); len(values) == 1 {
		identity.entryUUID, _ = uuid.Parse(values[0])
	}
	if values := entry.GetEqualFoldRawAttributeValues(ldapObjectGUID); len(values) == 1 {
		identity.objectGUID, _ = uuid.FromBytes(values[0])
	}
	return identity
}

func (identity ldapEntryIdentity) compare(other ldapEntryIdentity) (known, same bool) {
	entryUUIDComparable := identity.entryUUID != uuid.Nil && other.entryUUID != uuid.Nil
	objectGUIDComparable := identity.objectGUID != uuid.Nil && other.objectGUID != uuid.Nil
	known = entryUUIDComparable || objectGUIDComparable
	same = known && (!entryUUIDComparable || identity.entryUUID == other.entryUUID) &&
		(!objectGUIDComparable || identity.objectGUID == other.objectGUID)
	return known, same
}

// Name returns the full name of an LDAP user.
func (lu *LdapUser) Name() string {
	return fmt.Sprintf("%s %s", lu.FirstName, lu.LastName)
}

func (lu *LdapUser) mappedEmail() string {
	return strings.ToLower(strings.TrimSpace(lu.Email))
}

func (lu *LdapUser) directGroupMembership(groupDN string) *bool {
	member := false
	if groupDN == "" {
		return &member
	}
	hasGroups := false
	for _, group := range lu.MemberOf {
		if strings.TrimSpace(group) == "" {
			continue
		}
		hasGroups = true
		if strings.EqualFold(group, groupDN) {
			member = true
			return &member
		}
	}
	if hasGroups {
		return &member
	}
	return nil
}

// ToUser merges the LdapUser data into a provided *model.User.
// If 'u' is nil, it creates and returns a new *model.User.
func (lu *LdapUser) ToUser(u *model.User) *model.User {
	if u == nil {
		u = &model.User{}
	}
	u.Email = lu.mappedEmail()
	u.Nickname = strings.TrimSpace(lu.Name())
	if u.Nickname == "" {
		u.Nickname = strings.SplitN(u.Email, "@", 2)[0]
	}
	u.Nickname = string([]rune(u.Nickname)[:min(len([]rune(u.Nickname)), 32)])
	if lu.Enabled {
		u.Status = model.COMMON_STATUS_ENABLE
	} else {
		u.Status = model.COMMON_STATUS_DISABLED
	}
	return u
}

// connectAndBind creates an LDAP connection, optionally starts TLS, and then binds using the provided credentials.
func (ls *LdapService) connectAndBind(cfg *model.LdapSetting, username, password string) (*ldap.Conn, error) {
	u, err := url.Parse(cfg.Url)
	if err != nil {
		return nil, errors.Join(ErrUrlParseFailed, err)
	}

	var dialOptions []ldap.DialOpt
	if ls != nil && !ls.deadline.IsZero() {
		dialOptions = append(dialOptions, ldap.DialWithDialer(&net.Dialer{Deadline: ls.deadline}))
	}
	if u.Scheme == "ldaps" {
		// WARNING: InsecureSkipVerify: true is not recommended for production
		tlsConfig := &tls.Config{InsecureSkipVerify: !cfg.TlsVerify}
		if cfg.TlsCaFile != "" {
			caCert, err := readLdapCAFile(cfg.TlsCaFile)
			if err != nil {
				return nil, errors.Join(ErrFileReadFailed, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, errors.Join(ErrLdapTlsFailed, errors.New("failed to append CA certificate"))
			}
			tlsConfig.RootCAs = caCertPool
		}
		dialOptions = append(dialOptions, ldap.DialWithTLSConfig(tlsConfig))
	}

	conn, err := ldap.DialURL(cfg.Url, dialOptions...)
	if err != nil {
		return nil, errors.Join(ErrLdapConnectFailed, err)
	}

	if err := ls.setRequestTimeout(conn); err != nil {
		conn.Close()
		return nil, err
	}
	// Bind as the "service" user
	if err = conn.Bind(username, password); err != nil {
		conn.Close()
		return nil, errors.Join(ErrLdapBindService, err)
	}
	return conn, nil
}

func readLdapCAFile(path string) ([]byte, error) {
	// Do not wait for a FIFO writer when opening the path to check its type.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("LDAP CA path must refer to a regular file")
	}
	if info.Size() == 0 {
		return nil, errors.New("LDAP CA file is empty")
	}
	if info.Size() > maxLdapCAFileSize {
		return nil, errors.New("LDAP CA file exceeds the size limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxLdapCAFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLdapCAFileSize {
		return nil, errors.New("LDAP CA file exceeds the size limit")
	}
	return data, nil
}

func (ls *LdapService) setRequestTimeout(conn *ldap.Conn) error {
	if ls == nil || ls.deadline.IsZero() {
		return nil
	}
	remaining := time.Until(ls.deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	conn.SetTimeout(remaining)
	return nil
}

// connectAndBindAdmin creates an LDAP connection, optionally starts TLS, and then binds using the admin credentials.
func (ls *LdapService) connectAndBindAdmin(cfg *model.LdapSetting) (*ldap.Conn, error) {
	return ls.connectAndBind(cfg, cfg.BindDn, cfg.BindPassword)
}

// verifyCredentials checks the provided username and password against LDAP.
func (ls *LdapService) verifyCredentials(cfg *model.LdapSetting, username, password string) error {
	ldapConn, err := ls.connectAndBind(cfg, username, password)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return ErrLdapPasswordNotMatch
		}
		return err
	}
	defer ldapConn.Close()
	return nil
}

func (ls *LdapService) loginUser(cfg *model.LdapSetting, email, password string) (*LdapUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidateEmailAddress(email) || password == "" {
		return nil, ErrLdapPasswordNotMatch
	}
	ldapUser, err := ls.getUserInfoByEmailLdap(cfg, email)
	if err != nil {
		return nil, err
	}
	if err := revokeLdapAccess(cfg, ldapUser); err != nil {
		return nil, err
	}
	if !ldapUser.Enabled {
		return nil, ErrLdapUserDisabled
	}
	// Use the service account to check admission before attempting a user bind.
	directAdmin := ldapUser.adminGroupMember != nil && *ldapUser.adminGroupMember
	requireAdmin := cfg.User.AllowGroup != "" && directAdmin
	if cfg.User.AllowGroup != "" && !directAdmin {
		allowed, allowErr := ls.isUserInGroup(cfg, ldapUser, cfg.User.AllowGroup, false)
		if !allowed {
			requireAdmin = true
			isAdmin, err := ls.isUserAdmin(cfg, ldapUser, false)
			if err != nil {
				return nil, err
			}
			if !isAdmin {
				if err := recordLdapAdmin(cfg, ldapUser, isAdmin); err != nil {
					return nil, err
				}
				if allowErr != nil {
					return nil, allowErr
				}
				return nil, errors.New("user not in allowed group")
			}
		}
	}
	if err := ls.verifyCredentials(cfg, ldapUser.Dn, password); err != nil {
		return nil, err
	}
	ldapUser.passwordVerified = true
	// Confirm any administrator exception and capture the role after the bind.
	if cfg.User.Sync || requireAdmin {
		isAdmin, err := ls.isUserAdmin(cfg, ldapUser, true)
		if err != nil {
			return nil, err
		}
		if err := recordLdapAdmin(cfg, ldapUser, isAdmin); err != nil {
			return nil, err
		}
		if !isAdmin && cfg.User.AllowGroup != "" && (requireAdmin || ldapUser.memberOfRefreshed) {
			if requireAdmin && !directAdmin {
				return nil, errors.New("user not in allowed group")
			}
			// Prefer the refreshed membership. A missing list still requires a group lookup.
			var allowed bool
			if membership := ldapUser.directGroupMembership(cfg.User.AllowGroup); ldapUser.memberOfRefreshed && membership != nil {
				allowed = *membership
			} else {
				allowed, err = ls.isUserInGroup(cfg, ldapUser, cfg.User.AllowGroup, false)
				if err != nil {
					return nil, err
				}
			}
			if !allowed {
				return nil, errors.New("user not in allowed group")
			}
		}
	} else {
		// Unsynchronized accounts only need a fresh role when first mapped.
		ldapUser.adminGroupMember = nil
	}
	return ldapUser, nil
}

func (ls *LdapService) isUserInGroup(cfg *model.LdapSetting, ldapUser *LdapUser, groupDN string, verifyReverse bool) (bool, error) {
	if member := ldapUser.directGroupMembership(groupDN); member != nil && *member {
		return true, nil
	}
	allowed, err := ls.searchGroupMember(cfg, ldapUser, groupDN)
	if err == nil && allowed && verifyReverse {
		_, err = ls.readSameGroupEntry(cfg, ldapUser)
	}
	return allowed && err == nil, err
}

func (ls *LdapService) searchGroupMember(cfg *model.LdapSetting, ldapUser *LdapUser, groupDN string) (bool, error) {
	// For "member" attribute, perform a reverse search on the group
	member := "member"
	userDN := ldap.EscapeFilter(ldapUser.Dn)
	groupFilter := fmt.Sprintf("(%s=%s)", member, userDN)

	// Create the LDAP search request
	groupSearchRequest := ldap.NewSearchRequest(
		groupDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,     // Unlimited search results
		0,     // No time limit
		false, // Return both attributes and DN
		groupFilter,
		[]string{"dn"},
		nil,
	)

	// Perform the group search
	groupResult, err := ls.searchResult(cfg, groupSearchRequest)
	if err != nil {
		if !errors.Is(err, ErrLdapBindService) && ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return false, nil
		}
		return false, err
	}

	// If any results are returned, the user is part of the group
	return len(groupResult.Entries) > 0, nil
}

// prepareLdapUser collects the user values before a write transaction is started.
func (ls *LdapService) prepareLdapUser(cfg *model.LdapSetting, lu *LdapUser) (*model.User, error) {
	email := lu.mappedEmail()
	if !ValidateEmailAddress(email) {
		return nil, errors.New("InvalidEmail")
	}
	if !cfg.User.Sync {
		var existing model.User
		err := DB.Where("email = ?", email).First(&existing).Error
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	user, err := ls.localUserValues(cfg, lu)
	if err != nil {
		return nil, err
	}
	user.GroupId = model.DefaultUserGroupID
	return user, nil
}

func recordLdapAdmin(cfg *model.LdapSetting, lu *LdapUser, isAdmin bool) error {
	lu.adminGroupMember = &isAdmin
	return revokeLdapAccess(cfg, lu)
}

// Revocations must remain effective even if a later session or binding write fails.
func revokeLdapAccess(cfg *model.LdapSetting, lu *LdapUser) error {
	email := lu.mappedEmail()
	if strings.TrimSpace(lu.Dn) == "" || !ValidateEmailAddress(email) {
		return nil
	}
	updates := make(map[string]any, 3)
	if lu.disabledConfirmed {
		updates["status"] = model.COMMON_STATUS_DISABLED
	}
	demote := lu.adminGroupMember != nil && !*lu.adminGroupMember && !lu.adminDenialRecorded
	if demote {
		updates["is_admin"] = false
	}
	if len(updates) == 0 {
		return nil
	}
	trackUnmapped := cfg.User.AdminGroup != ""
	if !cfg.User.Sync && !trackUnmapped {
		return nil
	}
	updates["ldap_permission_revision"] = gorm.Expr("ldap_permission_revision + 1")
	updateMapped := func(tx *gorm.DB) *gorm.DB {
		user := tx.Model(&model.User{}).Where("email = ?", email)
		if !lu.disabledConfirmed && !trackUnmapped {
			user = user.Where("is_admin = ?", true)
		}
		return user.UpdateColumns(updates)
	}
	write := func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthShare); err != nil {
			return err
		}
		if cfg.User.Sync {
			result := updateMapped(tx)
			if result.Error != nil || result.RowsAffected != 0 || !trackUnmapped {
				return result.Error
			}
		}
		revision, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		result := tx.Model(&model.LdapAccountVerification{}).Where("email = ?", email).
			UpdateColumn("revision", revision.String())
		if result.Error == nil && result.RowsAffected == 0 && cfg.User.Sync {
			// Creation may have consumed the verification while this transaction waited.
			return updateMapped(tx).Error
		}
		return result.Error
	}
	for attempt := 1; ; attempt++ {
		err := DB.Transaction(write)
		if err == nil {
			lu.adminDenialRecorded = lu.adminDenialRecorded || demote
			return nil
		}
		if attempt == 3 || !isMySQLDeadlock(err) {
			return err
		}
	}
}

type ldapRecheckRequired struct {
	account          *model.User
	unmappedRevision string
}

func (*ldapRecheckRequired) Error() string {
	return "LDAP privileges require verification"
}

func registerLdapAccountVerification(cfg *model.LdapSetting, email string) (string, error) {
	revision, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	verification := model.LdapAccountVerification{Email: email, Revision: revision.String()}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockLdapRevision(tx, cfg.Revision, clause.LockingStrengthShare); err != nil {
			return err
		}
		var existing model.User
		err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Select("id").Where("email = ?", email).Take(&existing).Error
		if err == nil {
			return errors.New("LDAP account was created before verification")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&verification).Error; err != nil {
			return err
		}
		return tx.Where("email = ?", email).Take(&verification).Error
	})
	return verification.Revision, err
}

func withLdapRecheck(cfg *model.LdapSetting, directoryUser *LdapUser, prepared *model.User, commit func(*ldapRecheckRequired) error) error {
	err := commit(nil)
	var required *ldapRecheckRequired
	if !errors.As(err, &required) {
		return err
	}
	if required.account.Id == 0 {
		revision, err := registerLdapAccountVerification(cfg, prepared.Email)
		if err != nil {
			return err
		}
		required.unmappedRevision = revision
	}
	revoked, err := verifyLdapPrivilegeIncrease(cfg, directoryUser, prepared)
	if revoked != nil {
		if revokeErr := revokeLdapAccess(cfg, revoked); revokeErr != nil {
			return revokeErr
		}
	}
	if err != nil {
		return errors.Join(ErrLdapToLocalUserFailed, err)
	}
	return commit(required)
}

func mapLdapUser(tx *gorm.DB, cfg *model.LdapSetting, prepared *model.User, checked *ldapRecheckRequired) (*model.User, error) {
	localUser := &model.User{}
	err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where("email = ?", prepared.Email).First(localUser).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && prepared.Id == 0 && (checked == nil || checked.account.Id == 0) {
		if checked == nil && prepared.IsAdmin != nil && *prepared.IsAdmin {
			return nil, &ldapRecheckRequired{account: localUser}
		}
		pending := tx.Where("email = ?", prepared.Email)
		if checked != nil {
			pending = pending.Where("revision = ?", checked.unmappedRevision)
		}
		result := pending.Delete(&model.LdapAccountVerification{})
		if result.Error != nil {
			return nil, result.Error
		}
		if checked != nil && result.RowsAffected != 1 {
			return nil, errors.New("LDAP permissions changed during first account verification")
		}
		created := *prepared
		if err := tx.Create(&created).Error; err != nil {
			return nil, errors.Join(ErrLdapCreateUserFailed, err)
		}
		return &created, nil
	}
	if err != nil {
		return nil, err
	}
	if err := updateLdapUser(tx, localUser, prepared, checked, cfg.User.Sync); err != nil {
		return nil, err
	}
	return localUser, nil
}

// Recheck permissions outside the database transaction.
func verifyLdapPrivilegeIncrease(cfg *model.LdapSetting, original *LdapUser, prepared *model.User) (*LdapUser, error) {
	directory := newLdapRequest()
	fresh, err := directory.getUserInfoByEmailLdap(cfg, prepared.Email)
	if err != nil {
		return nil, err
	}
	_, sameEntry := original.identity.compare(fresh.identity)
	if !sameEntry || fresh.Dn == "" || fresh.Dn != original.Dn || fresh.mappedEmail() != prepared.Email {
		return nil, ErrLdapMailNotMatch
	}
	if prepared.Status == model.COMMON_STATUS_ENABLE && !fresh.Enabled {
		return fresh, ErrLdapUserDisabled
	}
	if prepared.IsAdmin != nil && *prepared.IsAdmin {
		admin, err := directory.isUserAdmin(cfg, fresh, true)
		if err != nil {
			return nil, err
		}
		if !admin {
			fresh.adminGroupMember = &admin
			return fresh, errors.New("LDAP administrator membership changed")
		}
	} else if original.passwordVerified && cfg.User.AllowGroup != "" {
		allowed, err := directory.isUserInGroup(cfg, fresh, cfg.User.AllowGroup, true)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, errors.New("user not in allowed group")
		}
	}
	return nil, nil
}

func updateLdapUser(db *gorm.DB, localUser, directoryUser *model.User, proof *ldapRecheckRequired, sync bool) error {
	localAdmin := localUser.IsAdmin != nil && *localUser.IsAdmin
	if proof != nil {
		checked := proof.account
		checkedAdmin := checked.IsAdmin != nil && *checked.IsAdmin
		if checked.Id != localUser.Id || checked.Email != localUser.Email || checked.Status != localUser.Status ||
			checkedAdmin != localAdmin || checked.LdapSynced != localUser.LdapSynced ||
			checked.LdapPermissionRevision != localUser.LdapPermissionRevision {
			return errors.New("LDAP account permissions changed during verification")
		}
	}
	if sync {
		if proof == nil &&
			((directoryUser.Status == model.COMMON_STATUS_ENABLE && localUser.Status != model.COMMON_STATUS_ENABLE) ||
				(directoryUser.IsAdmin != nil && *directoryUser.IsAdmin && !localAdmin)) {
			return &ldapRecheckRequired{account: localUser}
		}
		// Keep personal profile edits while applying directory permissions and revocation.
		if err := db.Model(localUser).Select("is_admin", "status", "ldap_synced").Updates(directoryUser).Error; err != nil {
			return err
		}
		return db.First(localUser, localUser.Id).Error
	}
	if !localUser.LdapSynced {
		if err := db.Model(localUser).Update("ldap_synced", true).Error; err != nil {
			return err
		}
		localUser.LdapSynced = true
	}
	return nil
}

func (ls *LdapService) localUserValues(cfg *model.LdapSetting, lu *LdapUser) (*model.User, error) {
	isAdmin := lu.adminGroupMember
	if isAdmin == nil {
		member, err := ls.isUserAdmin(cfg, lu, lu.passwordVerified)
		if err != nil && (cfg.User.Sync || errors.Is(err, ErrLdapMailNotMatch)) {
			return nil, err
		}
		isAdmin = &member
		if err == nil {
			lu.adminGroupMember = isAdmin
		}
	}
	user := lu.ToUser(nil)
	user.IsAdmin = isAdmin
	user.LdapSynced = true
	return user, nil
}

// IsEmailExists checks if an email exists in LDAP (can be useful for local registration checks).
func (ls *LdapService) IsEmailExists(email string) (bool, error) {
	cfg, err := GetLdapSetting()
	if err != nil {
		return false, err
	}
	return ls.isEmailExists(&cfg, email)
}

func (ls *LdapService) isEmailExists(cfg *model.LdapSetting, email string) (bool, error) {
	if !cfg.Enable {
		return false, nil
	}
	sr, err := ls.emailSearchResult(cfg, email)
	if err != nil {
		return false, err
	}
	return len(sr.Entries) > 0, nil
}

func (ls *LdapService) getUserInfoByEmailLdap(cfg *model.LdapSetting, email string) (*LdapUser, error) {
	if !cfg.Enable {
		return nil, ErrLdapNotEnabled
	}
	sr, err := ls.emailSearchResult(cfg, email)
	// Partial or ambiguous matches cannot establish that the email is outside the directory.
	if sr != nil && len(sr.Entries) > 0 {
		if err != nil {
			return nil, err
		}
		if len(sr.Entries) != 1 {
			return nil, ErrLdapMailNotMatch
		}
		return ls.userResultToLdapUser(cfg, sr.Entries[0]), nil
	}
	if err != nil {
		return nil, errors.Join(ErrLdapSearchFailed, err)
	}
	return nil, ErrLdapUserNotFound
}

// emailSearchResult returns the search result for the given email.
func (ls *LdapService) emailSearchResult(cfg *model.LdapSetting, email string) (*ldap.SearchResult, error) {
	filter := ls.filterField(ls.fieldEmail(cfg), email)
	searchRequest := ls.buildUserSearchRequest(cfg, filter)
	return ls.searchResult(cfg, searchRequest)
}

func (ls *LdapService) searchResult(cfg *model.LdapSetting, searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	ldapConn, err := ls.connectAndBindAdmin(cfg)
	if err != nil {
		return nil, err
	}
	defer ldapConn.Close()
	if err := ls.setRequestTimeout(ldapConn); err != nil {
		return nil, err
	}
	return ldapConn.Search(searchRequest)
}

// buildUserSearchRequest constructs an LDAP SearchRequest for users given a filter.
func (ls *LdapService) buildUserSearchRequest(cfg *model.LdapSetting, filter string) *ldap.SearchRequest {
	baseDn := ls.baseDnUser(cfg) // user-specific base DN, or fallback
	filterConfig := cfg.User.Filter
	if filterConfig == "" {
		filterConfig = "(cn=*)"
	}

	// Combine the default filter with our field filter, e.g. (&(cn=*)(uid=jdoe))
	combinedFilter := fmt.Sprintf("(&%s%s)", filterConfig, filter)

	attributes := ls.buildUserAttributes(cfg)

	return ldap.NewSearchRequest(
		baseDn,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,     // unlimited search results
		0,     // no server-side time limit
		false, // typesOnly
		combinedFilter,
		attributes,
		nil,
	)
}

// buildUserAttributes returns the list of attributes we want from LDAP user searches.
func (ls *LdapService) buildUserAttributes(cfg *model.LdapSetting) []string {
	attributes := []string{
		"dn",
		ls.fieldEmail(cfg),
		ls.fieldFirstName(cfg),
		ls.fieldLastName(cfg),
		ls.fieldMemberOf(),
		ldapEntryUUID,
		ldapObjectGUID,
	}
	if cfg.User.EnableAttr != "" {
		attributes = append(attributes, cfg.User.EnableAttr)
	}
	return attributes
}

// userResultToLdapUser maps an *ldap.Entry to our LdapUser struct.
func (ls *LdapService) userResultToLdapUser(cfg *model.LdapSetting, entry *ldap.Entry) *LdapUser {
	lu := &LdapUser{
		Dn:        entry.DN,
		Email:     entry.GetAttributeValue(ls.fieldEmail(cfg)),
		FirstName: entry.GetAttributeValue(ls.fieldFirstName(cfg)),
		LastName:  entry.GetAttributeValue(ls.fieldLastName(cfg)),
		MemberOf:  entry.GetEqualFoldAttributeValues(ls.fieldMemberOf()),
		identity:  ldapIdentity(entry),
	}
	if cfg.User.EnableAttr != "" {
		lu.EnableAttrValue = entry.GetEqualFoldAttributeValue(cfg.User.EnableAttr)
	}
	lu.adminGroupMember = lu.directGroupMembership(cfg.User.AdminGroup)
	// Check if the user is enabled based on the LDAP configuration
	ls.isUserEnabled(cfg, lu)
	return lu
}

// filterField helps build simple attribute filters, e.g. (uid=username).
func (ls *LdapService) filterField(field, value string) string {
	return fmt.Sprintf("(%s=%s)", field, ldap.EscapeFilter(value))
}

// fieldEmail returns the configured email attribute or "mail" if not set.
func (ls *LdapService) fieldEmail(cfg *model.LdapSetting) string {
	if cfg.User.Email == "" {
		return "mail"
	}
	return cfg.User.Email
}

// fieldFirstName returns the configured first name attribute or "givenName" if not set.
func (ls *LdapService) fieldFirstName(cfg *model.LdapSetting) string {
	if cfg.User.FirstName == "" {
		return "givenName"
	}
	return cfg.User.FirstName
}

// fieldLastName returns the configured last name attribute or "sn" if not set.
func (ls *LdapService) fieldLastName(cfg *model.LdapSetting) string {
	if cfg.User.LastName == "" {
		return "sn"
	}
	return cfg.User.LastName
}

func (ls *LdapService) fieldMemberOf() string {
	return "memberOf"
}

// baseDnUser returns the user-specific base DN or the global base DN if none is set.
func (ls *LdapService) baseDnUser(cfg *model.LdapSetting) string {
	if cfg.User.BaseDn == "" {
		return cfg.BaseDn
	}
	return cfg.User.BaseDn
}

// isUserAdmin checks if the user is a member of the admin group.
func (ls *LdapService) isUserAdmin(cfg *model.LdapSetting, ldapUser *LdapUser, verifyReverse bool) (bool, error) {
	// Check if the admin group is configured
	adminGroup := cfg.User.AdminGroup
	if adminGroup == "" {
		return false, nil
	}
	if ldapUser.passwordVerified && len(ldapUser.MemberOf) > 0 {
		if err := ls.refreshMemberOf(cfg, ldapUser); err != nil {
			return false, err
		}
	}

	if admin := ldapUser.directGroupMembership(adminGroup); admin != nil {
		return *admin, nil
	}

	admin, err := ls.searchGroupMember(cfg, ldapUser, adminGroup)
	if err == nil && !admin {
		// A new denial must invalidate even a grant that is still waiting to commit.
		ldapUser.adminDenialRecorded = false
	}
	if err != nil || !admin || !verifyReverse {
		return admin, err
	}
	// The group search only matches a DN. Check that it still names the same entry.
	entry, err := ls.readSameGroupEntry(cfg, ldapUser)
	if err != nil {
		return false, err
	}
	if directAdmin := entry.directGroupMembership(adminGroup); directAdmin != nil {
		ldapUser.MemberOf = entry.MemberOf
		ldapUser.memberOfRefreshed = true
		ldapUser.adminDenialRecorded = false
		return *directAdmin, nil
	}
	return true, nil
}

// isUserEnabled applies the configured directory status check, if any.
func (ls *LdapService) isUserEnabled(cfg *model.LdapSetting, ldapUser *LdapUser) bool {
	ldapUser.disabledConfirmed = false
	if cfg.User.UsesADAccountControl() {
		value, err := strconv.ParseUint(ldapUser.EnableAttrValue, 10, 32)
		const accountDisable = 0x2
		ldapUser.Enabled = err == nil && value&accountDisable == 0
		ldapUser.disabledConfirmed = err == nil && value&accountDisable != 0
	} else if cfg.User.EnableAttr == "" {
		ldapUser.Enabled = cfg.User.EnableAttrValue == ""
	} else {
		expected := cfg.User.EnableAttrValue
		ldapUser.Enabled = expected != "" && ldapUser.EnableAttrValue == expected
		ldapUser.disabledConfirmed = !ldapUser.Enabled && expected != "" && strings.TrimSpace(ldapUser.EnableAttrValue) != ""
	}
	return ldapUser.Enabled
}

func (ls *LdapService) refreshMemberOf(cfg *model.LdapSetting, ldapUser *LdapUser) error {
	entry, err := ls.readMemberOf(cfg, ldapUser.Dn)
	if err != nil {
		return err
	}
	if known, same := ldapUser.identity.compare(entry.identity); same {
		ldapUser.MemberOf = entry.MemberOf
		ldapUser.memberOfRefreshed = true
		ldapUser.adminDenialRecorded = false
		return nil
	} else if !known {
		previous, current := ldapUser.directGroupMembership(cfg.User.AdminGroup), entry.directGroupMembership(cfg.User.AdminGroup)
		// Direct non-administrators can still log in to directories that do not expose a stable ID.
		if previous != nil && !*previous && current != nil && !*current {
			return nil
		}
	}
	return ErrLdapMailNotMatch
}

func (ls *LdapService) readSameGroupEntry(cfg *model.LdapSetting, ldapUser *LdapUser) (*LdapUser, error) {
	entry, err := ls.readMemberOf(cfg, ldapUser.Dn)
	if err != nil {
		return nil, err
	}
	if _, same := ldapUser.identity.compare(entry.identity); !same {
		return nil, ErrLdapMailNotMatch
	}
	return entry, nil
}

func (ls *LdapService) readMemberOf(cfg *model.LdapSetting, dn string) (*LdapUser, error) {
	attr := ls.fieldMemberOf()
	searchRequest := ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		0,     // unlimited search results
		0,     // no server-side time limit
		false, // typesOnly
		"(objectClass=*)",
		[]string{attr, ldapEntryUUID, ldapObjectGUID},
		nil,
	)
	sr, err := ls.searchResult(cfg, searchRequest)
	if err != nil {
		return nil, err
	}
	if len(sr.Entries) != 1 {
		return nil, ErrLdapMailNotMatch
	}
	entry := sr.Entries[0]
	return &LdapUser{MemberOf: entry.GetEqualFoldAttributeValues(attr), identity: ldapIdentity(entry)}, nil
}
