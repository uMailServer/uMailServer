package auth

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
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
}

// LDAPClient handles LDAP authentication
type LDAPClient struct {
	config LDAPConfig
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
		slog.Warn("ldap: tls certificate verification is disabled (skip_verify=true). This is insecure and should only be used in development.")
	}

	client := &LDAPClient{
		config: config,
	}

	// Test connection
	if err := client.TestConnection(); err != nil {
		return nil, fmt.Errorf("ldap connection test failed: %w", err)
	}

	return client, nil
}

// TestConnection verifies LDAP connectivity
func (c *LDAPClient) TestConnection() error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	// If bind DN is configured, try to bind
	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
			return fmt.Errorf("ldap bind failed: %w", err)
		}
	}

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
	timeout := time.Duration(c.config.Timeout)

	if u.Scheme == "ldaps" {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: c.config.SkipVerify,
			ServerName:         u.Hostname(),
		}
		conn, err = ldap.DialTLS("tcp", host, tlsConfig)
	} else {
		conn, err = ldap.Dial("tcp", host)
		if err == nil && c.config.StartTLS {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: c.config.SkipVerify,
				ServerName:         u.Hostname(),
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

	// Connect
	conn, err := c.connect()
	if err != nil {
		slog.Error("ldap connection failed", "error", err)
		return nil, err
	}
	defer conn.Close()

	// Bind with service account if configured
	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
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
		slog.Error("ldap search failed", "error", err, "filter", filter)
		return nil, fmt.Errorf("user search failed")
	}

	if len(sr.Entries) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	if len(sr.Entries) > 1 {
		slog.Warn("ldap search returned multiple users", "username", username)
		return nil, fmt.Errorf("multiple users found")
	}

	userDN := sr.Entries[0].DN

	// Attempt user bind to verify password
	err = conn.Bind(userDN, password)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, fmt.Errorf("invalid credentials")
		}
		slog.Error("ldap user bind failed", "error", err)
		return nil, fmt.Errorf("authentication failed")
	}

	// Re-bind as service account for group lookup
	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
			slog.Error("ldap re-bind failed", "error", err)
			return nil, fmt.Errorf("group lookup failed")
		}
	}

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

	conn, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if c.config.BindDN != "" {
		err = conn.Bind(c.config.BindDN, c.config.BindPassword)
		if err != nil {
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
		return nil, err
	}

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
