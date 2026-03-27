package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Hostname != "localhost" {
		t.Errorf("expected hostname localhost, got %s", cfg.Server.Hostname)
	}

	if cfg.SMTP.Inbound.Port != 25 {
		t.Errorf("expected SMTP port 25, got %d", cfg.SMTP.Inbound.Port)
	}

	if cfg.IMAP.Port != 993 {
		t.Errorf("expected IMAP port 993, got %d", cfg.IMAP.Port)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected Size
		wantErr  bool
	}{
		{"1KB", KB, false},
		{"1MB", MB, false},
		{"1GB", GB, false},
		{"1TB", TB, false},
		{"5GB", 5 * GB, false},
		{"50MB", 50 * MB, false},
		{"1024", 1024, false},
		{"1.5GB", Size(1.5 * float64(GB)), false},
		{"", 0, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestSizeString(t *testing.T) {
	tests := []struct {
		size     Size
		expected string
	}{
		{KB, "1KB"},
		{MB, "1MB"},
		{GB, "1GB"},
		{TB, "1TB"},
		{5 * GB, "5GB"},
		{50 * MB, "50MB"},
		{1024, "1KB"},  // 1024 bytes = 1KB exactly
		{1536, "1536"}, // 1.5KB not exact
	}

	for _, tt := range tests {
		got := tt.size.String()
		if got != tt.expected {
			t.Errorf("Size(%d).String() = %q, want %q", tt.size, got, tt.expected)
		}
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("UMAILSERVER_SERVER_HOSTNAME", "mail.example.com")
	os.Setenv("UMAILSERVER_SMTP_INBOUND_PORT", "2525")
	os.Setenv("UMAILSERVER_SPAM_REJECTTHRESHOLD", "8.0")
	defer func() {
		os.Unsetenv("UMAILSERVER_SERVER_HOSTNAME")
		os.Unsetenv("UMAILSERVER_SMTP_INBOUND_PORT")
		os.Unsetenv("UMAILSERVER_SPAM_REJECTTHRESHOLD")
	}()

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err != nil {
		t.Fatalf("loadFromEnv failed: %v", err)
	}

	if cfg.Server.Hostname != "mail.example.com" {
		t.Errorf("expected hostname mail.example.com, got %s", cfg.Server.Hostname)
	}

	if cfg.SMTP.Inbound.Port != 2525 {
		t.Errorf("expected SMTP port 2525, got %d", cfg.SMTP.Inbound.Port)
	}

	if cfg.Spam.RejectThreshold != 8.0 {
		t.Errorf("expected reject threshold 8.0, got %f", cfg.Spam.RejectThreshold)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "missing hostname",
			modify: func(c *Config) {
				c.Server.Hostname = ""
			},
			wantErr: true,
		},
		{
			name: "missing data_dir",
			modify: func(c *Config) {
				c.Server.DataDir = ""
			},
			wantErr: true,
		},
		{
			name: "invalid reject threshold",
			modify: func(c *Config) {
				c.Spam.RejectThreshold = 2.0
				c.Spam.JunkThreshold = 3.0
			},
			wantErr: true,
		},
		{
			name: "invalid quarantine threshold",
			modify: func(c *Config) {
				c.Spam.QuarantineThreshold = 10.0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.DataDir = tmpDir

	err := cfg.EnsureDataDir()
	if err != nil {
		t.Fatalf("EnsureDataDir failed: %v", err)
	}

	// Check directories were created
	dirs := []string{
		tmpDir,
		filepath.Join(tmpDir, "domains"),
		filepath.Join(tmpDir, "tmp"),
		filepath.Join(tmpDir, "queue"),
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}
}

func TestDuration(t *testing.T) {
	d := Duration(5 * time.Minute)
	if d.ToDuration() != 5*time.Minute {
		t.Errorf("expected 5m, got %s", d.ToDuration())
	}

	if d.String() != "5m0s" {
		t.Errorf("expected '5m0s', got %s", d.String())
	}
}

func TestNewSetupWizard(t *testing.T) {
	wizard := NewSetupWizard()
	if wizard == nil {
		t.Fatal("expected non-nil wizard")
	}
	if wizard.Config == nil {
		t.Error("expected Config to be initialized")
	}
	if wizard.reader == nil {
		t.Error("expected reader to be initialized")
	}
}

func TestCheckFirstRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Should return true when config doesn't exist
	if !CheckFirstRun(tmpDir) {
		t.Error("expected CheckFirstRun to return true when config doesn't exist")
	}

	// Create config file
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	// Should return false when config exists
	if CheckFirstRun(tmpDir) {
		t.Error("expected CheckFirstRun to return false when config exists")
	}
}

func TestGetDefaultDataDir(t *testing.T) {
	// Just ensure it doesn't panic and returns something
	dataDir := GetDefaultDataDir()
	if dataDir == "" {
		t.Error("expected non-empty data dir")
	}
}

func TestSetupWizardSave(t *testing.T) {
	tmpDir := t.TempDir()
	wizard := NewSetupWizard()
	wizard.Config.Server.DataDir = tmpDir
	wizard.Config.Server.Hostname = "test.example.com"

	configPath := filepath.Join(tmpDir, "test_config.yaml")
	err := wizard.Save(configPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}

func TestDomainManager(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)

	// Test Init
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test CreateDomain
	settings, err := dm.CreateDomain("example.com")
	if err != nil {
		t.Fatalf("CreateDomain failed: %v", err)
	}
	if settings.Name != "example.com" {
		t.Errorf("expected domain name example.com, got %s", settings.Name)
	}

	// Test DomainExists
	if !dm.DomainExists("example.com") {
		t.Error("expected DomainExists to return true")
	}

	// Test LoadDomain
	loaded, err := dm.LoadDomain("example.com")
	if err != nil {
		t.Fatalf("LoadDomain failed: %v", err)
	}
	if loaded.Name != "example.com" {
		t.Errorf("expected loaded domain name example.com, got %s", loaded.Name)
	}

	// Test ListDomains
	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}
	if len(domains) != 1 {
		t.Errorf("expected 1 domain, got %d", len(domains))
	}

	// Test GetAllDomains
	allDomains, err := dm.GetAllDomains()
	if err != nil {
		t.Fatalf("GetAllDomains failed: %v", err)
	}
	if len(allDomains) != 1 {
		t.Errorf("expected 1 domain in GetAllDomains, got %d", len(allDomains))
	}

	// Test DeleteDomain
	if err := dm.DeleteDomain("example.com"); err != nil {
		t.Fatalf("DeleteDomain failed: %v", err)
	}
	if dm.DomainExists("example.com") {
		t.Error("expected domain to be deleted")
	}
}

func TestDomainManagerGetDomainPath(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)

	path := dm.GetDomainPath("example.com")
	expectedSuffix := filepath.Join("domains", "example.com.yaml")
	if !strings.HasSuffix(path, expectedSuffix) {
		t.Errorf("expected path to end with %s, got %s", expectedSuffix, path)
	}
}

func TestDomainManagerGetDKIMPaths(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)

	keyPath := dm.GetDKIMKeyPath("example.com", "default")
	if keyPath == "" {
		t.Error("expected non-empty DKIM key path")
	}

	pubPath := dm.GetDKIMPublicPath("example.com", "default")
	if pubPath == "" {
		t.Error("expected non-empty DKIM public path")
	}
}

func TestLoadConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config file
	configContent := `
server:
  hostname: mail.example.com
  data_dir: /var/lib/umailserver
smtp:
  inbound:
    port: 2525
    enabled: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Load the config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Hostname != "mail.example.com" {
		t.Errorf("expected hostname mail.example.com, got %s", cfg.Server.Hostname)
	}

	if cfg.SMTP.Inbound.Port != 2525 {
		t.Errorf("expected SMTP port 2525, got %d", cfg.SMTP.Inbound.Port)
	}
}

func TestLoadNonExistentConfig(t *testing.T) {
	// Loading a non-existent config should use defaults
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with empty path failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Should have default values
	if cfg.Server.Hostname != "localhost" {
		t.Errorf("expected default hostname localhost, got %s", cfg.Server.Hostname)
	}
}

func TestDatabasePath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Path = "/var/lib/umailserver/db"

	path := cfg.DatabasePath()
	if !strings.HasSuffix(path, ".db") {
		t.Errorf("expected database path to end with .db, got %s", path)
	}
}

func TestDomainSettingsStruct(t *testing.T) {
	settings := &DomainSettings{
		Name:           "example.com",
		MaxAccounts:    100,
		MaxMailboxSize: 1 * GB,
		IsActive:       true,
		DKIM: DomainDKIMSettings{
			Enabled:  true,
			Selector: "default",
			KeyType:  "rsa",
			KeySize:  2048,
		},
		Security: DomainSecuritySettings{
			RequireTLS:    true,
			MinTLSVersion: "1.2",
		},
		Spam: DomainSpamSettings{
			Enabled:         true,
			RejectThreshold: 9.0,
		},
	}

	if settings.Name != "example.com" {
		t.Errorf("expected domain name example.com, got %s", settings.Name)
	}
	if settings.MaxAccounts != 100 {
		t.Errorf("expected max accounts 100, got %d", settings.MaxAccounts)
	}
	if !settings.DKIM.Enabled {
		t.Error("expected DKIM to be enabled")
	}
}

func TestDNSRecordStruct(t *testing.T) {
	record := DNSRecord{
		Type:     "A",
		Name:     "mail",
		Value:    "192.168.1.1",
		TTL:      3600,
		Priority: 10,
	}

	if record.Type != "A" {
		t.Errorf("expected type A, got %s", record.Type)
	}
	if record.TTL != 3600 {
		t.Errorf("expected TTL 3600, got %d", record.TTL)
	}
}

func TestDomainManagerCreateDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	// Create domain first time
	_, err := dm.CreateDomain("example.com")
	if err != nil {
		t.Fatalf("First CreateDomain failed: %v", err)
	}

	// Try to create again - should fail
	_, err = dm.CreateDomain("example.com")
	if err == nil {
		t.Error("expected error when creating duplicate domain")
	}
}

func TestDomainManagerLoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	// Try to load non-existent domain
	_, err := dm.LoadDomain("nonexistent.com")
	if err == nil {
		t.Error("expected error when loading non-existent domain")
	}
}

func TestDomainManagerListDomainsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	// List domains when none exist
	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}
	if len(domains) != 0 {
		t.Errorf("expected 0 domains, got %d", len(domains))
	}
}

func TestDomainManagerImportFromMainConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{
			Name:          "example.com",
			MaxAccounts:   50,
			MaxMailboxSize: 2 * GB,
			DKIM: DomainDKIMConfig{
				Selector: "default",
			},
		},
	}

	err := dm.ImportFromMainConfig(cfg)
	if err != nil {
		t.Fatalf("ImportFromMainConfig failed: %v", err)
	}

	// Verify domain was created
	if !dm.DomainExists("example.com") {
		t.Error("expected imported domain to exist")
	}
}
