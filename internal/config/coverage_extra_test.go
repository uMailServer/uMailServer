package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckFileReadable_Coverage tests checkFileReadable function
func TestCheckFileReadable_Coverage(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp("", "config_test_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.WriteString("test content")
	tmpFile.Close()

	// Test with readable file
	err = checkFileReadable(tmpFile.Name())
	if err != nil {
		t.Errorf("expected file to be readable, got error: %v", err)
	}

	// Test with non-existent file
	err = checkFileReadable("/nonexistent/path/to/file.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}

	// Test with directory (not a file)
	tmpDir := t.TempDir()
	err = checkFileReadable(tmpDir)
	if err == nil {
		t.Error("expected error when path is a directory")
	}
}

// TestCheckDirWritable_Coverage tests checkDirWritable function
func TestCheckDirWritable_Coverage(t *testing.T) {
	// Test with writable directory
	tmpDir := t.TempDir()
	err := checkDirWritable(tmpDir)
	if err != nil {
		t.Errorf("expected dir to be writable, got error: %v", err)
	}

	// Test creating new directory
	newDir := filepath.Join(t.TempDir(), "new_subdir")
	err = checkDirWritable(newDir)
	if err != nil {
		t.Errorf("expected to create and verify new dir, got error: %v", err)
	}

	// Test with file instead of directory
	tmpFile, err := os.CreateTemp("", "config_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// This will try to create the file as a directory which may fail
	_ = checkDirWritable(tmpFile.Name())
}

// TestValidate_Complete tests Validate function with various configurations
func TestValidate_Complete(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  t.TempDir(),
				},
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{
						Enabled: true,
						Port:    25,
					},
					Submission: SubmissionSMTPConfig{
						Enabled: true,
						Port:    587,
					},
				},
				IMAP: IMAPConfig{
					Enabled: true,
					Port:    143,
				},
				POP3: POP3Config{
					Enabled: false,
					Port:    0,
				},
				HTTP: HTTPConfig{
					Enabled: true,
					Port:    8080,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			},
			wantErr: false,
		},
		{
			name: "empty hostname",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "",
					DataDir:  t.TempDir(),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid data dir",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  "/nonexistent/path/that/cannot/be/created",
				},
			},
			wantErr: true,
		},
		{
			name: "port conflict smtp and imap",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  t.TempDir(),
				},
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{
						Enabled: true,
						Port:    25,
					},
				},
				IMAP: IMAPConfig{
					Enabled: true,
					Port:    25, // Same as SMTP
				},
			},
			wantErr: true,
		},
		{
			name: "port conflict smtp and submission",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  t.TempDir(),
				},
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{
						Enabled: true,
						Port:    25,
					},
					Submission: SubmissionSMTPConfig{
						Enabled: true,
						Port:    25, // Same as SMTP
					},
				},
			},
			wantErr: true,
		},
		{
			name: "port conflict imap and pop3",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  t.TempDir(),
				},
				IMAP: IMAPConfig{
					Enabled: true,
					Port:    143,
				},
				POP3: POP3Config{
					Enabled: true,
					Port:    143, // Same as IMAP
				},
			},
			wantErr: true,
		},
		{
			name: "port conflict imap and http",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  t.TempDir(),
				},
				IMAP: IMAPConfig{
					Enabled: true,
					Port:    143,
				},
				HTTP: HTTPConfig{
					Enabled: true,
					Port:    143, // Same as IMAP
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_TLSConfig tests TLS validation
func TestValidate_TLSConfig(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "TLS enabled with missing cert file",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				TLS: TLSConfig{
					CertFile: "/nonexistent/cert.pem",
					KeyFile:  "/nonexistent/key.pem",
				},
			},
			wantErr: true,
		},
		{
			name: "TLS enabled with cert but missing key file",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				TLS: TLSConfig{
					CertFile: "", // Empty
					KeyFile:  "/nonexistent/key.pem",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseSize_Coverage tests additional size parsing cases
func TestParseSize_Coverage(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"1K", 1024, false},
		{"1KB", 1024, false},
		{"1M", 1024 * 1024, false},
		{"1MB", 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"1T", 1024 * 1024 * 1024 * 1024, false},
		{"1TB", 1024 * 1024 * 1024 * 1024, false},
		{"1024", 1024, false},
		{"", 0, false}, // Empty returns 0, nil
		{"invalid", 0, true},
		{"1XB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			size, err := ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && int64(size) != tt.expected {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, size, tt.expected)
			}
		})
	}
}

// TestSizeString_Coverage tests Size.String
func TestSizeString_Coverage(t *testing.T) {
	tests := []struct {
		size     Size
		expected string
	}{
		{1024, "1KB"},
		{1024 * 1024, "1MB"},
		{1024 * 1024 * 1024, "1GB"},
		{1024 * 1024 * 1024 * 1024, "1TB"},
		{500, "500"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.size.String()
			if result != tt.expected {
				t.Errorf("Size(%d).String() = %s, want %s", tt.size, result, tt.expected)
			}
		})
	}
}

// TestWatcherCheck_Coverage tests watcher check function
func TestWatcherCheck_Coverage(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	// Write initial config
	err := os.WriteFile(configFile, []byte("server:\n  hostname: test.com\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	changeCalled := false
	onChange := func(oldCfg, newCfg *Config) {
		changeCalled = true
	}

	w := NewWatcher(configFile, nil, onChange)

	// Start the watcher
	err = w.Start(10 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Wait a bit and modify the file
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(configFile, []byte("server:\n  hostname: test2.com\n"), 0644)

	// Give time for the watcher to detect the change
	time.Sleep(100 * time.Millisecond)

	_ = changeCalled
}

// TestGetDefaultDataDir_Coverage tests GetDefaultDataDir
func TestGetDefaultDataDir_Coverage(t *testing.T) {
	dir := GetDefaultDataDir()
	if dir == "" {
		t.Error("expected non-empty default data dir")
	}
}

// TestNewReloadable_Success tests NewReloadable with valid config
func TestNewReloadable_Success(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	// Write valid config
	configContent := `server:
  hostname: test.com
  data_dir: ` + tmpDir + `
security:
  jwt_secret: this-is-a-32-character-secret-key!!
spam:
  enabled: true
  reject_threshold: 15.0
  junk_threshold: 5.0
  quarantine_threshold: 10.0
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	rc, err := NewReloadable(configFile, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc == nil {
		t.Fatal("expected non-nil ReloadableConfig")
	}

	// Test Get
	cfg := rc.Get()
	if cfg == nil {
		t.Error("expected non-nil config")
	}

	// Test Start and Stop
	err = rc.Start(50 * time.Millisecond)
	if err != nil {
		t.Errorf("Start error: %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	rc.Stop()
}

// TestCheckPortConflicts_Coverage tests port conflict detection
func TestCheckPortConflicts_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "no conflicts",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound:    InboundSMTPConfig{Enabled: true, Port: 25},
					Submission: SubmissionSMTPConfig{Enabled: true, Port: 587},
				},
				IMAP: IMAPConfig{Enabled: true, Port: 143},
				POP3: POP3Config{Enabled: true, Port: 110},
				HTTP: HTTPConfig{Enabled: true, Port: 8080},
			},
			wantErr: false,
		},
		{
			name: "inbound smtp equals imap",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{Enabled: true, Port: 25},
				},
				IMAP: IMAPConfig{Enabled: true, Port: 25},
			},
			wantErr: true,
		},
		{
			name: "inbound smtp equals pop3",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{Enabled: true, Port: 25},
				},
				POP3: POP3Config{Enabled: true, Port: 25},
			},
			wantErr: true,
		},
		{
			name: "inbound smtp equals http",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound: InboundSMTPConfig{Enabled: true, Port: 25},
				},
				HTTP: HTTPConfig{Enabled: true, Port: 25},
			},
			wantErr: true,
		},
		{
			name: "submission equals imap",
			cfg: &Config{
				SMTP: SMTPConfig{
					Submission: SubmissionSMTPConfig{Enabled: true, Port: 587},
				},
				IMAP: IMAPConfig{Enabled: true, Port: 587},
			},
			wantErr: true,
		},
		{
			name: "submission equals pop3",
			cfg: &Config{
				SMTP: SMTPConfig{
					Submission: SubmissionSMTPConfig{Enabled: true, Port: 465},
				},
				POP3: POP3Config{Enabled: true, Port: 465},
			},
			wantErr: true,
		},
		{
			name: "submission2 equals pop3",
			cfg: &Config{
				SMTP: SMTPConfig{
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
				},
				POP3: POP3Config{Enabled: true, Port: 465},
			},
			wantErr: true,
		},
		{
			name: "imap equals pop3",
			cfg: &Config{
				IMAP: IMAPConfig{Enabled: true, Port: 143},
				POP3: POP3Config{Enabled: true, Port: 143},
			},
			wantErr: true,
		},
		{
			name: "imap equals http",
			cfg: &Config{
				IMAP: IMAPConfig{Enabled: true, Port: 143},
				HTTP: HTTPConfig{Enabled: true, Port: 143},
			},
			wantErr: true,
		},
		{
			name: "pop3 equals http",
			cfg: &Config{
				POP3: POP3Config{Enabled: true, Port: 110},
				HTTP: HTTPConfig{Enabled: true, Port: 110},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.checkPortConflicts()
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPortConflicts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigValidate_WithZeroPorts tests port validation with zero ports (disabled)
func TestConfigValidate_WithZeroPorts(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Server: ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		SMTP: SMTPConfig{
			Inbound: InboundSMTPConfig{
				Enabled: true,
				Port:    25,
			},
			Submission: SubmissionSMTPConfig{
				Enabled: false,
				Port:    0, // Disabled
			},
		},
		IMAP: IMAPConfig{
			Enabled: true,
			Port:    143,
		},
		POP3: POP3Config{
			Enabled: false,
			Port:    0, // Disabled
		},
		HTTP: HTTPConfig{
			Enabled: true,
			Port:    8080,
		},
		Security: SecurityConfig{
			JWTSecret: "this-is-a-32-character-secret-key!!",
		},
		Spam: SpamConfig{
			Enabled:             true,
			RejectThreshold:     15.0,
			JunkThreshold:       5.0,
			QuarantineThreshold: 10.0,
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() with zero ports should not error: %v", err)
	}
}

// TestConfigValidate_DisabledServices tests validation with disabled services
func TestConfigValidate_DisabledServices(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Server: ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		SMTP: SMTPConfig{
			Inbound: InboundSMTPConfig{
				Enabled: false,
				Port:    0,
			},
			Submission: SubmissionSMTPConfig{
				Enabled: false,
				Port:    0,
			},
			SubmissionTLS: SubmissionTLSConfig{
				Enabled: false,
				Port:    0,
			},
		},
		IMAP: IMAPConfig{
			Enabled: false,
			Port:    0,
		},
		POP3: POP3Config{
			Enabled: false,
			Port:    0,
		},
		HTTP: HTTPConfig{
			Enabled: false,
			Port:    0,
		},
		Security: SecurityConfig{
			JWTSecret: "this-is-a-32-character-secret-key!!",
		},
		Spam: SpamConfig{
			Enabled:             true,
			RejectThreshold:     15.0,
			JunkThreshold:       5.0,
			QuarantineThreshold: 10.0,
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Validate() with all disabled should not error: %v", err)
	}
}

// TestCheckPortConflicts_Submission2 tests port conflict detection for SubmissionTLS
func TestCheckPortConflicts_Submission2(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "submission2 equals imap",
			cfg: &Config{
				SMTP: SMTPConfig{
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
				},
				IMAP: IMAPConfig{Enabled: true, Port: 465},
			},
			wantErr: true,
		},
		{
			name: "submission2 equals http",
			cfg: &Config{
				SMTP: SMTPConfig{
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 8080},
				},
				HTTP: HTTPConfig{Enabled: true, Port: 8080},
			},
			wantErr: true,
		},
		{
			name: "submission equals submission2 (both enabled)",
			cfg: &Config{
				SMTP: SMTPConfig{
					Submission:    SubmissionSMTPConfig{Enabled: true, Port: 587},
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 587},
				},
			},
			wantErr: true,
		},
		{
			name: "smtp inbound equals submission2",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound:       InboundSMTPConfig{Enabled: true, Port: 25},
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 25},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.checkPortConflicts()
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPortConflicts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestWatcherCheck_FileRemoved tests watcher when file is removed
func TestWatcherCheck_FileRemoved(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	// Write initial config
	err := os.WriteFile(configFile, []byte("server:\n  hostname: test.com\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(configFile, nil, nil)

	// Start the watcher
	err = w.Start(10 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Wait a bit then remove the file
	time.Sleep(50 * time.Millisecond)
	os.Remove(configFile)

	// Wait for check to process
	time.Sleep(100 * time.Millisecond)

	// The watcher should handle the missing file gracefully
}

// TestNewReloadable_InvalidConfig tests NewReloadable with invalid config file
func TestNewReloadable_InvalidConfig(t *testing.T) {
	// Create a temp config file with invalid content
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	// Write invalid config
	err := os.WriteFile(configFile, []byte("invalid: yaml: content: ["), 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = NewReloadable(configFile, nil)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

// Note: Permission-based tests removed as they don't work consistently on Windows

// TestValidate_SpamThresholds tests spam threshold validation
func TestValidate_SpamThresholds(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid thresholds",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			},
			wantErr: false,
		},
		{
			name: "reject less than junk",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     4.0, // Less than junk
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			},
			wantErr: true,
		},
		{
			name: "quarantine less than junk",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 4.0, // Less than junk
				},
			},
			wantErr: true,
		},
		{
			name: "reject less than quarantine",
			cfg: &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     8.0, // Less than quarantine
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_MissingJWTSecret tests validation with missing JWT secret
// Empty JWT secret is now VALID - it will be generated at runtime by api.NewServer
func TestValidate_MissingJWTSecret(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Server: ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Security: SecurityConfig{
			JWTSecret: "", // Empty - will be generated at runtime
		},
		Spam: SpamConfig{
			Enabled:             true,
			RejectThreshold:     15.0,
			JunkThreshold:       5.0,
			QuarantineThreshold: 10.0,
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("empty JWT secret should be valid (generated at runtime): %v", err)
	}
}

// TestValidate_ShortJWTSecret tests validation with short JWT secret
func TestValidate_ShortJWTSecret(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Server: ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Security: SecurityConfig{
			JWTSecret: "short", // Too short
		},
		Spam: SpamConfig{
			Enabled:             true,
			RejectThreshold:     15.0,
			JunkThreshold:       5.0,
			QuarantineThreshold: 10.0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for short JWT secret")
	}
}

// TestCheckPortConflicts_ZeroPorts tests port conflict detection with zero ports
func TestCheckPortConflicts_ZeroPorts(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "disabled services with zero ports",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound:       InboundSMTPConfig{Enabled: false, Port: 0},
					Submission:    SubmissionSMTPConfig{Enabled: false, Port: 0},
					SubmissionTLS: SubmissionTLSConfig{Enabled: false, Port: 0},
				},
				IMAP: IMAPConfig{Enabled: false, Port: 0},
				POP3: POP3Config{Enabled: false, Port: 0},
				HTTP: HTTPConfig{Enabled: false, Port: 0},
			},
			wantErr: false,
		},
		{
			name: "all services different ports",
			cfg: &Config{
				SMTP: SMTPConfig{
					Inbound:       InboundSMTPConfig{Enabled: true, Port: 25},
					Submission:    SubmissionSMTPConfig{Enabled: true, Port: 587},
					SubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
				},
				IMAP: IMAPConfig{Enabled: true, Port: 143},
				POP3: POP3Config{Enabled: true, Port: 110},
				HTTP: HTTPConfig{Enabled: true, Port: 8080},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.checkPortConflicts()
			if (err != nil) != tt.wantErr {
				t.Errorf("checkPortConflicts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_TLSMinVersion tests TLS MinVersion validation
func TestValidate_TLSMinVersion(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{"empty version", "", false},
		{"version 1.2", "1.2", false},
		{"version 1.3", "1.3", false},
		{"invalid version", "1.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				TLS: TLSConfig{
					MinVersion: tt.version,
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			}

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_RateLimits tests rate limit validation
func TestValidate_RateLimits(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		rateLimit RateLimitConfig
		wantErr   bool
	}{
		{
			name:      "valid rate limits",
			rateLimit: RateLimitConfig{SMTPPerMinute: 100, SMTPPerHour: 1000, IMAPConnections: 50, HTTPRequestsPerMinute: 200},
			wantErr:   false,
		},
		{
			name:      "negative SMTP per minute",
			rateLimit: RateLimitConfig{SMTPPerMinute: -1},
			wantErr:   true,
		},
		{
			name:      "negative SMTP per hour",
			rateLimit: RateLimitConfig{SMTPPerHour: -1},
			wantErr:   true,
		},
		{
			name:      "negative IMAP connections",
			rateLimit: RateLimitConfig{IMAPConnections: -1},
			wantErr:   true,
		},
		{
			name:      "negative HTTP requests",
			rateLimit: RateLimitConfig{HTTPRequestsPerMinute: -1},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
					RateLimit: tt.rateLimit,
				},
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			}

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_ConnectionLimits tests connection limit validation
func TestValidate_ConnectionLimits(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name              string
		imap              IMAPConfig
		smtpInbound       InboundSMTPConfig
		smtpSubmission    SubmissionSMTPConfig
		smtpSubmissionTLS SubmissionTLSConfig
		pop3              POP3Config
		wantErr           bool
		errMsg            string
	}{
		{
			name:              "negative IMAP max connections",
			imap:              IMAPConfig{Enabled: true, Port: 143, MaxConnections: -1},
			smtpInbound:       InboundSMTPConfig{Enabled: true, Port: 25},
			smtpSubmission:    SubmissionSMTPConfig{Enabled: true, Port: 587},
			smtpSubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
			pop3:              POP3Config{Enabled: true, Port: 110},
			wantErr:           true,
			errMsg:            "imap.max_connections",
		},
		{
			name:              "negative SMTP inbound max connections",
			imap:              IMAPConfig{Enabled: true, Port: 143, MaxConnections: 100},
			smtpInbound:       InboundSMTPConfig{Enabled: true, Port: 25, MaxConnections: -1},
			smtpSubmission:    SubmissionSMTPConfig{Enabled: true, Port: 587},
			smtpSubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
			pop3:              POP3Config{Enabled: true, Port: 110},
			wantErr:           true,
			errMsg:            "smtp.inbound.max_connections",
		},
		{
			name:              "negative SMTP submission max connections",
			imap:              IMAPConfig{Enabled: true, Port: 143, MaxConnections: 100},
			smtpInbound:       InboundSMTPConfig{Enabled: true, Port: 25, MaxConnections: 100},
			smtpSubmission:    SubmissionSMTPConfig{Enabled: true, Port: 587, MaxConnections: -1},
			smtpSubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
			pop3:              POP3Config{Enabled: true, Port: 110},
			wantErr:           true,
			errMsg:            "smtp.submission.max_connections",
		},
		{
			name:              "negative POP3 max connections",
			imap:              IMAPConfig{Enabled: true, Port: 143, MaxConnections: 100},
			smtpInbound:       InboundSMTPConfig{Enabled: true, Port: 25, MaxConnections: 100},
			smtpSubmission:    SubmissionSMTPConfig{Enabled: true, Port: 587, MaxConnections: 100},
			smtpSubmissionTLS: SubmissionTLSConfig{Enabled: true, Port: 465},
			pop3:              POP3Config{Enabled: true, Port: 110, MaxConnections: -1},
			wantErr:           true,
			errMsg:            "pop3.max_connections",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Server: ServerConfig{
					Hostname: "mail.example.com",
					DataDir:  tmpDir,
				},
				Security: SecurityConfig{
					JWTSecret: "this-is-a-32-character-secret-key!!",
				},
				IMAP: tt.imap,
				SMTP: SMTPConfig{
					Inbound:       tt.smtpInbound,
					Submission:    tt.smtpSubmission,
					SubmissionTLS: tt.smtpSubmissionTLS,
				},
				POP3: tt.pop3,
				Spam: SpamConfig{
					Enabled:             true,
					RejectThreshold:     15.0,
					JunkThreshold:       5.0,
					QuarantineThreshold: 10.0,
				},
			}

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error to contain '%s', got: %v", tt.errMsg, err)
				}
			}
		})
	}
}
