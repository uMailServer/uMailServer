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
	Server   ServerConfig   `yaml:"server"`
	TLS      TLSConfig      `yaml:"tls"`
	SMTP     SMTPConfig     `yaml:"smtp"`
	IMAP     IMAPConfig     `yaml:"imap"`
	POP3     POP3Config     `yaml:"pop3"`
	HTTP     HTTPConfig     `yaml:"http"`
	Admin    AdminConfig    `yaml:"admin"`
	Spam     SpamConfig     `yaml:"spam"`
	AV       AVConfig       `yaml:"av"`
	Security SecurityConfig `yaml:"security"`
	MCP      MCPConfig      `yaml:"mcp"`
	Domains  []DomainConfig `yaml:"domains"`
	Logging  LoggingConfig  `yaml:"logging"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Database DatabaseConfig `yaml:"database"`
	Storage  StorageConfig  `yaml:"storage"`
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
	ReadTimeout    Duration `yaml:"read_timeout"`
	WriteTimeout   Duration `yaml:"write_timeout"`
}

// SubmissionSMTPConfig holds authenticated submission settings (STARTTLS)
type SubmissionSMTPConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Port        int    `yaml:"port"`
	Bind        string `yaml:"bind"`
	RequireAuth bool   `yaml:"require_auth"`
	RequireTLS  bool   `yaml:"require_tls"`
}

// SubmissionTLSConfig holds implicit TLS submission settings
type SubmissionTLSConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Port        int    `yaml:"port"`
	Bind        string `yaml:"bind"`
	RequireAuth bool   `yaml:"require_auth"`
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
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Bind    string `yaml:"bind"`
}

// HTTPConfig holds HTTP server settings
type HTTPConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Port     int    `yaml:"port"`
	HTTPPort int    `yaml:"http_port"`
	Bind     string `yaml:"bind"`
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
	SMTPPerMinute         int `yaml:"smtp_per_minute"`
	SMTPPerHour           int `yaml:"smtp_per_hour"`
	IMAPConnections       int `yaml:"imap_connections"`
	HTTPRequestsPerMinute int `yaml:"http_requests_per_minute"`
}

// MCPConfig holds MCP server settings
type MCPConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
	Bind      string `yaml:"bind"`
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

	// Validate domains
	for i, domain := range c.Domains {
		if domain.Name == "" {
			return fmt.Errorf("domains[%d].name is required", i)
		}
	}

	return nil
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
