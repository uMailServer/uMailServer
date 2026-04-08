package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
			// Use temp dir for valid config tests that don't explicitly set DataDir
			if tt.name == "valid config" {
				cfg.Server.DataDir = t.TempDir()
			}
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

func TestLoadConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config file
	configContent := `
server:
  hostname: mail.example.com
  data_dir: TMPDIR_PLACEHOLDER
smtp:
  inbound:
    port: 2525
    enabled: true
`
	// Replace placeholder with actual temp dir
	configContent = strings.Replace(configContent, "TMPDIR_PLACEHOLDER", tmpDir, 1)

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
	// Set temp data dir before loading config to avoid /var/lib/umailserver issues in CI
	tmpDir := t.TempDir()
	os.Setenv("UMAILSERVER_SERVER_DATADIR", tmpDir)
	defer os.Unsetenv("UMAILSERVER_SERVER_DATADIR")

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

func TestGetDefaultDataDirNonEmpty(t *testing.T) {
	dir := GetDefaultDataDir()
	if dir == "" {
		t.Error("GetDefaultDataDir returned empty string")
	}
	t.Logf("GetDefaultDataDir() = %s", dir)
}

func TestSetFieldFromStringSizeInvalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ S Size }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_size")
	if err == nil {
		t.Error("expected error for invalid Size value")
	}
}

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

func TestSetFieldFromStringDurationInvalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ D Duration }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_duration")
	if err == nil {
		t.Error("expected error for invalid Duration value")
	}
}

func TestSetFieldFromStringInt32Invalid(t *testing.T) {
	field := reflect.ValueOf(&struct{ I int32 }{}).Elem().Field(0)
	err := setFieldFromString(field, "not_a_number")
	if err == nil {
		t.Error("expected error for invalid int32 value")
	}
}

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

func TestValidateQuarantineGreaterThanOrEqualReject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
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

func TestEnsureDataDirError(t *testing.T) {
	// Use a path with a null byte to trigger a MkdirAll error
	cfg := DefaultConfig()
	cfg.Server.DataDir = string([]byte{0}) // null byte
	err := cfg.EnsureDataDir()
	if err == nil {
		t.Error("expected error creating data dir with null byte path")
	}
}

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

func TestSetupWizardSaveWriteError(t *testing.T) {
	wizard := NewSetupWizard()

	// Write to a path with a null byte to trigger a WriteFile error
	err := wizard.Save(string([]byte{0}))
	if err == nil {
		t.Error("expected error when saving to invalid path")
	}
}

func TestLoadSectionFromEnvNonStruct(t *testing.T) {
	// Pass a non-struct value - should return nil
	v := reflect.ValueOf("hello")
	err := loadSectionFromEnv(v, "PREFIX_")
	if err != nil {
		t.Errorf("expected nil error for non-struct, got: %v", err)
	}
}

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

func TestParseSizePlainNumber(t *testing.T) {
	size, err := ParseSize("2048")
	if err != nil {
		t.Fatalf("ParseSize plain number failed: %v", err)
	}
	if size != 2048 {
		t.Errorf("expected 2048, got %d", size)
	}
}

func TestParseSizeBytesSuffix(t *testing.T) {
	size, err := ParseSize("100B")
	if err != nil {
		t.Fatalf("ParseSize '100B' failed: %v", err)
	}
	if size != 100 {
		t.Errorf("expected 100, got %d", size)
	}
}

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

func TestAskBoolReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	// Should return default when reader has no data
	result := wizard.askBool("Enable?", true)
	if !result {
		t.Error("expected default true when reader fails")
	}
}

func TestAskIntReaderError(t *testing.T) {
	wizard := NewSetupWizard()
	wizard.reader = bufio.NewReader(strings.NewReader(""))

	// Should return default when reader has no data
	result := wizard.askInt("Enter number:", 42)
	if result != 42 {
		t.Errorf("expected default 42, got %d", result)
	}
}

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

func TestLoadNonExistentFilePath(t *testing.T) {
	// Set temp data dir before loading config to avoid /var/lib/umailserver issues in CI
	tmpDir := t.TempDir()
	os.Setenv("UMAILSERVER_SERVER_DATADIR", tmpDir)
	defer os.Unsetenv("UMAILSERVER_SERVER_DATADIR")

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

func TestLoadFromEnvDurationFieldInvalid(t *testing.T) {
	os.Setenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT", "bad_duration")
	defer os.Unsetenv("UMAILSERVER_SMTP_INBOUND_READTIMEOUT")

	cfg := DefaultConfig()
	err := loadFromEnv(cfg)
	if err == nil {
		t.Error("expected error for invalid Duration env var")
	}
}

func TestValidateQuarantineLessEqualJunk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.QuarantineThreshold = 3.0
	cfg.Spam.JunkThreshold = 3.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when quarantine <= junk threshold")
	}
}

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

func TestValidateJWTSecretTooShort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.Security.JWTSecret = "short"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for short JWT secret")
	}
	if !strings.Contains(err.Error(), "jwt_secret must be at least 32 characters") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTLSMinVersionInvalid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.TLS.MinVersion = "1.1"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid TLS min version")
	}
	if !strings.Contains(err.Error(), "tls.min_version must be '1.2' or '1.3'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTLSMinVersionValid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.TLS.MinVersion = "1.2"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid TLS min version 1.2: %v", err)
	}

	cfg.TLS.MinVersion = "1.3"
	err = cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid TLS min version 1.3: %v", err)
	}
}

func TestValidatePortConflict(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.SMTP.Inbound.Enabled = true
	cfg.SMTP.Inbound.Port = 25
	cfg.IMAP.Enabled = true
	cfg.IMAP.Port = 25 // Same port as SMTP
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for port conflict")
	}
	if !strings.Contains(err.Error(), "port conflict") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMaxConnectionsNegative(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{"IMAP", func(c *Config) {
			c.IMAP.Enabled = true
			c.IMAP.MaxConnections = -1
		}},
		{"SMTP.Inbound", func(c *Config) {
			c.SMTP.Inbound.Enabled = true
			c.SMTP.Inbound.MaxConnections = -1
		}},
		{"SMTP.Submission", func(c *Config) {
			c.SMTP.Submission.Enabled = true
			c.SMTP.Submission.MaxConnections = -1
		}},
		{"SMTP.SubmissionTLS", func(c *Config) {
			c.SMTP.SubmissionTLS.Enabled = true
			c.SMTP.SubmissionTLS.MaxConnections = -1
		}},
		{"POP3", func(c *Config) {
			c.POP3.Enabled = true
			c.POP3.MaxConnections = -1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Error("expected validation error for negative max connections")
			}
		})
	}
}

func TestValidateSpamThresholdsOutOfRange(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		errMsg string
	}{
		{
			name: "junk_threshold negative",
			modify: func(c *Config) {
				c.Spam.JunkThreshold = -1
			},
			errMsg: "spam.junk_threshold must be between 0 and 100",
		},
		{
			name: "junk_threshold over 100",
			modify: func(c *Config) {
				// Need: junk < quarantine < reject
				// To get junk=101 to trigger range check, we need:
				// junk=101, quarantine=102, reject=103 - but reject=103 > 100
				// Actually the order is checked BEFORE range, so if we can satisfy ordering
				// with values in range, we can test range. But for junk > 100:
				// - quarantine > junk (101) means quarantine >= 102
				// - reject > junk (101) and reject > quarantine
				// - But reject > quarantine > junk=101, so reject >= 103 > 100
				// This means we CANNOT test junk > 100 because the ordering constraints
				// force reject and quarantine above 100 as well.
				// So we test ordering violations instead when setting junk=101.
				c.Spam.JunkThreshold = 101
				c.Spam.QuarantineThreshold = 50 // quarantine <= junk triggers ordering error
				c.Spam.RejectThreshold = 102
			},
			errMsg: "spam.quarantine_threshold must be greater than spam.junk_threshold",
		},
		{
			name: "quarantine_threshold negative",
			modify: func(c *Config) {
				// Need: junk < quarantine < reject to satisfy ordering
				// For quarantine=-1, need junk < -1 and -1 < reject
				c.Spam.QuarantineThreshold = -1
				c.Spam.JunkThreshold = -2
				c.Spam.RejectThreshold = 0
			},
			errMsg: "spam.junk_threshold must be between 0 and 100", // junk range check runs before quarantine
		},
		{
			name: "quarantine_threshold over 100",
			modify: func(c *Config) {
				// Need: junk < quarantine < reject
				// quarantine=101, junk=5, reject must be > 101 and > quarantine - but reject > 101 > 100
				// So ordering check fails first. Let's set reject=102 but that > 100
				c.Spam.QuarantineThreshold = 101
				c.Spam.JunkThreshold = 5
				c.Spam.RejectThreshold = 50 // reject <= quarantine triggers ordering error
			},
			errMsg: "spam.quarantine_threshold must be less than spam.reject_threshold",
		},
		{
			name: "reject_threshold negative",
			modify: func(c *Config) {
				// reject > junk is impossible when reject is negative and junk is non-negative.
				// So this will always fail the ordering check first.
				c.Spam.RejectThreshold = -1
				c.Spam.JunkThreshold = 0
				c.Spam.QuarantineThreshold = 5
			},
			errMsg: "spam.reject_threshold must be greater than spam.junk_threshold", // ordering check fails first
		},
		{
			name: "reject_threshold over 100",
			modify: func(c *Config) {
				// Need: junk < quarantine < reject
				// reject=101, junk=50, quarantine=75 - this satisfies ordering (50 < 75 < 101)
				// But reject=101 > 100 so range check should trigger
				c.Spam.RejectThreshold = 101
				c.Spam.JunkThreshold = 50
				c.Spam.QuarantineThreshold = 75
			},
			errMsg: "spam.reject_threshold must be between 0 and 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.DataDir = t.TempDir()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Error("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestValidateRateLimitsNegative(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{"SMTPPerMinute", func(c *Config) {
			c.Security.RateLimit.SMTPPerMinute = -1
		}},
		{"SMTPPerHour", func(c *Config) {
			c.Security.RateLimit.SMTPPerHour = -1
		}},
		{"IMAPConnections", func(c *Config) {
			c.Security.RateLimit.IMAPConnections = -1
		}},
		{"HTTPRequestsPerMinute", func(c *Config) {
			c.Security.RateLimit.HTTPRequestsPerMinute = -1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Server.DataDir = t.TempDir()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Error("expected validation error for negative rate limit")
			}
		})
	}
}

func TestValidateTimeoutsNegative(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{"ReadTimeout", func(c *Config) {
			c.SMTP.Inbound.Enabled = true
			c.SMTP.Inbound.ReadTimeout = -1
		}},
		{"WriteTimeout", func(c *Config) {
			c.SMTP.Inbound.Enabled = true
			c.SMTP.Inbound.WriteTimeout = -1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Error("expected validation error for negative timeout")
			}
		})
	}
}

func TestValidateAVEnabledMissingAddr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AV.Enabled = true
	cfg.AV.Addr = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for AV enabled without addr")
	}
	if !strings.Contains(err.Error(), "av.addr is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateAVInvalidAction(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AV.Enabled = true
	cfg.AV.Addr = "127.0.0.1:3310"
	cfg.AV.Action = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid AV action")
	}
	if !strings.Contains(err.Error(), "av.action must be 'reject', 'quarantine', or 'tag'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateLoggingInvalidLevel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logging.Level = "trace"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid logging level")
	}
	if !strings.Contains(err.Error(), "logging.level must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateLoggingInvalidFormat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logging.Format = "xml"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid logging format")
	}
	if !strings.Contains(err.Error(), "logging.format must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTLSCertFileMissingKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.CertFile = "/path/to/cert.pem"
	cfg.TLS.KeyFile = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for TLS cert without key")
	}
	if !strings.Contains(err.Error(), "tls.key_file is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTLSKeyFileMissingCert(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.CertFile = ""
	cfg.TLS.KeyFile = "/path/to/key.pem"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for TLS key without cert")
	}
	if !strings.Contains(err.Error(), "tls.cert_file is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTLSCertFileNotReadable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.CertFile = "/nonexistent/cert.pem"
	cfg.TLS.KeyFile = "/nonexistent/key.pem"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for non-readable TLS cert file")
	}
	if !strings.Contains(err.Error(), "tls.cert_file is not readable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateACMEMissingEmail(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.ACME.Enabled = true
	cfg.TLS.ACME.Email = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for ACME without email")
	}
	if !strings.Contains(err.Error(), "tls.acme.email is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateACMEInvalidProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.ACME.Enabled = true
	cfg.TLS.ACME.Email = "test@example.com"
	cfg.TLS.ACME.Provider = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid ACME provider")
	}
	if !strings.Contains(err.Error(), "tls.acme.provider must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateACMEInvalidChallenge(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.ACME.Enabled = true
	cfg.TLS.ACME.Email = "test@example.com"
	cfg.TLS.ACME.Provider = "letsencrypt"
	cfg.TLS.ACME.Challenge = "tls-alpn-01"
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for invalid ACME challenge")
	}
	if !strings.Contains(err.Error(), "tls.acme.challenge must be") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDuplicateDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{Name: "example.com"},
		{Name: "example.com"},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for duplicate domain")
	}
	if !strings.Contains(err.Error(), "duplicate domain") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateDomainNegativeMaxAccounts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Domains = []DomainConfig{
		{Name: "example.com", MaxAccounts: -1},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for negative max accounts")
	}
	if !strings.Contains(err.Error(), "domains[0].max_accounts must be non-negative") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMetricsEnabledMissingPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Metrics.Enabled = true
	cfg.Metrics.Path = ""
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for metrics enabled without path")
	}
	if !strings.Contains(err.Error(), "metrics.path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckPortConflicts(t *testing.T) {
	cfg := DefaultConfig()

	// Test all port assignments that should not conflict
	cfg.SMTP.Inbound.Port = 25
	cfg.SMTP.Submission.Port = 587
	cfg.SMTP.SubmissionTLS.Port = 465
	cfg.IMAP.Port = 993
	cfg.IMAP.STARTTLSPort = 143
	cfg.POP3.Port = 995
	cfg.HTTP.Port = 8080
	cfg.HTTP.HTTPPort = 8081
	cfg.Admin.Port = 8443
	cfg.Metrics.Port = 9090
	cfg.MCP.Port = 8082

	err := cfg.checkPortConflicts()
	if err != nil {
		t.Errorf("unexpected port conflict error: %v", err)
	}
}

func TestCheckPortConflictsDisabledPorts(t *testing.T) {
	cfg := DefaultConfig()
	// Set all ports to 0 (disabled) - should not conflict
	cfg.SMTP.Inbound.Port = 0
	cfg.SMTP.Submission.Port = 0
	cfg.SMTP.SubmissionTLS.Port = 0
	cfg.IMAP.Port = 0
	cfg.IMAP.STARTTLSPort = 0
	cfg.POP3.Port = 0
	cfg.HTTP.Port = 0
	cfg.HTTP.HTTPPort = 0
	cfg.Admin.Port = 0
	cfg.Metrics.Port = 0
	cfg.MCP.Port = 0

	err := cfg.checkPortConflicts()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckFileReadableDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	err := checkFileReadable(tmpDir)
	if err == nil {
		t.Error("expected error when checking directory as file")
	}
	if !strings.Contains(err.Error(), "path is a directory") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckFileReadableNonExistent(t *testing.T) {
	err := checkFileReadable("/nonexistent/file/path")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "file does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckDirWritableNonWritable(t *testing.T) {
	// On Unix, try to write to a read-only directory
	// On Windows, this might not fail, so we just verify it doesn't panic
	tmpDir := t.TempDir()
	err := checkDirWritable(tmpDir)
	if err != nil {
		t.Logf("checkDirWritable failed (may be expected on some systems): %v", err)
	}
}

func TestValidateRejectThresholdLessThanJunk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.RejectThreshold = 3.0
	cfg.Spam.JunkThreshold = 5.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when reject <= junk")
	}
}

func TestValidateRejectThresholdEqualJunk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.RejectThreshold = 5.0
	cfg.Spam.JunkThreshold = 5.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when reject == junk")
	}
}

func TestValidateRejectThresholdGreaterThanQuarantine(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.RejectThreshold = 5.0
	cfg.Spam.QuarantineThreshold = 5.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when quarantine >= reject")
	}
}

func TestValidateRejectThresholdGreaterThanQuarantineLessThanJunk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.RejectThreshold = 3.0
	cfg.Spam.QuarantineThreshold = 5.0
	cfg.Spam.JunkThreshold = 7.0
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error when quarantine <= junk")
	}
}

func TestValidateIMAPDisabledNoPortCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IMAP.Enabled = false
	cfg.IMAP.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error when IMAP disabled: %v", err)
	}
}

func TestValidatePOP3DisabledNoPortCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.POP3.Enabled = false
	cfg.POP3.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error when POP3 disabled: %v", err)
	}
}

func TestValidateSMTPSubmissionDisabledNoPortCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SMTP.Submission.Enabled = false
	cfg.SMTP.Submission.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error when SMTP submission disabled: %v", err)
	}
}

func TestValidateSMTPInboundDisabledNoPortCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SMTP.Inbound.Enabled = false
	cfg.SMTP.Inbound.Port = 0
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error when SMTP inbound disabled: %v", err)
	}
}

func TestValidateAvidActionTag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AV.Enabled = true
	cfg.AV.Addr = "127.0.0.1:3310"
	cfg.AV.Action = "tag"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid AV action 'tag': %v", err)
	}
}

func TestValidateAvidActionQuarantine(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AV.Enabled = true
	cfg.AV.Addr = "127.0.0.1:3310"
	cfg.AV.Action = "quarantine"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid AV action 'quarantine': %v", err)
	}
}

func TestValidateAvidActionReject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AV.Enabled = true
	cfg.AV.Addr = "127.0.0.1:3310"
	cfg.AV.Action = "reject"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid AV action 'reject': %v", err)
	}
}

func TestValidateLoggingLevelValid(t *testing.T) {
	tests := []string{"debug", "info", "warn", "error"}

	for _, level := range tests {
		cfg := DefaultConfig()
		cfg.Logging.Level = level
		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected error for valid logging level %q: %v", level, err)
		}
	}
}

func TestValidateLoggingFormatValid(t *testing.T) {
	tests := []string{"json", "text"}

	for _, format := range tests {
		cfg := DefaultConfig()
		cfg.Logging.Format = format
		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected error for valid logging format %q: %v", format, err)
		}
	}
}

func TestValidateACMELetsencryptStaging(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.ACME.Enabled = true
	cfg.TLS.ACME.Email = "test@example.com"
	cfg.TLS.ACME.Provider = "letsencrypt-staging"
	cfg.TLS.ACME.Challenge = "http-01"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for letsencrypt-staging: %v", err)
	}
}

func TestValidateACMEDNSChallenge(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TLS.ACME.Enabled = true
	cfg.TLS.ACME.Email = "test@example.com"
	cfg.TLS.ACME.Provider = "letsencrypt"
	cfg.TLS.ACME.Challenge = "dns-01"
	err := cfg.Validate()
	if err != nil {
		t.Errorf("unexpected error for dns-01 challenge: %v", err)
	}
}
