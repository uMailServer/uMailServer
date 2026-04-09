package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the main configuration structure
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	TLS         TLSConfig         `yaml:"tls"`
	SMTP        SMTPConfig        `yaml:"smtp"`
	IMAP        IMAPConfig        `yaml:"imap"`
	POP3        POP3Config        `yaml:"pop3"`
	HTTP        HTTPConfig        `yaml:"http"`
	Admin       AdminConfig       `yaml:"admin"`
	Spam        SpamConfig        `yaml:"spam"`
	AV          AVConfig          `yaml:"av"`
	Security    SecurityConfig    `yaml:"security"`
	LDAP        LDAPConfig        `yaml:"ldap"`
	MCP         MCPConfig         `yaml:"mcp"`
	ManageSieve ManageSieveConfig `yaml:"managesieve"`
	Domains     []DomainConfig    `yaml:"domains"`
	Logging     LoggingConfig     `yaml:"logging"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Database    DatabaseConfig    `yaml:"database"`
	Storage     StorageConfig     `yaml:"storage"`
}

// ServerConfig holds general server settings
type ServerConfig struct {
	Hostname string `yaml:"hostname"` // FQDN: mail.example.com
	DataDir  string `yaml:"data_dir"` // /var/lib/umailserver
}

// TLSConfig holds TLS and certificate settings
type TLSConfig struct {
	ACME       ACMEConfig `yaml:"acme"`
	CertFile   string     `yaml:"cert_file"`   // Manual cert path
	KeyFile    string     `yaml:"key_file"`    // Manual key path
	MinVersion string     `yaml:"min_version"` // "1.2" or "1.3"
}

// ACMEConfig holds Let's Encrypt settings
type ACMEConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Email       string `yaml:"email"`
	Provider    string `yaml:"provider"`               // letsencrypt, letsencrypt-staging
	Challenge   string `yaml:"challenge"`              // http-01, dns-01
	DNSProvider string `yaml:"dns_provider,omitempty"` // cloudflare, route53, etc.
}

// SMTPConfig holds SMTP server settings
type SMTPConfig struct {
	Inbound       InboundSMTPConfig    `yaml:"inbound"`
	Submission    SubmissionSMTPConfig `yaml:"submission"`
	SubmissionTLS SubmissionTLSConfig  `yaml:"submission_tls"`
}

// InboundSMTPConfig holds MX/inbound SMTP settings
type InboundSMTPConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Port           int      `yaml:"port"`
	Bind           string   `yaml:"bind"`
	MaxMessageSize Size     `yaml:"max_message_size"`
	MaxRecipients  int      `yaml:"max_recipients"`
	MaxConnections int      `yaml:"max_connections"`
	ReadTimeout    Duration `yaml:"read_timeout"`
	WriteTimeout   Duration `yaml:"write_timeout"`
}

// SubmissionSMTPConfig holds authenticated submission settings (STARTTLS)
type SubmissionSMTPConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Port           int    `yaml:"port"`
	Bind           string `yaml:"bind"`
	RequireAuth    bool   `yaml:"require_auth"`
	RequireTLS     bool   `yaml:"require_tls"`
	MaxConnections int    `yaml:"max_connections"`
}

// SubmissionTLSConfig holds implicit TLS submission settings
type SubmissionTLSConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Port           int    `yaml:"port"`
	Bind           string `yaml:"bind"`
	RequireAuth    bool   `yaml:"require_auth"`
	MaxConnections int    `yaml:"max_connections"`
}

// IMAPConfig holds IMAP server settings
type IMAPConfig struct {
	Enabled        bool     `yaml:"enabled"`
	Port           int      `yaml:"port"`
	Bind           string   `yaml:"bind"`
	STARTTLSPort   int      `yaml:"starttls_port"`
	IdleTimeout    Duration `yaml:"idle_timeout"`
	MaxConnections int      `yaml:"max_connections"`
}

// POP3Config holds POP3 server settings
type POP3Config struct {
	Enabled        bool   `yaml:"enabled"`
	Port           int    `yaml:"port"`
	Bind           string `yaml:"bind"`
	MaxConnections int    `yaml:"max_connections"`
}

// HTTPConfig holds HTTP server settings
type HTTPConfig struct {
	Enabled     bool     `yaml:"enabled"`
	Port        int      `yaml:"port"`
	HTTPPort    int      `yaml:"http_port"`
	Bind        string   `yaml:"bind"`
	CorsOrigins []string `yaml:"cors_origins"`
}

// AdminConfig holds admin panel settings
type AdminConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Bind    string `yaml:"bind"`
}

// SpamConfig holds spam filtering settings
type SpamConfig struct {
	Enabled             bool              `yaml:"enabled"`
	RejectThreshold     float64           `yaml:"reject_threshold"`
	JunkThreshold       float64           `yaml:"junk_threshold"`
	QuarantineThreshold float64           `yaml:"quarantine_threshold"`
	Bayesian            BayesianConfig    `yaml:"bayesian"`
	Greylisting         GreylistingConfig `yaml:"greylisting"`
	RBLServers          []string          `yaml:"rbl_servers"`
}

// BayesianConfig holds Bayesian classifier settings
type BayesianConfig struct {
	Enabled   bool `yaml:"enabled"`
	AutoTrain bool `yaml:"auto_train"`
}

// GreylistingConfig holds greylisting settings
type GreylistingConfig struct {
	Enabled bool     `yaml:"enabled"`
	Delay   Duration `yaml:"delay"`
}

// AVConfig holds antivirus scanning settings
type AVConfig struct {
	Enabled bool     `yaml:"enabled"`
	Addr    string   `yaml:"addr"` // ClamAV address (e.g., "127.0.0.1:3310")
	Timeout Duration `yaml:"timeout"`
	Action  string   `yaml:"action"` // "reject", "quarantine", "tag"
}

// SecurityConfig holds security settings
type SecurityConfig struct {
	MaxLoginAttempts int             `yaml:"max_login_attempts"`
	LockoutDuration  Duration        `yaml:"lockout_duration"`
	RateLimit        RateLimitConfig `yaml:"rate_limit"`
	JWTSecret        string          `yaml:"jwt_secret"`
}

// RateLimitConfig holds rate limiting settings
type RateLimitConfig struct {
	// Per-IP limits (inbound connections/messages)
	IPPerMinute   int `yaml:"ip_per_minute"`  // messages per minute per IP (inbound)
	IPPerHour     int `yaml:"ip_per_hour"`    // messages per hour per IP
	IPPerDay      int `yaml:"ip_per_day"`     // messages per day per IP
	IPConnections int `yaml:"ip_connections"` // concurrent connections per IP

	// Per-user limits (authenticated outbound sending)
	UserPerMinute     int `yaml:"user_per_minute"`     // messages per minute per user
	UserPerHour       int `yaml:"user_per_hour"`       // messages per hour per user
	UserPerDay        int `yaml:"user_per_day"`        // messages per day per user (daily quota)
	UserMaxRecipients int `yaml:"user_max_recipients"` // max recipients per message

	// Global limits
	GlobalPerMinute int `yaml:"global_per_minute"` // global messages per minute
	GlobalPerHour   int `yaml:"global_per_hour"`   // global messages per hour

	// Legacy aliases (for backwards compatibility)
	SMTPPerMinute         int `yaml:"smtp_per_minute"`
	SMTPPerHour           int `yaml:"smtp_per_hour"`
	IMAPConnections       int `yaml:"imap_connections"`
	HTTPRequestsPerMinute int `yaml:"http_requests_per_minute"`
}

// LDAPConfig holds LDAP/AD authentication settings
type LDAPConfig struct {
	Enabled        bool          `yaml:"enabled"`
	URL            string        `yaml:"url"`
	BindDN         string        `yaml:"bind_dn"`
	BindPassword   string        `yaml:"bind_password"`
	BaseDN         string        `yaml:"base_dn"`
	UserFilter     string        `yaml:"user_filter"`
	EmailAttribute string        `yaml:"email_attribute"`
	NameAttribute  string        `yaml:"name_attribute"`
	GroupAttribute string        `yaml:"group_attribute"`
	AdminGroups    []string      `yaml:"admin_groups"`
	StartTLS       bool          `yaml:"start_tls"`
	SkipVerify     bool          `yaml:"skip_verify"`
	Timeout        time.Duration `yaml:"timeout"`
}

// MCPConfig holds MCP server settings
type MCPConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
	Bind      string `yaml:"bind"`
}

// ManageSieveConfig holds ManageSieve server settings
type ManageSieveConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Bind    string `yaml:"bind"`
}

// DomainConfig holds per-domain settings
type DomainConfig struct {
	Name           string           `yaml:"name"`
	MaxAccounts    int              `yaml:"max_accounts"`
	MaxMailboxSize Size             `yaml:"max_mailbox_size"`
	DKIM           DomainDKIMConfig `yaml:"dkim"`
}

// DomainDKIMConfig holds DKIM settings for a domain
type DomainDKIMConfig struct {
	Selector string `yaml:"selector"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
	Output string `yaml:"output"` // stdout, stderr, or file path

	// Log rotation settings (only used when Output is a file path)
	MaxSizeMB  int `yaml:"max_size_mb"`  // Maximum log file size in MB before rotation
	MaxBackups int `yaml:"max_backups"`  // Maximum number of old log files to keep
	MaxAgeDays int `yaml:"max_age_days"` // Maximum number of days to retain old log files
}

// MetricsConfig holds metrics settings
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Bind    string `yaml:"bind"`
	Path    string `yaml:"path"`
}

// DatabaseConfig holds database settings
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// StorageConfig holds storage settings
type StorageConfig struct {
	Sync          bool `yaml:"sync"`
	SharedFolders bool `yaml:"shared_folders"`
}

// Load loads configuration from file with defaults and env overrides
func Load(path string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Load from file if provided
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
			// Config file doesn't exist, use defaults
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config file: %w", err)
			}
		}
	}

	// Apply environment variable overrides
	if err := loadFromEnv(cfg); err != nil {
		return nil, fmt.Errorf("failed to load env vars: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// loadFromEnv loads configuration from environment variables
// Format: UMAILSERVER_<SECTION>_<KEY>
// Example: UMAILSERVER_SMTP_INBOUND_PORT=2525
func loadFromEnv(cfg *Config) error {
	prefix := "UMAILSERVER_"

	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)
		section := strings.ToUpper(fieldType.Name)

		if err := loadSectionFromEnv(field, prefix+section+"_"); err != nil {
			return err
		}
	}

	return nil
}

// loadSectionFromEnv recursively loads struct fields from environment variables
func loadSectionFromEnv(v reflect.Value, prefix string) error {
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		if !field.CanSet() {
			continue
		}

		envKey := prefix + strings.ToUpper(fieldType.Name)
		envVal := os.Getenv(envKey)

		if envVal != "" {
			if err := setFieldFromString(field, envVal); err != nil {
				return fmt.Errorf("failed to set %s: %w", envKey, err)
			}
		}

		// Recurse into nested structs
		if field.Kind() == reflect.Struct {
			if err := loadSectionFromEnv(field, envKey+"_"); err != nil {
				return err
			}
		}
	}

	return nil
}

// setFieldFromString sets a field value from a string
func setFieldFromString(field reflect.Value, val string) error {
	// Check custom types first, since Size and Duration have Kind()==Int64
	// and would otherwise be handled by the standard int parsing which
	// cannot parse human-readable values like "50MB" or "10m".
	if field.Type() == reflect.TypeOf(Size(0)) {
		size, err := ParseSize(val)
		if err != nil {
			return err
		}
		field.SetInt(int64(size))
		return nil
	}
	if field.Type() == reflect.TypeOf(Duration(0)) {
		dur, err := time.ParseDuration(val)
		if err != nil {
			// Fallback: try parsing as plain nanoseconds
			n, err2 := strconv.ParseInt(val, 10, 64)
			if err2 != nil {
				return err // return original ParseDuration error
			}
			field.SetInt(n)
			return nil
		}
		field.SetInt(int64(dur))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Int32:
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return err
		}
		field.SetInt(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	}
	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Required fields
	if c.Server.Hostname == "" {
		return fmt.Errorf("server.hostname is required")
	}
	if c.Server.DataDir == "" {
		return fmt.Errorf("server.data_dir is required")
	}

	// Validate data directory is writable
	if err := checkDirWritable(c.Server.DataDir); err != nil {
		return fmt.Errorf("server.data_dir is not writable: %w", err)
	}

	// Validate JWT secret length (empty is allowed - will be generated at runtime)
	if c.Security.JWTSecret != "" && len(c.Security.JWTSecret) < 32 {
		return fmt.Errorf("security.jwt_secret must be at least 32 characters")
	}

	// Validate TLS MinVersion
	if c.TLS.MinVersion != "" && c.TLS.MinVersion != "1.2" && c.TLS.MinVersion != "1.3" {
		return fmt.Errorf("tls.min_version must be '1.2' or '1.3'")
	}

	// Validate ports
	if c.SMTP.Inbound.Enabled && c.SMTP.Inbound.Port <= 0 {
		return fmt.Errorf("smtp.inbound.port must be positive")
	}
	if c.SMTP.Submission.Enabled && c.SMTP.Submission.Port <= 0 {
		return fmt.Errorf("smtp.submission.port must be positive")
	}
	if c.IMAP.Enabled && c.IMAP.Port <= 0 {
		return fmt.Errorf("imap.port must be positive")
	}
	if c.POP3.Enabled && c.POP3.Port <= 0 {
		return fmt.Errorf("pop3.port must be positive")
	}

	// Validate connection limits (if specified, must be non-negative)
	if c.IMAP.Enabled && c.IMAP.MaxConnections < 0 {
		return fmt.Errorf("imap.max_connections must be non-negative")
	}
	if c.SMTP.Inbound.Enabled && c.SMTP.Inbound.MaxConnections < 0 {
		return fmt.Errorf("smtp.inbound.max_connections must be non-negative")
	}
	if c.SMTP.Submission.Enabled && c.SMTP.Submission.MaxConnections < 0 {
		return fmt.Errorf("smtp.submission.max_connections must be non-negative")
	}
	if c.SMTP.SubmissionTLS.Enabled && c.SMTP.SubmissionTLS.MaxConnections < 0 {
		return fmt.Errorf("smtp.submission_tls.max_connections must be non-negative")
	}
	if c.POP3.Enabled && c.POP3.MaxConnections < 0 {
		return fmt.Errorf("pop3.max_connections must be non-negative")
	}

	// Check for port conflicts
	if err := c.checkPortConflicts(); err != nil {
		return err
	}

	// Validate thresholds
	if c.Spam.RejectThreshold <= c.Spam.JunkThreshold {
		return fmt.Errorf("spam.reject_threshold must be greater than spam.junk_threshold")
	}
	if c.Spam.QuarantineThreshold <= c.Spam.JunkThreshold {
		return fmt.Errorf("spam.quarantine_threshold must be greater than spam.junk_threshold")
	}
	if c.Spam.QuarantineThreshold >= c.Spam.RejectThreshold {
		return fmt.Errorf("spam.quarantine_threshold must be less than spam.reject_threshold")
	}

	// Validate spam thresholds are in reasonable range
	if c.Spam.JunkThreshold < 0 || c.Spam.JunkThreshold > 100 {
		return fmt.Errorf("spam.junk_threshold must be between 0 and 100")
	}
	if c.Spam.QuarantineThreshold < 0 || c.Spam.QuarantineThreshold > 100 {
		return fmt.Errorf("spam.quarantine_threshold must be between 0 and 100")
	}
	if c.Spam.RejectThreshold < 0 || c.Spam.RejectThreshold > 100 {
		return fmt.Errorf("spam.reject_threshold must be between 0 and 100")
	}

	// Validate rate limits
	if c.Security.RateLimit.SMTPPerMinute < 0 {
		return fmt.Errorf("security.rate_limit.smtp_per_minute must be non-negative")
	}
	if c.Security.RateLimit.SMTPPerHour < 0 {
		return fmt.Errorf("security.rate_limit.smtp_per_hour must be non-negative")
	}
	if c.Security.RateLimit.IMAPConnections < 0 {
		return fmt.Errorf("security.rate_limit.imap_connections must be non-negative")
	}
	if c.Security.RateLimit.HTTPRequestsPerMinute < 0 {
		return fmt.Errorf("security.rate_limit.http_requests_per_minute must be non-negative")
	}

	// Validate timeouts
	if c.SMTP.Inbound.ReadTimeout < 0 {
		return fmt.Errorf("smtp.inbound.read_timeout must be non-negative")
	}
	if c.SMTP.Inbound.WriteTimeout < 0 {
		return fmt.Errorf("smtp.inbound.write_timeout must be non-negative")
	}

	// Validate AV settings
	if c.AV.Enabled {
		if c.AV.Addr == "" {
			return fmt.Errorf("av.addr is required when av.enabled is true")
		}
		validActions := map[string]bool{"reject": true, "quarantine": true, "tag": true}
		if !validActions[c.AV.Action] {
			return fmt.Errorf("av.action must be 'reject', 'quarantine', or 'tag'")
		}
	}

	// Validate logging settings
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Logging.Level != "" && !validLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level must be 'debug', 'info', 'warn', or 'error'")
	}
	validFormats := map[string]bool{"json": true, "text": true}
	if c.Logging.Format != "" && !validFormats[c.Logging.Format] {
		return fmt.Errorf("logging.format must be 'json' or 'text'")
	}

	// Validate TLS certificate files if provided
	if c.TLS.CertFile != "" || c.TLS.KeyFile != "" {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file is required when tls.key_file is set")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file is required when tls.cert_file is set")
		}
		if err := checkFileReadable(c.TLS.CertFile); err != nil {
			return fmt.Errorf("tls.cert_file is not readable: %w", err)
		}
		if err := checkFileReadable(c.TLS.KeyFile); err != nil {
			return fmt.Errorf("tls.key_file is not readable: %w", err)
		}
	}

	// Validate ACME settings
	if c.TLS.ACME.Enabled {
		if c.TLS.ACME.Email == "" {
			return fmt.Errorf("tls.acme.email is required when ACME is enabled")
		}
		validProviders := map[string]bool{"letsencrypt": true, "letsencrypt-staging": true}
		if !validProviders[c.TLS.ACME.Provider] {
			return fmt.Errorf("tls.acme.provider must be 'letsencrypt' or 'letsencrypt-staging'")
		}
		validChallenges := map[string]bool{"http-01": true, "dns-01": true}
		if !validChallenges[c.TLS.ACME.Challenge] {
			return fmt.Errorf("tls.acme.challenge must be 'http-01' or 'dns-01'")
		}
	}

	// Validate domains
	domainNames := make(map[string]bool)
	for i, domain := range c.Domains {
		if domain.Name == "" {
			return fmt.Errorf("domains[%d].name is required", i)
		}
		if domainNames[domain.Name] {
			return fmt.Errorf("duplicate domain: %s", domain.Name)
		}
		domainNames[domain.Name] = true
		if domain.MaxAccounts < 0 {
			return fmt.Errorf("domains[%d].max_accounts must be non-negative", i)
		}
	}

	// Validate metrics path
	if c.Metrics.Enabled && c.Metrics.Path == "" {
		return fmt.Errorf("metrics.path is required when metrics is enabled")
	}

	return nil
}

// checkPortConflicts checks for conflicting port configurations
func (c *Config) checkPortConflicts() error {
	ports := make(map[int]string)

	checkPort := func(port int, name string) error {
		if port <= 0 {
			return nil // Disabled or invalid
		}
		if existing, ok := ports[port]; ok {
			return fmt.Errorf("port conflict: %s and %s both use port %d", existing, name, port)
		}
		ports[port] = name
		return nil
	}

	if err := checkPort(c.SMTP.Inbound.Port, "smtp.inbound"); err != nil {
		return err
	}
	if err := checkPort(c.SMTP.Submission.Port, "smtp.submission"); err != nil {
		return err
	}
	if err := checkPort(c.SMTP.SubmissionTLS.Port, "smtp.submission_tls"); err != nil {
		return err
	}
	if err := checkPort(c.IMAP.Port, "imap"); err != nil {
		return err
	}
	if err := checkPort(c.IMAP.STARTTLSPort, "imap.starttls"); err != nil {
		return err
	}
	if err := checkPort(c.POP3.Port, "pop3"); err != nil {
		return err
	}
	if err := checkPort(c.HTTP.Port, "http"); err != nil {
		return err
	}
	if err := checkPort(c.HTTP.HTTPPort, "http.plain"); err != nil {
		return err
	}
	if err := checkPort(c.Admin.Port, "admin"); err != nil {
		return err
	}
	if err := checkPort(c.Metrics.Port, "metrics"); err != nil {
		return err
	}
	if err := checkPort(c.MCP.Port, "mcp"); err != nil {
		return err
	}

	return nil
}

// checkFileReadable verifies that the given file exists and is readable
func checkFileReadable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist")
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}
	// Try to open for reading
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}
	f.Close()
	return nil
}

// checkDirWritable verifies that the given directory can be written to.
func checkDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".write_test_"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.WriteFile(tmp, []byte("test"), 0644); err != nil {
		return err
	}
	return os.Remove(tmp)
}

// EnsureDataDir ensures the data directory exists with proper structure
func (c *Config) EnsureDataDir() error {
	dirs := []string{
		c.Server.DataDir,
		filepath.Join(c.Server.DataDir, "domains"),
		filepath.Join(c.Server.DataDir, "tmp"),
		filepath.Join(c.Server.DataDir, "queue"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// DatabasePath returns the full path to the database file
func (c *Config) DatabasePath() string {
	return filepath.Join(c.Database.Path, "umailserver.db")
}
