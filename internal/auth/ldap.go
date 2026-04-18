package auth

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConfig holds LDAP/AD connection configuration
type LDAPConfig struct {
	Enabled        bool          `yaml:"enabled"`
	URL            string        `yaml:"url"`                    // ldap://localhost:389 or ldaps://localhost:636
	BindDN         string        `yaml:"bind_dn"`                // DN for initial bind (optional)
	BindPassword   string        `yaml:"bind_password" json:"-"` // Password for initial bind (optional)
	BaseDN         string        `yaml:"base_dn"`                // Base DN for user search
	UserFilter     string        `yaml:"user_filter"`            // Filter for user search (default: "(uid=%s)")
	EmailAttribute string        `yaml:"email_attribute"`        // Attribute for email (default: "mail")
	NameAttribute  string        `yaml:"name_attribute"`         // Attribute for display name (default: "cn")
	GroupAttribute string        `yaml:"group_attribute"`        // Attribute for group membership (default: "memberOf")
	AdminGroups    []string      `yaml:"admin_groups"`           // Groups that grant admin access
	StartTLS       bool          `yaml:"start_tls"`              // Use StartTLS on port 389
	SkipVerify     bool          `yaml:"skip_verify"`            // Skip TLS certificate verification (dev only)
	Timeout        time.Duration `yaml:"timeout"`                // Connection timeout
	MaxConnections int           `yaml:"max_connections"`        // Max pooled LDAP connections (default 10)
}

// LDAPClient handles LDAP authentication
type LDAPClient struct {
	config        LDAPConfig
	pool          *ldapPool
	loginMu       sync.Mutex
	loginAttempts map[string]*ldapLoginAttempt
}

// ldapLoginAttempt tracks failed LDAP auth attempts per username
type ldapLoginAttempt struct {
	count        int       // consecutive failures
	lastSeen     time.Time // last attempt timestamp
	lockoutUntil time.Time // lockout expiration (0 = not locked out)
}

// checkLoginRateLimit returns true if the username is allowed to attempt login.
// Uses 5 attempts per 15-minute window with exponential backoff (max 30 min).
func (c *LDAPClient) checkLoginRateLimit(username string) bool {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	now := time.Now()
	if c.loginAttempts == nil {
		c.loginAttempts = make(map[string]*ldapLoginAttempt)
	}

	attempt, exists := c.loginAttempts[username]
	if !exists || now.Sub(attempt.lastSeen) > 15*time.Minute {
		c.loginAttempts[username] = &ldapLoginAttempt{count: 1, lastSeen: now}
		return true
	}

	if attempt.lockoutUntil.After(now) {
		return false
	}

	// Limit to 5 failures per 15 minutes
	if attempt.count >= 5 {
		// Exponential backoff: lockout starts at 1 min, doubles each time
		backoff := time.Minute * time.Duration(1<<min(attempt.count-5, 4))
		if backoff > 30*time.Minute {
			backoff = 30 * time.Minute
		}
		attempt.lockoutUntil = now.Add(backoff)
		attempt.count++
		attempt.lastSeen = now
		return false
	}

	attempt.count++
	attempt.lastSeen = now
	return true
}

// recordLoginFailure records a failed login attempt for rate limiting
func (c *LDAPClient) recordLoginFailure(username string) {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	now := time.Now()
	if c.loginAttempts == nil {
		c.loginAttempts = make(map[string]*ldapLoginAttempt)
	}

	attempt, exists := c.loginAttempts[username]
	if !exists {
		c.loginAttempts[username] = &ldapLoginAttempt{count: 1, lastSeen: now}
		return
	}

	attempt.count++
	attempt.lastSeen = now
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LDAPUser represents an authenticated LDAP user
type LDAPUser struct {
	DN          string
	Username    string
	Email       string
	DisplayName string
	Groups      []string
	IsAdmin     bool
}

// NewLDAPClient creates a new LDAP client
func NewLDAPClient(config LDAPConfig) (*LDAPClient, error) {
	if config.UserFilter == "" {
		config.UserFilter = "(uid=%s)"
	}
	if config.EmailAttribute == "" {
		config.EmailAttribute = "mail"
	}
	if config.NameAttribute == "" {
		config.NameAttribute = "cn"
	}
	if config.GroupAttribute == "" {
		config.GroupAttribute = "memberOf"
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if config.Enabled && config.BindDN == "" {
		return nil, fmt.Errorf("ldap: bind_dn is required when ldap is enabled")
	}

	if err := validateUserFilter(config.UserFilter); err != nil {
		return nil, fmt.Errorf("ldap: invalid user_filter: %w", err)
	}

	if config.SkipVerify {
		slog.Warn("ldap: tls certificate verification is disabled (skip_verify=true). " +
			"This is INSECURE and should only be used in development or with self-signed certs. " +
			"For production, use proper TLS certificates.")
	}

	client := &LDAPClient{
		config: config,
	}
	client.pool = newLDAPPool(func() (pooledLDAPConn, error) {
		return client.connect()
	}, config.MaxConnections)

	// Test connection
	if err := client.TestConnection(); err != nil {
		return nil, fmt.Errorf("ldap connection test failed: %w", err)
	}

	return client, nil
}

// Close drains the connection pool. Safe to call multiple times.
func (c *LDAPClient) Close() {
	if c == nil || c.pool == nil {
		return
	}
	c.pool.close()
}

// TestConnection verifies LDAP connectivity. On success the conn is returned
// to the pool warm so the first real auth doesn't pay the dial cost.
func (c *LDAPClient) TestConnection() error {
	pc, err := c.pool.acquire()
	if err != nil {
		return err
	}
	conn, ok := pc.(*ldap.Conn)
	if !ok {
		c.pool.discard(pc)
		return fmt.Errorf("ldap pool returned unexpected connection type")
	}

	if c.config.BindDN != "" {
		if err := conn.Bind(c.config.BindDN, c.config.BindPassword); err != nil {
			c.pool.discard(conn)
			return fmt.Errorf("ldap bind failed: %w", err)
		}
	}

	c.pool.release(conn)
	return nil
}

// validateLDAPHost checks that the LDAP server hostname is not localhost
// or a private/internal IP address to prevent SSRF attacks
func validateLDAPHost(hostname string) error {
	// Block localhost variants
	lowerHost := strings.ToLower(hostname)
	if lowerHost == "localhost" || lowerHost == "localhost.localdomain" {
		return fmt.Errorf("ldap: localhost is not allowed for security reasons")
	}

	// Try to parse as IP address
	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("ldap: private/loopback IP addresses are not allowed for security reasons")
		}
		return nil
	}

	// Block numeric IPv6 addresses that resolve to private/localhost
	if strings.Contains(hostname, ":") {
		return fmt.Errorf("ldap: IPv6 addresses are not allowed for security reasons")
	}

	return nil
}

// connect establishes a connection to the LDAP server
func (c *LDAPClient) connect() (*ldap.Conn, error) {
	u, err := url.Parse(c.config.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid ldap url: %w", err)
	}

	hostname := u.Hostname()

	// SSRF protection: validate hostname is not localhost or private IP
	if err := validateLDAPHost(hostname); err != nil {
		return nil, err
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "ldaps" {
			host += ":636"
		} else {
			host += ":389"
		}
	}

	var conn *ldap.Conn
	timeout := c.config.Timeout

	if u.Scheme == "ldaps" {
		tlsConfig := &tls.Config{
			// #nosec G402 -- SkipVerify is intentionally user-configurable for self-signed/internal CA environments
			InsecureSkipVerify: c.config.SkipVerify,
			ServerName:         u.Hostname(),
			MinVersion:         tls.VersionTLS12,
		}
		conn, err = ldap.DialURL(fmt.Sprintf("ldaps://%s", host), ldap.DialWithTLSConfig(tlsConfig))
	} else {
		conn, err = ldap.DialURL(fmt.Sprintf("ldap://%s", host))
		if err == nil && c.config.StartTLS {
			tlsConfig := &tls.Config{
				// #nosec G402 -- SkipVerify is intentionally user-configurable for self-signed/internal CA environments
				InsecureSkipVerify: c.config.SkipVerify,
				ServerName:         u.Hostname(),
				MinVersion:         tls.VersionTLS12,
			}
			err = conn.StartTLS(tlsConfig)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("ldap connection failed: %w", err)
	}

	conn.SetTimeout(timeout)
	return conn, nil
}

// Authenticate validates user credentials against LDAP
func (c *LDAPClient) Authenticate(username, password string) (*LDAPUser, error) {
	if !c.config.Enabled {
		return nil, fmt.Errorf("ldap authentication is disabled")
	}

	// Check rate limit (5 failures per 15 minutes per username)
	if !c.checkLoginRateLimit(username) {
		return nil, fmt.Errorf("ldap rate limit exceeded, try again later")
	}

	pc, err := c.pool.acquire()
	if err != nil {
		slog.Error("ldap connection failed", "error", err)
		return nil, err
	}
	conn, ok := pc.(*ldap.Conn)
	if !ok {
		c.pool.discard(pc)
		return nil, fmt.Errorf("ldap pool returned unexpected connection type")
	}

	// Bind with service account if configured
	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
			c.pool.discard(conn)
			slog.Error("ldap service bind failed", "error", err)
			return nil, fmt.Errorf("ldap service bind failed")
		}
	}

	// Search for user
	filter := fmt.Sprintf(c.config.UserFilter, ldap.EscapeFilter(username))
	searchRequest := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		filter,
		[]string{
			"dn",
			c.config.EmailAttribute,
			c.config.NameAttribute,
			c.config.GroupAttribute,
		},
		nil,
	)

	sr, err := conn.Search(searchRequest)
	if err != nil {
		c.pool.discard(conn)
		slog.Error("ldap search failed", "error", err, "filter", filter)
		return nil, fmt.Errorf("user search failed")
	}

	if len(sr.Entries) == 0 {
		c.pool.release(conn)
		return nil, fmt.Errorf("user not found")
	}

	if len(sr.Entries) > 1 {
		c.pool.release(conn)
		slog.Warn("ldap search returned multiple users", "username", username)
		return nil, fmt.Errorf("multiple users found")
	}

	userDN := sr.Entries[0].DN

	// Attempt user bind to verify password
	err = conn.Bind(userDN, password)
	if err != nil {
		// Conn left in unknown bind state; discard rather than poison the pool.
		c.pool.discard(conn)
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			c.recordLoginFailure(username)
			return nil, fmt.Errorf("invalid credentials")
		}
		slog.Error("ldap user bind failed", "error", err)
		c.recordLoginFailure(username)
		return nil, fmt.Errorf("authentication failed")
	}

	// Re-bind as service account for group lookup and to leave conn reusable.
	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
			c.pool.discard(conn)
			slog.Error("ldap re-bind failed", "error", err)
			return nil, fmt.Errorf("group lookup failed")
		}
	}
	c.pool.release(conn)

	// Extract user info
	entry := sr.Entries[0]
	user := &LDAPUser{
		DN:          userDN,
		Username:    username,
		Email:       entry.GetAttributeValue(c.config.EmailAttribute),
		DisplayName: entry.GetAttributeValue(c.config.NameAttribute),
		Groups:      entry.GetAttributeValues(c.config.GroupAttribute),
	}

	// Check if user is in admin group
	for _, group := range user.Groups {
		for _, adminGroup := range c.config.AdminGroups {
			if strings.EqualFold(group, adminGroup) {
				user.IsAdmin = true
				break
			}
		}
		if user.IsAdmin {
			break
		}
	}

	slog.Info("ldap authentication successful",
		"username", username,
		"email", user.Email,
		"is_admin", user.IsAdmin,
	)

	return user, nil
}

// GetUser retrieves user details by username without authentication
func (c *LDAPClient) GetUser(username string) (*LDAPUser, error) {
	if !c.config.Enabled {
		return nil, fmt.Errorf("ldap authentication is disabled")
	}

	pc, err := c.pool.acquire()
	if err != nil {
		return nil, err
	}
	conn, ok := pc.(*ldap.Conn)
	if !ok {
		c.pool.discard(pc)
		return nil, fmt.Errorf("ldap pool returned unexpected connection type")
	}

	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
			c.pool.discard(conn)
			return nil, fmt.Errorf("ldap bind failed: %w", err)
		}
	}

	filter := fmt.Sprintf(c.config.UserFilter, ldap.EscapeFilter(username))
	searchRequest := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		filter,
		[]string{
			"dn",
			c.config.EmailAttribute,
			c.config.NameAttribute,
			c.config.GroupAttribute,
		},
		nil,
	)

	sr, err := conn.Search(searchRequest)
	if err != nil {
		c.pool.discard(conn)
		return nil, err
	}
	c.pool.release(conn)

	if len(sr.Entries) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	entry := sr.Entries[0]
	user := &LDAPUser{
		DN:          entry.DN,
		Username:    username,
		Email:       entry.GetAttributeValue(c.config.EmailAttribute),
		DisplayName: entry.GetAttributeValue(c.config.NameAttribute),
		Groups:      entry.GetAttributeValues(c.config.GroupAttribute),
	}

	for _, group := range user.Groups {
		for _, adminGroup := range c.config.AdminGroups {
			if strings.EqualFold(group, adminGroup) {
				user.IsAdmin = true
				break
			}
		}
		if user.IsAdmin {
			break
		}
	}

	return user, nil
}

// IsEnabled returns whether LDAP authentication is enabled
func (c *LDAPClient) IsEnabled() bool {
	return c != nil && c.config.Enabled
}

// validateUserFilter checks that the LDAP user filter is safe.
// It must contain exactly one %%s placeholder and have balanced parentheses.
func validateUserFilter(filter string) error {
	if filter == "" {
		return nil // Will use default (uid=%s)
	}
	if strings.Count(filter, "%s") != 1 {
		return fmt.Errorf("user_filter must contain exactly one %%s placeholder")
	}
	depth := 0
	for _, c := range filter {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("user_filter has unbalanced parentheses")
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("user_filter has unbalanced parentheses")
	}
	return nil
}
