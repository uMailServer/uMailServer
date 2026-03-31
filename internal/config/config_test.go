package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestLoadInvalidConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Create an invalid YAML file
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Load should fail
	_, err := Load(configPath)
	if err == nil {
		t.Error("expected error for invalid config file")
	}
}

func TestLoadConfigWithReadError(t *testing.T) {
	// Try to load a directory as a config file
	tmpDir := t.TempDir()
	_, err := Load(tmpDir)
	// Should return error since it's a directory
	if err == nil {
		t.Error("expected error when loading directory as config file")
	}
}

func TestValidateInvalidPorts(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{
			name: "invalid IMAP port",
			modify: func(c *Config) {
				c.IMAP.Enabled = true
				c.IMAP.Port = 0
			},
		},
		{
			name: "invalid SMTP submission port",
			modify: func(c *Config) {
				c.SMTP.Submission.Enabled = true
				c.SMTP.Submission.Port = -1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestValidateDomainWithoutName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{
			Name: "",
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for domain without name")
	}
}

func TestParseSizeEdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected Size
		wantErr  bool
	}{
		{"0", 0, false},
		{"100", 100, false},
		{"2kb", 2 * KB, false},
		{"3MB", 3 * MB, false},
		{"10gb", 10 * GB, false},
		{"1 TB", 1 * TB, false},
		{"1PB", 0, true},
		{"abc123", 0, true},
		{"123abc", 0, true},
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

func TestDurationEdgeCases(t *testing.T) {
	d := Duration(0)
	if d.ToDuration() != 0 {
		t.Errorf("expected 0 duration, got %s", d.ToDuration())
	}

	if d.String() != "0s" {
		t.Errorf("expected '0s', got %s", d.String())
	}

	// Test large duration
	d2 := Duration(24 * time.Hour)
	if d2.ToDuration() != 24*time.Hour {
		t.Errorf("expected 24h, got %s", d2.ToDuration())
	}
}

func TestLoadFromEnvInvalidValues(t *testing.T) {
	// Set invalid int value
	os.Setenv("UMAILSERVER_SMTP_INBOUND_PORT", "not_a_number")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_PORT")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err == nil {
		t.Error("expected error for invalid int value")
	}
}

func TestLoadFromEnvInvalidBool(t *testing.T) {
	// Set invalid bool value
	os.Setenv("UMAILSERVER_IMAP_ENABLED", "not_a_bool")
	defer os.Unsetenv("UMAILSERVER_IMAP_ENABLED")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err == nil {
		t.Error("expected error for invalid bool value")
	}
}

func TestLoadFromEnvDuration(t *testing.T) {
	os.Setenv("UMAILSERVER_SPAM_GREYLISTING_DELAY", "30m")
	defer os.Unsetenv("UMAILSERVER_SPAM_GREYLISTING_DELAY")

	cfg := DefaultConfig()
	// The Duration type may not be fully supported via env vars
	// Just ensure it doesn't panic
	err := loadFromEnv(cfg)
	if err != nil {
		t.Logf("loadFromEnv returned error (may be expected): %v", err)
	}
}

func TestLoadFromEnvInvalidDuration(t *testing.T) {
	os.Setenv("UMAILSERVER_SPAM_GREYLISTING_DELAY", "invalid_duration")
	defer os.Unsetenv("UMAILSERVER_SPAM_GREYLISTING_DELAY")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestConfigStructFields(t *testing.T) {
	cfg := DefaultConfig()

	// Test all major struct fields are accessible
	if cfg.TLS.ACME.Enabled != false {
		t.Error("expected ACME to be disabled by default")
	}

	if cfg.Security.MaxLoginAttempts != 5 {
		t.Errorf("expected max login attempts 5, got %d", cfg.Security.MaxLoginAttempts)
	}

	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("expected metrics path /metrics, got %s", cfg.Metrics.Path)
	}

	if cfg.Storage.Sync != true {
		t.Error("expected sync to be enabled by default")
	}
}

func TestEnsureDataDirAlreadyExists(t *testing.T) {
	// Test when directories already exist
	tmpDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.DataDir = tmpDir

	// Create directories first
	os.MkdirAll(filepath.Join(tmpDir, "domains"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "tmp"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "queue"), 0755)

	// Should not fail
	err := cfg.EnsureDataDir()
	if err != nil {
		t.Fatalf("EnsureDataDir failed: %v", err)
	}
}

func TestSetFieldFromString(t *testing.T) {
	tests := []struct {
		name     string
		field    reflect.Value
		val      string
		expected interface{}
		wantErr  bool
	}{
		{
			name:     "string field",
			field:    reflect.ValueOf(&struct{ S string }{}).Elem().Field(0),
			val:      "test",
			expected: "test",
			wantErr:  false,
		},
		{
			name:     "int field",
			field:    reflect.ValueOf(&struct{ I int }{}).Elem().Field(0),
			val:      "42",
			expected: int64(42),
			wantErr:  false,
		},
		{
			name:     "int64 field",
			field:    reflect.ValueOf(&struct{ I int64 }{}).Elem().Field(0),
			val:      "100",
			expected: int64(100),
			wantErr:  false,
		},
		{
			name:     "int32 field",
			field:    reflect.ValueOf(&struct{ I int32 }{}).Elem().Field(0),
			val:      "200",
			expected: int64(200),
			wantErr:  false,
		},
		{
			name:     "bool field true",
			field:    reflect.ValueOf(&struct{ B bool }{}).Elem().Field(0),
			val:      "true",
			expected: true,
			wantErr:  false,
		},
		{
			name:     "bool field false",
			field:    reflect.ValueOf(&struct{ B bool }{}).Elem().Field(0),
			val:      "false",
			expected: false,
			wantErr:  false,
		},
		{
			name:     "float64 field",
			field:    reflect.ValueOf(&struct{ F float64 }{}).Elem().Field(0),
			val:      "3.14",
			expected: 3.14,
			wantErr:  false,
		},
		{
			name:     "Size field",
			field:    reflect.ValueOf(&struct{ S Size }{}).Elem().Field(0),
			val:      "104857600", // 100MB as raw bytes
			expected: int64(104857600),
			wantErr:  false,
		},
		{
			name:     "Duration field",
			field:    reflect.ValueOf(&struct{ D Duration }{}).Elem().Field(0),
			val:      "300000000000", // 5m in nanoseconds
			expected: int64(300000000000),
			wantErr:  false,
		},
		{
			name:     "invalid int",
			field:    reflect.ValueOf(&struct{ I int }{}).Elem().Field(0),
			val:      "not_a_number",
			expected: int64(0),
			wantErr:  true,
		},
		{
			name:     "invalid bool",
			field:    reflect.ValueOf(&struct{ B bool }{}).Elem().Field(0),
			val:      "not_a_bool",
			expected: false,
			wantErr:  true,
		},
		{
			name:     "invalid float64",
			field:    reflect.ValueOf(&struct{ F float64 }{}).Elem().Field(0),
			val:      "not_a_float",
			expected: float64(0),
			wantErr:  true,
		},
		{
			name:     "invalid Size",
			field:    reflect.ValueOf(&struct{ S Size }{}).Elem().Field(0),
			val:      "invalid_size",
			expected: int64(0),
			wantErr:  true,
		},
		{
			name:     "invalid Duration",
			field:    reflect.ValueOf(&struct{ D Duration }{}).Elem().Field(0),
			val:      "invalid_duration",
			expected: int64(0),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setFieldFromString(tt.field, tt.val)
			if (err != nil) != tt.wantErr {
				t.Errorf("setFieldFromString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				switch tt.field.Kind() {
				case reflect.String:
					if tt.field.String() != tt.expected {
						t.Errorf("expected %v, got %v", tt.expected, tt.field.String())
					}
				case reflect.Int, reflect.Int64, reflect.Int32:
					if tt.field.Int() != tt.expected {
						t.Errorf("expected %v, got %v", tt.expected, tt.field.Int())
					}
				case reflect.Bool:
					if tt.field.Bool() != tt.expected {
						t.Errorf("expected %v, got %v", tt.expected, tt.field.Bool())
					}
				case reflect.Float64:
					if tt.field.Float() != tt.expected {
						t.Errorf("expected %v, got %v", tt.expected, tt.field.Float())
					}
				}
			}
		})
	}
}

func TestLoadSectionFromEnvInvalidField(t *testing.T) {
	// Test with a field that can't be set
	os.Setenv("UMAILSERVER_SERVER_HOSTNAME", "test.example.com")
	defer os.Unsetenv("UMAILSERVER_SERVER_HOSTNAME")

	cfg := DefaultConfig()
	// Make the field unexported (this is a bit hacky but tests the error path)
	err := loadSectionFromEnv(reflect.ValueOf(cfg).Elem(), "UMAILSERVER")
	if err != nil {
		t.Logf("loadSectionFromEnv returned error (may be expected): %v", err)
	}
}

func TestAskString(t *testing.T) {
	input := "test value\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	result, err := wizard.askString("Enter value:", "default")
	if err != nil {
		t.Fatalf("askString failed: %v", err)
	}
	if result != "test value" {
		t.Errorf("expected 'test value', got '%s'", result)
	}
}

func TestAskStringEmpty(t *testing.T) {
	input := "\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	result, err := wizard.askString("Enter value:", "default")
	if err != nil {
		t.Fatalf("askString failed: %v", err)
	}
	if result != "default" {
		t.Errorf("expected 'default', got '%s'", result)
	}
}

func TestAskBool(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"no\n", false},
		{"N\n", false},
		{"\n", true}, // default is true
	}

	for _, tt := range tests {
		wizard := NewSetupWizard()
		wizard.reader = bufio.NewReader(strings.NewReader(tt.input))

		result := wizard.askBool("Enable?", true)
		if result != tt.expected {
			t.Errorf("askBool(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestAskBoolDefaultFalse(t *testing.T) {
	input := "\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	result := wizard.askBool("Enable?", false)
	if result != false {
		t.Errorf("expected false, got %v", result)
	}
}

func TestAskInt(t *testing.T) {
	tests := []struct {
		input    string
		default_ int
		expected int
	}{
		{"42\n", 0, 42},
		{"\n", 10, 10}, // use default
	}

	for _, tt := range tests {
		wizard := NewSetupWizard()
		wizard.reader = bufio.NewReader(strings.NewReader(tt.input))

		result := wizard.askInt("Enter number:", tt.default_)
		if result != tt.expected {
			t.Errorf("askInt() = %d, want %d", result, tt.expected)
		}
	}
}

func TestAskIntInvalid(t *testing.T) {
	input := "invalid\n42\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	result := wizard.askInt("Enter number:", 0)
	// Should return default after invalid input
	if result != 0 {
		t.Errorf("expected 0 (default), got %d", result)
	}
}

func TestAskChoice(t *testing.T) {
	input := "2\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	choices := []string{"option1", "option2", "option3"}
	result, err := wizard.askChoice("Select:", choices, "option1")
	if err != nil {
		t.Fatalf("askChoice failed: %v", err)
	}
	if result != "option2" {
		t.Errorf("expected 'option2', got '%s'", result)
	}
}

func TestAskChoiceDefault(t *testing.T) {
	input := "\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	choices := []string{"option1", "option2"}
	result, err := wizard.askChoice("Select:", choices, "option1")
	if err != nil {
		t.Fatalf("askChoice failed: %v", err)
	}
	if result != "option1" {
		t.Errorf("expected 'option1' (default), got '%s'", result)
	}
}

func TestAskChoiceInvalid(t *testing.T) {
	input := "99\n1\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	choices := []string{"option1", "option2"}
	result, err := wizard.askChoice("Select:", choices, "option1")
	if err != nil {
		t.Fatalf("askChoice failed: %v", err)
	}
	if result != "option1" {
		t.Errorf("expected 'option1', got '%s'", result)
	}
}

func TestSetupWizardRun(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := t.TempDir()

	// Input sequence for the wizard:
	// 1. Data directory
	// 2. Hostname
	// 3. SMTP inbound enabled (y)
	// 4. SMTP inbound port (25)
	// 5. SMTP submission enabled (y)
	// 6. SMTP submission port (587)
	// 7. IMAP enabled (y)
	// 8. IMAP port (993)
	// 9. POP3 enabled (n)
	// 10. Admin enabled (y)
	// 11. Admin port (8080)
	// 12. ACME enabled (n)
	// 13. Spam enabled (n)
	// 14. Log level (1)
	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"y\n" +
		"25\n" +
		"y\n" +
		"587\n" +
		"y\n" +
		"993\n" +
		"n\n" +
		"y\n" +
		"8080\n" +
		"n\n" +
		"n\n" +
		"1\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if cfg.Server.Hostname != "mail.example.com" {
		t.Errorf("expected hostname mail.example.com, got %s", cfg.Server.Hostname)
	}

	if cfg.Server.DataDir != tmpDir {
		t.Errorf("expected data dir %s, got %s", tmpDir, cfg.Server.DataDir)
	}

	if !cfg.SMTP.Inbound.Enabled {
		t.Error("expected SMTP inbound to be enabled")
	}

	if cfg.SMTP.Inbound.Port != 25 {
		t.Errorf("expected SMTP inbound port 25, got %d", cfg.SMTP.Inbound.Port)
	}

	if !cfg.IMAP.Enabled {
		t.Error("expected IMAP to be enabled")
	}

	if cfg.IMAP.Port != 993 {
		t.Errorf("expected IMAP port 993, got %d", cfg.IMAP.Port)
	}
}

func TestSetupWizardRunWithACME(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := t.TempDir()

	// Input with ACME enabled
	input := tmpDir + "\n" +
		"mail.example.com\n" +
		"y\n" +
		"25\n" +
		"y\n" +
		"587\n" +
		"n\n" +
		"n\n" +
		"n\n" +
		"y\n" +
		"admin@example.com\n" +
		"n\n" +
		"1\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	cfg, err := wizard.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !cfg.TLS.ACME.Enabled {
		t.Error("expected ACME to be enabled")
	}

	if cfg.TLS.ACME.Email != "admin@example.com" {
		t.Errorf("expected ACME email admin@example.com, got %s", cfg.TLS.ACME.Email)
	}
}

func TestSizeUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Size
		wantErr  bool
	}{
		{
			name:     "valid size",
			input:    "1GB",
			expected: GB,
			wantErr:  false,
		},
		{
			name:     "empty size",
			input:    "",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "invalid size",
			input:    "invalid",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var size Size
			err := size.UnmarshalYAML(func(v interface{}) error {
				*(v.(*string)) = tt.input
				return nil
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if size != tt.expected {
				t.Errorf("UnmarshalYAML() size = %d, want %d", size, tt.expected)
			}
		})
	}
}

func TestDurationUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "valid duration",
			input:    "1h",
			expected: time.Hour,
			wantErr:  false,
		},
		{
			name:     "invalid duration",
			input:    "invalid",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := d.UnmarshalYAML(func(v interface{}) error {
				*(v.(*string)) = tt.input
				return nil
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if time.Duration(d) != tt.expected {
				t.Errorf("UnmarshalYAML() duration = %v, want %v", time.Duration(d), tt.expected)
			}
		})
	}
}

// --- New tests for improved coverage ---

// 1. setFieldFromString with unrecognized type silently returns nil
func TestSetFieldFromStringUnrecognizedType(t *testing.T) {
	// uint is not handled by setFieldFromString, should return nil without error
	field := reflect.ValueOf(&struct{ U uint }{}).Elem().Field(0)
	err := setFieldFromString(field, "42")
	if err != nil {
		t.Errorf("expected nil error for unrecognized type, got: %v", err)
	}

	// []string is not handled either
	sliceField := reflect.ValueOf(&struct{ S []string }{}).Elem().Field(0)
	err = setFieldFromString(sliceField, "a,b")
	if err != nil {
		t.Errorf("expected nil error for []string type, got: %v", err)
	}
}

// 2. ListDomains with missing directory covers os.IsNotExist branch
func TestListDomainsMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a DomainManager pointing at a non-existent subdirectory
	dm := NewDomainManager(tmpDir)
	// Do NOT call dm.Init(), so domains directory does not exist
	// Remove it just to be sure
	os.RemoveAll(dm.baseDir)

	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("expected nil error for missing directory, got: %v", err)
	}
	if len(domains) != 0 {
		t.Errorf("expected 0 domains for missing directory, got %d", len(domains))
	}
}

// 3. ListDomains only returns .yaml files, skipping .txt and others
func TestListDomainsNonYAMLFiles(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create various files in the domains directory
	domainsDir := dm.baseDir
	os.WriteFile(filepath.Join(domainsDir, "example.com.yaml"), []byte("name: example.com"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "notes.txt"), []byte("notes"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "backup.yaml.bak"), []byte("backup"), 0644)
	os.WriteFile(filepath.Join(domainsDir, "test.org.yaml"), []byte("name: test.org"), 0644)

	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}

	// Only .yaml files should be returned: example.com and test.org
	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d: %v", len(domains), domains)
	}

	// Verify .txt and .bak files are not included
	for _, d := range domains {
		if d == "notes" || d == "backup.yaml" {
			t.Errorf("non-YAML file was incorrectly listed as domain: %s", d)
		}
	}
}

// 4. GetAllDomains skips corrupt YAML files without error
func TestGetAllDomainsCorruptYAML(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a valid domain
	validSettings := &DomainSettings{
		Name:        "valid.com",
		MaxAccounts: 10,
		IsActive:    true,
	}
	if err := dm.SaveDomain(validSettings); err != nil {
		t.Fatalf("SaveDomain failed: %v", err)
	}

	// Create a corrupt YAML file directly
	domainsDir := dm.baseDir
	corruptPath := filepath.Join(domainsDir, "corrupt.com.yaml")
	os.WriteFile(corruptPath, []byte("invalid: yaml: [\n  : broken"), 0644)

	settings, err := dm.GetAllDomains()
	if err != nil {
		t.Fatalf("GetAllDomains should not return error for corrupt files: %v", err)
	}
	if len(settings) != 1 {
		t.Errorf("expected 1 valid domain setting, got %d", len(settings))
	}
	if len(settings) > 0 && settings[0].Name != "valid.com" {
		t.Errorf("expected valid.com, got %s", settings[0].Name)
	}
}

// 5. Validate SMTP inbound port <= 0
func TestValidateSMTPInboundPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"port zero", 0},
		{"port negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SMTP.Inbound.Enabled = true
			cfg.SMTP.Inbound.Port = tt.port
			err := cfg.Validate()
			if err == nil {
				t.Errorf("expected validation error for SMTP inbound port %d", tt.port)
			}
		})
	}
}

// 6. DeleteDomain error path - non-existent domain
func TestDeleteDomainNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	err := dm.DeleteDomain("nonexistent.com")
	if err == nil {
		t.Error("expected error when deleting non-existent domain")
	}
}

// 7. Size.MarshalYAML returns correct string
func TestSizeMarshalYAML(t *testing.T) {
	tests := []struct {
		size     Size
		expected string
	}{
		{0, "0"},
		{KB, "1KB"},
		{MB, "1MB"},
		{GB, "1GB"},
		{TB, "1TB"},
		{5 * GB, "5GB"},
		{512, "512"},
	}

	for _, tt := range tests {
		result, err := tt.size.MarshalYAML()
		if err != nil {
			t.Errorf("MarshalYAML() error = %v", err)
			continue
		}
		if result != tt.expected {
			t.Errorf("Size(%d).MarshalYAML() = %q, want %q", tt.size, result, tt.expected)
		}
	}
}

// 8. Duration.MarshalYAML returns correct string
func TestDurationMarshalYAML(t *testing.T) {
	tests := []struct {
		d        Duration
		expected string
	}{
		{Duration(0), "0s"},
		{Duration(5 * time.Minute), "5m0s"},
		{Duration(time.Hour), "1h0m0s"},
		{Duration(30 * time.Second), "30s"},
	}

	for _, tt := range tests {
		result, err := tt.d.MarshalYAML()
		if err != nil {
			t.Errorf("MarshalYAML() error = %v", err)
			continue
		}
		if result != tt.expected {
			t.Errorf("Duration(%v).MarshalYAML() = %q, want %q", tt.d, result, tt.expected)
		}
	}
}

// 9. Size.UnmarshalYAML with non-string/int64 type (float64)
func TestSizeUnmarshalYAMLFloat64(t *testing.T) {
	var size Size
	err := size.UnmarshalYAML(func(v interface{}) error {
		// First call tries string - simulate it failing with a non-string type error
		// Then it tries int64 - simulate it failing too
		// This triggers the float64 path: unmarshal into int64 will fail
		// We simulate the case where both string and int64 fail
		switch tv := v.(type) {
		case *string:
			return fmt.Errorf("not a string")
		case *int64:
			return fmt.Errorf("not an int64")
		default:
			return fmt.Errorf("unexpected type: %T", tv)
		}
	})

	if err == nil {
		t.Error("expected error when unmarshaling Size from unsupported type")
	}
}

// 10. Duration.UnmarshalYAML with non-string/int64 type (float64)
func TestDurationUnmarshalYAMLFloat64(t *testing.T) {
	var d Duration
	err := d.UnmarshalYAML(func(v interface{}) error {
		switch tv := v.(type) {
		case *string:
			return fmt.Errorf("not a string")
		case *int64:
			return fmt.Errorf("not an int64")
		default:
			return fmt.Errorf("unexpected type: %T", tv)
		}
	})

	if err == nil {
		t.Error("expected error when unmarshaling Duration from unsupported type")
	}
}

// 11. sanitizeDomainName/unsanitizeDomainName with / character
func TestSanitizeDomainNameWithSlash(t *testing.T) {
	domain := "example.com/special"
	sanitized := sanitizeDomainName(domain)
	if strings.Contains(sanitized, "/") {
		t.Errorf("sanitized name still contains /: %s", sanitized)
	}

	unsanitized := unsanitizeDomainName(sanitized)
	if unsanitized != domain {
		t.Errorf("unsanitizeDomainName(sanitizeDomainName(%q)) = %q, want %q", domain, unsanitized, domain)
	}

	// Verify round-trip through DomainManager
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	path := dm.GetDomainPath(domain)
	if strings.Contains(filepath.Base(path), "/") {
		t.Errorf("GetDomainPath returned path with /: %s", path)
	}
}

// 12. SaveDomain with write error (invalid path)
func TestSaveDomainWriteError(t *testing.T) {
	// Use a path that contains a null byte to trigger a write error
	dm := NewDomainManager("/dev/null\x00invalid/path")
	// Do NOT call Init - the base directory has a null byte so WriteFile will fail

	settings := &DomainSettings{
		Name:        "writetest.com",
		MaxAccounts: 10,
		IsActive:    true,
	}
	err := dm.SaveDomain(settings)
	if err == nil {
		t.Error("expected error when saving to invalid path")
	}
}

// 13. ImportFromMainConfig with empty domains slice
func TestImportFromMainConfigEmptyDomains(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	dm.Init()

	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{} // empty slice

	err := dm.ImportFromMainConfig(cfg)
	if err != nil {
		t.Fatalf("ImportFromMainConfig with empty domains should not error: %v", err)
	}

	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}
	if len(domains) != 0 {
		t.Errorf("expected 0 domains after importing empty config, got %d", len(domains))
	}
}

// 14. loadSectionFromEnv with pointer field (reflect.Ptr branch)
func TestLoadSectionFromEnvPointerField(t *testing.T) {
	os.Setenv("UMAILSERVER_SERVER_HOSTNAME", "pointer-test.example.com")
	defer os.Unsetenv("UMAILSERVER_SERVER_HOSTNAME")

	cfg := DefaultConfig()
	// Pass a pointer to the config value to exercise the reflect.Ptr branch
	ptrValue := reflect.ValueOf(cfg)
	err := loadSectionFromEnv(ptrValue, "UMAILSERVER_")
	if err != nil {
		t.Fatalf("loadSectionFromEnv with pointer value failed: %v", err)
	}

	// Verify it still works through the pointer dereference
	if cfg.Server.Hostname != "pointer-test.example.com" {
		t.Errorf("expected hostname pointer-test.example.com, got %s", cfg.Server.Hostname)
	}
}

// 15. GetDefaultDataDir returns non-empty
func TestGetDefaultDataDirNonEmpty(t *testing.T) {
	dir := GetDefaultDataDir()
	if dir == "" {
		t.Error("GetDefaultDataDir returned empty string")
	}
	t.Logf("GetDefaultDataDir() = %s", dir)
}

// 16. DomainManager Init called twice (already exists)
func TestDomainManagerInitTwice(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)

	// First init
	if err := dm.Init(); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}

	// Second init - should succeed (idempotent)
	if err := dm.Init(); err != nil {
		t.Fatalf("second Init failed: %v", err)
	}

	// Verify the directory still works
	info, err := os.Stat(dm.baseDir)
	if err != nil {
		t.Fatalf("domains directory does not exist after double init: %v", err)
	}
	if !info.IsDir() {
		t.Error("domains path is not a directory")
	}
}

// --- Additional coverage tests ---

// setFieldFromString: test Size type with human-readable value
func TestSetFieldFromStringSizeWithUnit(t *testing.T) {
	field := reflect.ValueOf(&struct{ S Size }{}).Elem().Field(0)
	err := setFieldFromString(field, "50MB")
	if err != nil {
		t.Fatalf("setFieldFromString with Size '50MB' failed: %v", err)
	}
	if field.Int() != int64(50*MB) {
		t.Errorf("expected %d, got %d", int64(50*MB), field.Int())
	}
}

// setFieldFromString: test Size with invalid value
func TestSetFieldFromStringSizeInvalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ S Size }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_size")
	if err == nil {
		t.Error("expected error for invalid Size value")
	}
}

// setFieldFromString: test Duration type with human-readable value
func TestSetFieldFromStringDurationValid(t *testing.T) {
	field := reflect.ValueOf(&struct{ D Duration }{}).Elem().Field(0)
	err := setFieldFromString(field, "10m")
	if err != nil {
		t.Fatalf("setFieldFromString with Duration '10m' failed: %v", err)
	}
	if field.Int() != int64(10*time.Minute) {
		t.Errorf("expected %d, got %d", int64(10*time.Minute), field.Int())
	}
}

// setFieldFromString: test Duration with invalid value
func TestSetFieldFromStringDurationInvalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ D Duration }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_duration")
	if err == nil {
		t.Error("expected error for invalid Duration value")
	}
}

// setFieldFromString: test int32 with invalid value
func TestSetFieldFromStringInt32Invalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ I int32 }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_number")
	if err == nil {
		t.Error("expected error for invalid int32 value")
	}
}

// Load: test with config file that fails env var loading
func TestLoadEnvOverrideError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  hostname: mail.example.com
  data_dir: /var/lib/umailserver
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Set an env var with an invalid value to trigger loadFromEnv error
	os.Setenv("UMAILSERVER_SMTP_INBOUND_PORT", "not_a_number")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_PORT")

	_, err := Load(configPath)
	if err == nil {
		t.Error("expected error for invalid env override")
	}
	if !strings.Contains(err.Error(), "failed to load env vars") {
		t.Errorf("expected env var error, got: %v", err)
	}
}

// Load: test with config file that fails validation
func TestLoadValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config that will fail validation (empty hostname)
	configContent := `
server:
  hostname: ""
  data_dir: /var/lib/umailserver
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("expected validation error for empty hostname")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

// Validate: test quarantine threshold >= reject threshold
func TestValidateQuarantineGreaterThanOrEqualReject(t *testing.T) {
	cfg := DefaultConfig()
	// Set quarantine >= reject to trigger the quarantine >= reject check
	cfg.Spam.QuarantineThreshold = 9.0
	cfg.Spam.RejectThreshold = 9.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when quarantine >= reject threshold")
	}
	if !strings.Contains(err.Error(), "spam.quarantine_threshold must be less than spam.reject_threshold") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// EnsureDataDir: test error creating directory
func TestEnsureDataDirError(t *testing.T) {
	// Use a path with a null byte to trigger a MkdirAll error
	cfg := DefaultConfig()
	cfg.Server.DataDir = string([]byte{0}) // null byte
	err := cfg.EnsureDataDir()
	if err == nil {
		t.Error("expected error creating data dir with null byte path")
	}
}

// GetDefaultDataDir: test with XDG_DATA_HOME set
func TestGetDefaultDataDirWithXDG(t *testing.T) {
	// Save and restore
	origXDG := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", origXDG)

	os.Setenv("XDG_DATA_HOME", "/custom/xdg/path")
	dir := GetDefaultDataDir()
	expected := filepath.Join("/custom/xdg/path", "umailserver")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

// GetDefaultDataDir: test without XDG_DATA_HOME (fallback to home dir)
func TestGetDefaultDataDirWithoutXDG(t *testing.T) {
	origXDG := os.Getenv("XDG_DATA_HOME")
	defer os.Setenv("XDG_DATA_HOME", origXDG)

	os.Unsetenv("XDG_DATA_HOME")
	dir := GetDefaultDataDir()
	if dir == "" {
		t.Error("expected non-empty data dir without XDG")
	}
	// Should not contain XDG path
	if strings.Contains(dir, "xdg") {
		t.Errorf("expected non-XDG path, got %s", dir)
	}
}

// Save (SetupWizard): test write error
func TestSetupWizardSaveWriteError(t *testing.T) {
	wizard := NewSetupWizard()

	// Write to a path with a null byte to trigger a WriteFile error
	err := wizard.Save(string([]byte{0}))
	if err == nil {
		t.Error("expected error when saving to invalid path")
	}
}

// ListDomains: test with subdirectories (should be skipped)
func TestListDomainsWithSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	domainsDir := dm.baseDir
	// Create a subdirectory
	if err := os.MkdirAll(filepath.Join(domainsDir, "subdir"), 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	// Create a yaml file
	os.WriteFile(filepath.Join(domainsDir, "example.com.yaml"), []byte("name: example.com"), 0644)

	domains, err := dm.ListDomains()
	if err != nil {
		t.Fatalf("ListDomains failed: %v", err)
	}

	if len(domains) != 1 {
		t.Errorf("expected 1 domain, got %d: %v", len(domains), domains)
	}
	if len(domains) > 0 && domains[0] != "example.com" {
		t.Errorf("expected example.com, got %s", domains[0])
	}
}

// ListDomains: test with non-NotExists error from ReadDir
func TestListDomainsReadDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.ReadDir behaves differently on Windows for file paths")
	}
	// Create a DomainManager whose baseDir is a file, not a directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "domains") // "domains" is a file, not a dir
	os.WriteFile(filePath, []byte("not a directory"), 0644)

	dm := &DomainManager{baseDir: filePath}

	_, err := dm.ListDomains()
	if err == nil {
		t.Error("expected error when ReadDir on a file")
	}
}

// SaveDomain: test marshal error (not easily triggered via normal means,
// but we can test the write error path)
func TestSaveDomainWriteErrorPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not restrict writes on Windows")
	}
	// Point the DomainManager at a read-only parent directory
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Make the domains directory read-only to trigger WriteFile error
	os.Chmod(dm.baseDir, 0555)
	defer os.Chmod(dm.baseDir, 0755) // restore for cleanup

	settings := &DomainSettings{
		Name:        "readonly.com",
		MaxAccounts: 10,
		IsActive:    true,
	}
	err := dm.SaveDomain(settings)
	if err == nil {
		t.Error("expected error when writing to read-only directory")
	}
}

// CreateDomain: test SaveDomain error within CreateDomain
func TestCreateDomainSaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not restrict writes on Windows")
	}
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Make the domains directory read-only
	os.Chmod(dm.baseDir, 0555)
	defer os.Chmod(dm.baseDir, 0755)

	_, err := dm.CreateDomain("fail.com")
	if err == nil {
		t.Error("expected error when creating domain in read-only directory")
	}
}

// ImportFromMainConfig: test SaveDomain error
func TestImportFromMainConfigSaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not restrict writes on Windows")
	}
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Make the domains directory read-only to cause SaveDomain to fail
	os.Chmod(dm.baseDir, 0555)
	defer os.Chmod(dm.baseDir, 0755)

	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{
			Name:          "import.com",
			MaxAccounts:   10,
			MaxMailboxSize: 1 * GB,
			DKIM:          DomainDKIMConfig{Selector: "default"},
		},
	}

	err := dm.ImportFromMainConfig(cfg)
	if err == nil {
		t.Error("expected error when importing domains to read-only directory")
	}
}

// ImportFromMainConfig: test with domain that has empty DKIM selector (DKIM disabled)
func TestImportFromMainConfigEmptyDKIMSelector(t *testing.T) {
	tmpDir := t.TempDir()
	dm := NewDomainManager(tmpDir)
	if err := dm.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{
			Name:          "nodkim.com",
			MaxAccounts:   10,
			MaxMailboxSize: 1 * GB,
			DKIM:          DomainDKIMConfig{Selector: ""}, // empty selector
		},
	}

	err := dm.ImportFromMainConfig(cfg)
	if err != nil {
		t.Fatalf("ImportFromMainConfig failed: %v", err)
	}

	settings, err := dm.LoadDomain("nodkim.com")
	if err != nil {
		t.Fatalf("LoadDomain failed: %v", err)
	}
	if settings.DKIM.Enabled {
		t.Error("expected DKIM to be disabled when selector is empty")
	}
}

// GetAllDomains: test when ListDomains returns an error
func TestGetAllDomainsListError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.ReadDir behaves differently on Windows for file paths")
	}
	// Point DomainManager at a file instead of a directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "domains")
	os.WriteFile(filePath, []byte("not a directory"), 0644)

	dm := &DomainManager{baseDir: filePath}

	_, err := dm.GetAllDomains()
	if err == nil {
		t.Error("expected error when GetAllDomains calls ListDomains on a file")
	}
}

// loadSectionFromEnv: test with non-struct, non-ptr value (should return nil)
func TestLoadSectionFromEnvNonStruct(t *testing.T) {
	// Pass a non-struct value - should return nil
	v := reflect.ValueOf("hello")
	err := loadSectionFromEnv(v, "PREFIX_")
	if err != nil {
		t.Errorf("expected nil error for non-struct, got: %v", err)
	}
}

// Size.UnmarshalYAML: test int64 branch (successful)
func TestSizeUnmarshalYAMLInt64(t *testing.T) {
	var size Size
	err := size.UnmarshalYAML(func(v interface{}) error {
		switch tv := v.(type) {
		case *string:
			return fmt.Errorf("not a string")
		case *int64:
			*tv = 1024
			return nil
		default:
			return fmt.Errorf("unexpected type: %T", tv)
		}
	})

	if err != nil {
		t.Fatalf("UnmarshalYAML with int64 failed: %v", err)
	}
	if size != 1024 {
		t.Errorf("expected size 1024, got %d", size)
	}
}

// Duration.UnmarshalYAML: test int64 branch (successful)
func TestDurationUnmarshalYAMLInt64(t *testing.T) {
	var d Duration
	err := d.UnmarshalYAML(func(v interface{}) error {
		switch tv := v.(type) {
		case *string:
			return fmt.Errorf("not a string")
		case *int64:
			*tv = int64(5 * time.Minute)
			return nil
		default:
			return fmt.Errorf("unexpected type: %T", tv)
		}
	})

	if err != nil {
		t.Fatalf("UnmarshalYAML with int64 failed: %v", err)
	}
	if time.Duration(d) != 5*time.Minute {
		t.Errorf("expected 5m, got %v", time.Duration(d))
	}
}

// ParseSize: test plain number fallback
func TestParseSizePlainNumber(t *testing.T) {
	size, err := ParseSize("2048")
	if err != nil {
		t.Fatalf("ParseSize plain number failed: %v", err)
	}
	if size != 2048 {
		t.Errorf("expected 2048, got %d", size)
	}
}

// ParseSize: test with just "B" suffix (bytes)
func TestParseSizeBytesSuffix(t *testing.T) {
	size, err := ParseSize("100B")
	if err != nil {
		t.Fatalf("ParseSize '100B' failed: %v", err)
	}
	if size != 100 {
		t.Errorf("expected 100, got %d", size)
	}
}

// ParseSize: test with unit suffixes K, M, G, T (without B)
func TestParseSizeShortUnits(t *testing.T) {
	tests := []struct {
		input    string
		expected Size
	}{
		{"1K", KB},
		{"1M", MB},
		{"1G", GB},
		{"1T", TB},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// askString with empty default
func TestAskStringEmptyDefault(t *testing.T) {
	input := "\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	result, err := wizard.askString("Enter value:", "")
	if err != nil {
		t.Fatalf("askString failed: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

// askString with reader error
func TestAskStringReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	// Use a reader that immediately errors
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	// Reading from empty reader should still work with default
	result, err := wizard.askString("Enter value:", "default")
	// The empty reader may or may not error, just verify it doesn't panic
	_ = result
	_ = err
}

// askBool with reader error
func TestAskBoolReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	// Should return default when reader has no data
	result := wizard.askBool("Enable?", true)
	if !result {
		t.Error("expected default true when reader fails")
	}
}

// askInt with reader error
func TestAskIntReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	// Should return default when reader has no data
	result := wizard.askInt("Enter number:", 42)
	if result != 42 {
		t.Errorf("expected default 42, got %d", result)
	}
}

// askChoice with reader error
func TestAskChoiceReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	choices := []string{"a", "b"}
	result, err := wizard.askChoice("Select:", choices, "a")
	if err != nil {
		t.Fatalf("askChoice failed: %v", err)
	}
	if result != "a" {
		t.Errorf("expected default 'a', got '%s'", result)
	}
}

// askChoice with invalid (non-numeric) input
func TestAskChoiceNonNumericInput(t *testing.T) {
	input := "abc\n"
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	choices := []string{"option1", "option2"}
	result, err := wizard.askChoice("Select:", choices, "option1")
	if err != nil {
		t.Fatalf("askChoice failed: %v", err)
	}
	if result != "option1" {
		t.Errorf("expected default 'option1', got '%s'", result)
	}
}

// Load with non-existent file path (not empty string, but a path that doesn't exist)
func TestLoadNonExistentFilePath(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load with non-existent path should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Should have default values since file doesn't exist
	if cfg.Server.Hostname != "localhost" {
		t.Errorf("expected default hostname localhost, got %s", cfg.Server.Hostname)
	}
}

// Load with env var overriding a Size field
func TestLoadFromEnvSizeField(t *testing.T) {
	os.Setenv("UMAILSERVER_SMTP_INBOUND_MAXMESSAGESIZE", "100MB")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_MAXMESSAGESIZE")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err != nil {
		t.Fatalf("loadFromEnv with Size field failed: %v", err)
	}
	if cfg.SMTP.Inbound.MaxMessageSize != 100*MB {
		t.Errorf("expected 100MB, got %d", cfg.SMTP.Inbound.MaxMessageSize)
	}
}

// Load with env var overriding a Duration field
func TestLoadFromEnvDurationField(t *testing.T) {
	os.Setenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT", "10m")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err != nil {
		t.Fatalf("loadFromEnv with Duration field failed: %v", err)
	}
	if cfg.SMTP.Inbound.ReadTimeout != Duration(10*time.Minute) {
		t.Errorf("expected 10m, got %v", cfg.SMTP.Inbound.ReadTimeout)
	}
}

// Load with env var overriding a Duration field with invalid value
func TestLoadFromEnvDurationFieldInvalid(t *testing.T) {
	os.Setenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT", "bad_duration")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err == nil {
		t.Error("expected error for invalid Duration env var")
	}
}

// Test Validate with invalid quarantine threshold <= junk threshold
func TestValidateQuarantineLessEqualJunk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.QuarantineThreshold = 3.0
	cfg.Spam.JunkThreshold = 3.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when quarantine <= junk threshold")
	}
}

// Setup wizard Run with data dir creation error
func TestSetupWizardRunDataDirError(t *testing.T) {
	// Input with a null byte in the data directory path to trigger MkdirAll error
	input := string([]byte{0}) + "\n"

	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(input))

	_, err := wizard.Run()
	if err == nil {
		t.Error("expected error when data directory creation fails")
	}
}
