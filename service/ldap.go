package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/lejianwen/rustdesk-api/v2/config"
	"github.com/lejianwen/rustdesk-api/v2/model"
	"gorm.io/gorm"
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

// LdapUser represents the user attributes retrieved from LDAP.
type LdapUser struct {
	Dn              string
	Email           string
	FirstName       string
	LastName        string
	MemberOf        []string
	EnableAttrValue string
	Enabled         bool
}

// Name returns the full name of an LDAP user.
func (lu *LdapUser) Name() string {
	return fmt.Sprintf("%s %s", lu.FirstName, lu.LastName)
}

// ToUser merges the LdapUser data into a provided *model.User.
// If 'u' is nil, it creates and returns a new *model.User.
func (lu *LdapUser) ToUser(u *model.User) *model.User {
	if u == nil {
		u = &model.User{}
	}
	u.Email = strings.ToLower(strings.TrimSpace(lu.Email))
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
func (ls *LdapService) connectAndBind(cfg *config.Ldap, username, password string) (*ldap.Conn, error) {
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
			caCert, err := os.ReadFile(cfg.TlsCaFile)
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
func (ls *LdapService) connectAndBindAdmin(cfg *config.Ldap) (*ldap.Conn, error) {
	return ls.connectAndBind(cfg, cfg.BindDn, cfg.BindPassword)
}

// verifyCredentials checks the provided username and password against LDAP.
func (ls *LdapService) verifyCredentials(cfg *config.Ldap, username, password string) error {
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

// Authenticate checks the provided email and password against LDAP.
// Returns the corresponding *model.User if successful, or an error if not.
func (ls *LdapService) Authenticate(email, password string) (*model.User, error) {
	ldapUser, err := ls.loginUser(email, password)
	if err != nil {
		return nil, err
	}
	user, err := ls.mapToLocalUser(&Config.Ldap, ldapUser)
	if err != nil {
		return nil, errors.Join(ErrLdapToLocalUserFailed, err)
	}
	return user, nil
}

func (ls *LdapService) loginUser(email, password string) (*LdapUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !ValidateEmailAddress(email) || password == "" {
		return nil, ErrLdapPasswordNotMatch
	}
	ldapUser, err := ls.GetUserInfoByEmailLdap(email)
	if err != nil {
		return nil, err
	}
	if !ldapUser.Enabled {
		return nil, ErrLdapUserDisabled
	}
	cfg := &Config.Ldap

	if cfg.User.AllowGroup != "" {
		isAdmin, _ := ls.isUserAdmin(cfg, ldapUser)
		if !isAdmin && !ls.isUserInGroup(cfg, ldapUser, cfg.User.AllowGroup) {
			return nil, errors.New("user not in allowed group")
		}
	}
	if err := ls.verifyCredentials(cfg, ldapUser.Dn, password); err != nil {
		return nil, err
	}
	return ldapUser, nil
}

// isUserInGroup checks if the user is a member of the specified group. by_sw
func (ls *LdapService) isUserInGroup(cfg *config.Ldap, ldapUser *LdapUser, groupDN string) bool {
	// Check "memberOf" directly
	if len(ldapUser.MemberOf) > 0 {
		for _, group := range ldapUser.MemberOf {
			if strings.EqualFold(group, groupDN) {
				return true
			}
		}
	}

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
		return false
	}

	// If any results are returned, the user is part of the group
	return len(groupResult.Entries) > 0
}

// mapToLocalUser checks whether the user exists locally; if not, creates one.
// If the user exists and Ldap.Sync is enabled, it updates local info.
func (ls *LdapService) mapToLocalUser(cfg *config.Ldap, lu *LdapUser) (*model.User, error) {
	email := strings.ToLower(strings.TrimSpace(lu.Email))
	if !ValidateEmailAddress(email) {
		return nil, errors.New("InvalidEmail")
	}
	localUser := &model.User{}
	err := DB.Where("email = ?", email).First(localUser).Error
	newUser := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !newUser {
		return nil, err
	}
	if !newUser && !cfg.User.Sync {
		if !localUser.LdapSynced {
			if err := DB.Model(localUser).Update("ldap_synced", true).Error; err != nil {
				return nil, err
			}
			localUser.LdapSynced = true
		}
		return localUser, nil
	}

	directoryUser, err := ls.localUserValues(cfg, lu)
	if err != nil {
		return nil, err
	}
	directoryUser.LdapSynced = true
	if newUser {
		directoryUser.GroupId = model.DefaultUserGroupID
		if err := DB.Create(directoryUser).Error; err != nil {
			return nil, errors.Join(ErrLdapCreateUserFailed, err)
		}
		return directoryUser, nil
	}

	if err := syncLdapUser(DB, localUser, directoryUser); err != nil {
		return nil, err
	}
	return localUser, nil
}

func syncLdapUser(db *gorm.DB, localUser, directoryUser *model.User) error {
	// Keep personal profile edits while applying directory permissions and revocation.
	if err := db.Model(localUser).Select("is_admin", "status", "ldap_synced").Updates(directoryUser).Error; err != nil {
		return err
	}
	return db.First(localUser, localUser.Id).Error
}

func (ls *LdapService) localUserValues(cfg *config.Ldap, lu *LdapUser) (*model.User, error) {
	isAdmin, err := ls.isUserAdmin(cfg, lu)
	if err != nil && cfg.User.Sync {
		return nil, err
	}
	user := lu.ToUser(nil)
	user.IsAdmin = &isAdmin
	return user, nil
}

// IsEmailExists checks if an email exists in LDAP (can be useful for local registration checks).
func (ls *LdapService) IsEmailExists(email string) (bool, error) {
	cfg := &Config.Ldap
	if !cfg.Enable {
		return false, nil
	}
	sr, err := ls.emailSearchResult(cfg, email)
	if err != nil {
		return false, err
	}
	return len(sr.Entries) > 0, nil
}

// GetUserInfoByEmailLdap returns the user info from LDAP for the given email.
func (ls *LdapService) GetUserInfoByEmailLdap(email string) (*LdapUser, error) {
	cfg := &Config.Ldap
	if !cfg.Enable {
		return nil, ErrLdapNotEnabled
	}
	sr, err := ls.emailSearchResult(cfg, email)
	if err != nil {
		return nil, errors.Join(ErrLdapSearchFailed, err)
	}
	if len(sr.Entries) != 1 {
		return nil, ErrLdapUserNotFound
	}
	return ls.userResultToLdapUser(cfg, sr.Entries[0]), nil
}

// GetUserInfoByEmailLocal returns the user info from LDAP for the given email. if the user exists, it will synchronize the user information to local database.
func (ls *LdapService) GetUserInfoByEmailLocal(email string) (*model.User, error) {
	ldapUser, err := ls.GetUserInfoByEmailLdap(email)
	if err != nil {
		return &model.User{}, err
	}
	return ls.mapToLocalUser(&Config.Ldap, ldapUser)
}

// emailSearchResult returns the search result for the given email.
func (ls *LdapService) emailSearchResult(cfg *config.Ldap, email string) (*ldap.SearchResult, error) {
	filter := ls.filterField(ls.fieldEmail(cfg), email)
	searchRequest := ls.buildUserSearchRequest(cfg, filter)
	return ls.searchResult(cfg, searchRequest)
}

func (ls *LdapService) searchResult(cfg *config.Ldap, searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
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
func (ls *LdapService) buildUserSearchRequest(cfg *config.Ldap, filter string) *ldap.SearchRequest {
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
func (ls *LdapService) buildUserAttributes(cfg *config.Ldap) []string {
	return []string{
		"dn",
		ls.fieldEmail(cfg),
		ls.fieldFirstName(cfg),
		ls.fieldLastName(cfg),
		ls.fieldMemberOf(),
		ls.fieldUserEnableAttr(cfg),
	}
}

// userResultToLdapUser maps an *ldap.Entry to our LdapUser struct.
func (ls *LdapService) userResultToLdapUser(cfg *config.Ldap, entry *ldap.Entry) *LdapUser {
	lu := &LdapUser{
		Dn:              entry.DN,
		Email:           entry.GetAttributeValue(ls.fieldEmail(cfg)),
		FirstName:       entry.GetAttributeValue(ls.fieldFirstName(cfg)),
		LastName:        entry.GetAttributeValue(ls.fieldLastName(cfg)),
		MemberOf:        entry.GetAttributeValues(ls.fieldMemberOf()),
		EnableAttrValue: entry.GetAttributeValue(ls.fieldUserEnableAttr(cfg)),
	}
	// Check if the user is enabled based on the LDAP configuration
	ls.isUserEnabled(cfg, lu)
	return lu
}

// filterField helps build simple attribute filters, e.g. (uid=username).
func (ls *LdapService) filterField(field, value string) string {
	return fmt.Sprintf("(%s=%s)", field, ldap.EscapeFilter(value))
}

// fieldEmail returns the configured email attribute or "mail" if not set.
func (ls *LdapService) fieldEmail(cfg *config.Ldap) string {
	if cfg.User.Email == "" {
		return "mail"
	}
	return cfg.User.Email
}

// fieldFirstName returns the configured first name attribute or "givenName" if not set.
func (ls *LdapService) fieldFirstName(cfg *config.Ldap) string {
	if cfg.User.FirstName == "" {
		return "givenName"
	}
	return cfg.User.FirstName
}

// fieldLastName returns the configured last name attribute or "sn" if not set.
func (ls *LdapService) fieldLastName(cfg *config.Ldap) string {
	if cfg.User.LastName == "" {
		return "sn"
	}
	return cfg.User.LastName
}

func (ls *LdapService) fieldMemberOf() string {
	return "memberOf"
}

func (ls *LdapService) fieldUserEnableAttr(cfg *config.Ldap) string {
	if cfg.User.EnableAttr == "" {
		return "userAccountControl"
	}
	return cfg.User.EnableAttr
}

// baseDnUser returns the user-specific base DN or the global base DN if none is set.
func (ls *LdapService) baseDnUser(cfg *config.Ldap) string {
	if cfg.User.BaseDn == "" {
		return cfg.BaseDn
	}
	return cfg.User.BaseDn
}

// isUserAdmin checks if the user is a member of the admin group.
func (ls *LdapService) isUserAdmin(cfg *config.Ldap, ldapUser *LdapUser) (bool, error) {
	// Check if the admin group is configured
	adminGroup := cfg.User.AdminGroup
	if adminGroup == "" {
		return false, nil
	}

	// Check "memberOf" directly
	if len(ldapUser.MemberOf) > 0 {
		for _, group := range ldapUser.MemberOf {
			if strings.EqualFold(group, adminGroup) {
				return true, nil
			}
		}
		return false, nil
	}

	// For "member" attribute, perform a reverse search on the group
	member := "member"
	userDN := ldap.EscapeFilter(ldapUser.Dn)
	groupFilter := fmt.Sprintf("(%s=%s)", member, userDN)

	// Create the LDAP search request
	groupSearchRequest := ldap.NewSearchRequest(
		adminGroup,
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

	// If any results are returned, the user is part of the admin group
	return len(groupResult.Entries) > 0, nil
}

// isUserEnabled checks if the user is enabled based on the LDAP configuration.
// If no enable attribute or value is set, all users are considered enabled by default.
func (ls *LdapService) isUserEnabled(cfg *config.Ldap, ldapUser *LdapUser) bool {
	// Retrieve the enable attribute and expected value from the configuration
	enableAttr := cfg.User.EnableAttr
	enableAttrValue := cfg.User.EnableAttrValue

	// If no enable attribute or value is configured, consider all users as enabled
	if enableAttr == "" || enableAttrValue == "" {
		ldapUser.Enabled = true
		return true
	}

	// Normalize the enable attribute for comparison
	enableAttr = strings.ToLower(enableAttr)

	// Handle Active Directory's userAccountControl attribute
	if enableAttr == "useraccountcontrol" {
		// Parse the userAccountControl value
		userAccountControl, err := strconv.Atoi(ldapUser.EnableAttrValue)
		if err != nil {
			fmt.Printf("[ERROR] Invalid userAccountControl value: %v\n", err)
			ldapUser.Enabled = false
			return false
		}

		// Account is disabled if the ACCOUNTDISABLE flag (0x2) is set
		const ACCOUNTDISABLE = 0x2
		ldapUser.Enabled = userAccountControl&ACCOUNTDISABLE == 0
		return ldapUser.Enabled
	}

	// For other attributes, perform a direct comparison with the expected value
	ldapUser.Enabled = ldapUser.EnableAttrValue == enableAttrValue
	return ldapUser.Enabled
}

// getAttrOfDn retrieves the value of an attribute for a given DN.
func (ls *LdapService) getAttrOfDn(cfg *config.Ldap, dn, attr string) string {
	searchRequest := ldap.NewSearchRequest(
		ldap.EscapeFilter(dn),
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		0,     // unlimited search results
		0,     // no server-side time limit
		false, // typesOnly
		"(objectClass=*)",
		[]string{attr},
		nil,
	)
	sr, err := ls.searchResult(cfg, searchRequest)
	if err != nil {
		return ""
	}
	if len(sr.Entries) == 0 {
		return ""
	}
	return sr.Entries[0].GetAttributeValue(attr)
}
