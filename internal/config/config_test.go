package config

import (
	"os"
	"path/filepath"
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
