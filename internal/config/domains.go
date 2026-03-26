package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DomainManager handles per-domain configuration
type DomainManager struct {
	baseDir string
}

// DomainSettings holds per-domain configuration
type DomainSettings struct {
	Name        string            `yaml:"name"`
	MaxAccounts int               `yaml:"max_accounts"`
	MaxAliases  int               `yaml:"max_aliases"`
	MaxMailboxSize Size           `yaml:"max_mailbox_size"`
	IsActive    bool              `yaml:"is_active"`

	// DKIM configuration
	DKIM DomainDKIMSettings `yaml:"dkim"`

	// Security settings
	Security DomainSecuritySettings `yaml:"security"`

	// Spam settings
	Spam DomainSpamSettings `yaml:"spam"`

	// Custom DNS records
	DNSRecords []DNSRecord `yaml:"dns_records,omitempty"`
}

// DomainDKIMSettings holds DKIM configuration for a domain
type DomainDKIMSettings struct {
	Enabled    bool   `yaml:"enabled"`
	Selector   string `yaml:"selector"`
	KeyType    string `yaml:"key_type"` // rsa, ed25519
	KeySize    int    `yaml:"key_size"` // 1024, 2048
	PrivateKey string `yaml:"private_key,omitempty"` // Path to private key
	PublicKey  string `yaml:"public_key,omitempty"`  // Path to public key
}

// DomainSecuritySettings holds security settings per domain
type DomainSecuritySettings struct {
	RequireTLS         bool     `yaml:"require_tls"`
	MinTLSVersion      string   `yaml:"min_tls_version"`
	MTASTSEnabled      bool     `yaml:"mta_sts_enabled"`
	MTASTSPolicy       string   `yaml:"mta_sts_policy"` // none, testing, enforce
	MTASTSMaxAge       Duration `yaml:"mta_sts_max_age"`
}

// DomainSpamSettings holds spam settings per domain
type DomainSpamSettings struct {
	Enabled             bool    `yaml:"enabled"`
	RejectThreshold     float64 `yaml:"reject_threshold"`
	JunkThreshold       float64 `yaml:"junk_threshold"`
	QuarantineThreshold float64 `yaml:"quarantine_threshold"`
	BypassSPF           bool    `yaml:"bypass_spf"`    // For forwarding servers
	BypassDKIM          bool    `yaml:"bypass_dkim"`   // For mailing lists
}

// DNSRecord represents a custom DNS record
type DNSRecord struct {
	Type     string `yaml:"type"`
	Name     string `yaml:"name"`
	Value    string `yaml:"value"`
	TTL      int    `yaml:"ttl"`
	Priority int    `yaml:"priority,omitempty"` // For MX, SRV records
}

// NewDomainManager creates a new domain manager
func NewDomainManager(baseDir string) *DomainManager {
	return &DomainManager{
		baseDir: filepath.Join(baseDir, "domains"),
	}
}

// Init ensures the domains directory exists
func (dm *DomainManager) Init() error {
	return os.MkdirAll(dm.baseDir, 0755)
}

// GetDomainPath returns the path to a domain's config file
func (dm *DomainManager) GetDomainPath(domain string) string {
	// Sanitize domain name for filesystem
	safeDomain := sanitizeDomainName(domain)
	return filepath.Join(dm.baseDir, safeDomain+".yaml")
}

// DomainExists checks if a domain config exists
func (dm *DomainManager) DomainExists(domain string) bool {
	path := dm.GetDomainPath(domain)
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// LoadDomain loads a domain's configuration
func (dm *DomainManager) LoadDomain(domain string) (*DomainSettings, error) {
	path := dm.GetDomainPath(domain)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read domain config: %w", err)
	}

	settings := &DomainSettings{}
	if err := yaml.Unmarshal(data, settings); err != nil {
		return nil, fmt.Errorf("failed to parse domain config: %w", err)
	}

	return settings, nil
}

// SaveDomain saves a domain's configuration
func (dm *DomainManager) SaveDomain(settings *DomainSettings) error {
	path := dm.GetDomainPath(settings.Name)

	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal domain config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write domain config: %w", err)
	}

	return nil
}

// DeleteDomain removes a domain's configuration
func (dm *DomainManager) DeleteDomain(domain string) error {
	path := dm.GetDomainPath(domain)
	return os.Remove(path)
}

// ListDomains returns a list of all configured domains
func (dm *DomainManager) ListDomains() ([]string, error) {
	entries, err := os.ReadDir(dm.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var domains []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") {
			domain := strings.TrimSuffix(name, ".yaml")
			domains = append(domains, unsanitizeDomainName(domain))
		}
	}

	return domains, nil
}

// GetAllDomains loads all domain configurations
func (dm *DomainManager) GetAllDomains() ([]*DomainSettings, error) {
	domainNames, err := dm.ListDomains()
	if err != nil {
		return nil, err
	}

	var settings []*DomainSettings
	for _, name := range domainNames {
		s, err := dm.LoadDomain(name)
		if err != nil {
			continue // Skip invalid configs
		}
		settings = append(settings, s)
	}

	return settings, nil
}

// CreateDomain creates a new domain with default settings
func (dm *DomainManager) CreateDomain(domain string) (*DomainSettings, error) {
	if dm.DomainExists(domain) {
		return nil, fmt.Errorf("domain already exists: %s", domain)
	}

	settings := &DomainSettings{
		Name:        domain,
		MaxAccounts: 100,
		MaxAliases:  500,
		MaxMailboxSize: 1 * GB,
		IsActive:    true,
		DKIM: DomainDKIMSettings{
			Enabled:  true,
			Selector: "default",
			KeyType:  "rsa",
			KeySize:  2048,
		},
		Security: DomainSecuritySettings{
			RequireTLS:    true,
			MinTLSVersion: "1.2",
			MTASTSEnabled:  true,
			MTASTSPolicy:  "testing",
			MTASTSMaxAge:  Duration(86400 * time.Second), // 24 hours
		},
		Spam: DomainSpamSettings{
			Enabled:             true,
			RejectThreshold:     9.0,
			JunkThreshold:       3.0,
			QuarantineThreshold: 6.0,
			BypassSPF:           false,
			BypassDKIM:          false,
		},
	}

	if err := dm.SaveDomain(settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// GetDKIMKeyPath returns the path to a domain's DKIM key
func (dm *DomainManager) GetDKIMKeyPath(domain, selector string) string {
	safeDomain := sanitizeDomainName(domain)
	return filepath.Join(dm.baseDir, safeDomain+"."+selector+".key")
}

// GetDKIMPublicPath returns the path to a domain's DKIM public key
func (dm *DomainManager) GetDKIMPublicPath(domain, selector string) string {
	safeDomain := sanitizeDomainName(domain)
	return filepath.Join(dm.baseDir, safeDomain+"."+selector+".pub")
}

// sanitizeDomainName makes a domain name safe for filesystem
func sanitizeDomainName(domain string) string {
	// Replace characters that might be problematic on filesystems
	return strings.ReplaceAll(domain, "/", "_")
}

// unsanitizeDomainName restores the original domain name
func unsanitizeDomainName(name string) string {
	return strings.ReplaceAll(name, "_", "/")
}

// ImportFromMainConfig imports domains from the old main config format
func (dm *DomainManager) ImportFromMainConfig(cfg *Config) error {
	for _, domain := range cfg.Domains {
		settings := &DomainSettings{
			Name:        domain.Name,
			MaxAccounts: domain.MaxAccounts,
			MaxMailboxSize: domain.MaxMailboxSize,
			IsActive:    true,
			DKIM: DomainDKIMSettings{
				Enabled:  domain.DKIM.Selector != "",
				Selector: domain.DKIM.Selector,
				KeyType:  "rsa",
				KeySize:  2048,
			},
		}

		if err := dm.SaveDomain(settings); err != nil {
			return err
		}
	}

	return nil
}
