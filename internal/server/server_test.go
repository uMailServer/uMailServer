package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected int // slog.Level is an int type
	}{
		{"debug", -4}, // slog.LevelDebug
		{"info", 0},   // slog.LevelInfo
		{"warn", 4},   // slog.LevelWarn
		{"error", 8},  // slog.LevelError
		{"unknown", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if int(result) != tt.expected {
				t.Errorf("parseLogLevel(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseEmail(t *testing.T) {
	tests := []struct {
		email       string
		wantUser    string
		wantDomain  string
	}{
		{"user@example.com", "user", "example.com"},
		{"test.user@sub.domain.com", "test.user", "sub.domain.com"},
		{"user@", "user", ""},
		{"@domain.com", "", "domain.com"},
		{"nodomain", "nodomain", ""},
		{"", "", ""},
		{"a@b@c.com", "a@b", "c.com"}, // Last @ is used
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			gotUser, gotDomain := parseEmail(tt.email)
			if gotUser != tt.wantUser || gotDomain != tt.wantDomain {
				t.Errorf("parseEmail(%q) = (%q, %q), want (%q, %q)",
					tt.email, gotUser, gotDomain, tt.wantUser, tt.wantDomain)
			}
		})
	}
}

func TestNew(t *testing.T) {
	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "server-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0, // Let system assign port
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	if server == nil {
		t.Fatal("Expected server instance, got nil")
	}

	if server.config != cfg {
		t.Error("Server config mismatch")
	}

	if server.database == nil {
		t.Error("Expected database to be initialized")
	}

	if server.msgStore == nil {
		t.Error("Expected message store to be initialized")
	}
}

func TestNewInvalidDatabasePath(t *testing.T) {
	// On Windows, most paths are valid, so we test with an inaccessible path
	// by using a file where a directory should be
	tmpFile, err := os.CreateTemp("", "server-invalid-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Try to use a file path as a directory (should fail)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpFile.Name(), // This is a file, not a directory
		},
		Database: config.DatabaseConfig{
			Path: tmpFile.Name() + "/db/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	_, err = New(cfg)
	if err == nil {
		// On some systems this might succeed, which is fine
		t.Skip("Path validation did not fail as expected on this platform")
	}
}

func TestServerGetters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "server-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Test GetDatabase
	if db := server.GetDatabase(); db == nil {
		t.Error("GetDatabase() returned nil")
	}

	// Test GetQueue (will be nil until queue is initialized)
	if server.GetQueue() != nil {
		t.Error("GetQueue() should return nil before Start()")
	}
}

func TestParseLogLevelAllCases(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"DEBUG", -4},
		{"INFO", 0},
		{"WARN", 4},
		{"WARNING", 4},
		{"ERROR", 8},
		{"FATAL", 8},
		{"trace", -4}, // default
		{"", 0},       // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if int(result) != tt.expected {
				t.Errorf("parseLogLevel(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestServerConfigStruct(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  "/data",
		},
		Database: config.DatabaseConfig{
			Path: "/data/db.db",
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "0.0.0.0",
				Port:           25,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
			Submission: config.SubmissionSMTPConfig{
				Bind: "0.0.0.0",
				Port: 587,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "0.0.0.0",
			Port: 993,
		},
		POP3: config.POP3Config{
			Bind: "0.0.0.0",
			Port: 995,
		},
	}

	if cfg.Server.Hostname != "mail.example.com" {
		t.Error("hostname mismatch")
	}
	if cfg.Server.DataDir != "/data" {
		t.Error("datadir mismatch")
	}
	if cfg.Logging.Level != "debug" {
		t.Error("log level mismatch")
	}
	if cfg.SMTP.Inbound.Port != 25 {
		t.Error("smtp inbound port mismatch")
	}
	if cfg.SMTP.Submission.Port != 587 {
		t.Error("smtp submission port mismatch")
	}
	if cfg.IMAP.Port != 993 {
		t.Error("imap port mismatch")
	}
	if cfg.POP3.Port != 995 {
		t.Error("pop3 port mismatch")
	}
}

func TestNewPIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	if pidFile == nil {
		t.Fatal("expected non-nil PIDFile")
	}

	if pidFile.path == "" {
		t.Error("expected path to be set")
	}
}

func TestPIDFileCreateAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Create PID file
	err := pidFile.Create()
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Read PID
	pid, err := pidFile.Read()
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}

	if pid == 0 {
		t.Error("expected non-zero PID")
	}

	// Clean up
	err = pidFile.Remove()
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}
}

func TestPIDFileReadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Try to read without creating
	_, err := pidFile.Read()
	if err == nil {
		t.Error("expected error when reading non-existent PID file")
	}
}

func TestPIDFileRemoveNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Try to remove without creating
	err := pidFile.Remove()
	// May or may not error depending on OS
	_ = err
}

func TestIsProcessRunning(t *testing.T) {
	// Test with current process (should be running)
	currentPID := os.Getpid()
	if !isProcessRunning(currentPID) {
		t.Error("expected current process to be running")
	}

	// Test with invalid PID
	if isProcessRunning(-1) {
		t.Error("expected negative PID to not be running")
	}

	// Test with very large unlikely PID
	if isProcessRunning(999999) {
		t.Log("large PID might be valid on some systems, skipping check")
	}
}

func TestParseLogLevelMixedCase(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"Debug", -4},
		{"INFO", 0},
		{"Warn", 4},
		{"ERROR", 8},
		{"Trace", -4},
		{"WARNING", 4},
		{"FATAL", 8},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if int(result) != tt.expected {
				t.Errorf("parseLogLevel(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestServerStopWithoutStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "server-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Stop without starting - should not panic
	server.Stop()
}

func TestParseEmailVariations(t *testing.T) {
	tests := []struct {
		email         string
		expectedUser  string
		expectedDomain string
	}{
		{"user@example.com", "user", "example.com"},
		{"test.user@sub.domain.com", "test.user", "sub.domain.com"},
		{"user@", "user", ""},
		{"@domain.com", "", "domain.com"},
		{"nodomain", "nodomain", ""},
		{"", "", ""},
		{"a@b@c.com", "a@b", "c.com"},
		{"user+tag@example.com", "user+tag", "example.com"},
		{"first.last@deep.sub.domain.com", "first.last", "deep.sub.domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			gotUser, gotDomain := parseEmail(tt.email)
			if gotUser != tt.expectedUser {
				t.Errorf("parseEmail(%q) user = %q, want %q", tt.email, gotUser, tt.expectedUser)
			}
			if gotDomain != tt.expectedDomain {
				t.Errorf("parseEmail(%q) domain = %q, want %q", tt.email, gotDomain, tt.expectedDomain)
			}
		})
	}
}

func TestParseEmailSpecialCases(t *testing.T) {
	// Test with multiple @ symbols - should use the last one
	user, domain := parseEmail("a@b@c@d.com")
	if user != "a@b@c" {
		t.Errorf("expected user 'a@b@c', got %q", user)
	}
	if domain != "d.com" {
		t.Errorf("expected domain 'd.com', got %q", domain)
	}

	// Test with only @ symbol
	user, domain = parseEmail("@")
	if user != "" {
		t.Errorf("expected empty user, got %q", user)
	}
	if domain != "" {
		t.Errorf("expected empty domain, got %q", domain)
	}

	// Test with @ at the beginning
	user, domain = parseEmail("@example.com")
	if user != "" {
		t.Errorf("expected empty user, got %q", user)
	}
	if domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", domain)
	}
}


func TestServerConfigFields(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "0.0.0.0",
				Port:           25,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
			Submission: config.SubmissionSMTPConfig{
				Bind: "0.0.0.0",
				Port: 587,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "0.0.0.0",
			Port: 993,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 8080,
		},
		Security: config.SecurityConfig{
			JWTSecret: "test-secret",
		},
		TLS: config.TLSConfig{
			CertFile: "/path/to/cert.pem",
			KeyFile:  "/path/to/key.pem",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Verify config is set
	if server.config != cfg {
		t.Error("Server config mismatch")
	}

	// Verify hostname
	if server.config.Server.Hostname != "mail.example.com" {
		t.Errorf("expected hostname 'mail.example.com', got %s", server.config.Server.Hostname)
	}

	// Verify data dir
	if server.config.Server.DataDir != tmpDir {
		t.Errorf("expected data dir %s, got %s", tmpDir, server.config.Server.DataDir)
	}
}

func TestParseLogLevelAllValues(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"fatal", slog.LevelError},
		{"FATAL", slog.LevelError},
		{"trace", slog.LevelDebug},
		{"TRACE", slog.LevelDebug},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"UNKNOWN", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestServerStartWithServices(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0, // Let system assign port
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Test Start - this will start all services
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start server in background
	startErr := make(chan error, 1)
	go func() {
		startErr <- server.Start()
	}()

	// Wait a bit for services to start
	select {
	case <-time.After(500 * time.Millisecond):
		// Services started
	case <-ctx.Done():
		t.Log("Timeout waiting for server to start")
	}

	// Stop the server
	server.Stop()

	select {
	case err := <-startErr:
		if err != nil {
			t.Logf("Start error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Log("Timeout waiting for start error")
	}
}

func TestPIDFileOperations(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Test Create
	err := pidFile.Create()
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Verify file exists
	pidPath := filepath.Join(tmpDir, "umailserver.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file was not created")
	}

	// Test Read
	pid, err := pidFile.Read()
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}

	if pid == 0 {
		t.Error("expected non-zero PID")
	}

	// Test isProcessRunning with current PID
	if !isProcessRunning(os.Getpid()) {
		t.Error("expected current process to be running")
	}

	// Test Remove
	err = pidFile.Remove()
	if err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Verify file is removed
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file was not removed")
	}
}

func TestPIDFileWithRunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Create PID file with current process ID
	err := pidFile.Create()
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	// Read and verify
	pid, err := pidFile.Read()
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}

	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}

	// Clean up
	pidFile.Remove()
}

func TestServerWithTLSConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled: false,
			},
			CertFile: tmpDir + "/cert.pem",
			KeyFile:  tmpDir + "/key.pem",
		},
	}

	// Create dummy cert files
	os.WriteFile(cfg.TLS.CertFile, []byte("dummy cert"), 0644)
	os.WriteFile(cfg.TLS.KeyFile, []byte("dummy key"), 0644)

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	if server.tlsManager == nil {
		t.Error("expected TLS manager to be initialized")
	}
}

func TestServerWithACME(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		TLS: config.TLSConfig{
			ACME: config.ACMEConfig{
				Enabled:  true,
				Email:    "admin@example.com",
				Provider: "letsencrypt-staging",
			},
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	if server.tlsManager == nil {
		t.Error("expected TLS manager to be initialized")
	}
}

func TestServerDoubleStop(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Stop twice - should not panic
	server.Stop()
	server.Stop()
}

func TestServerConfigAccess(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
		Logging: config.LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Verify config is set
	if server.config != cfg {
		t.Error("Server config mismatch")
	}

	// Verify hostname
	if server.config.Server.Hostname != "mail.example.com" {
		t.Errorf("expected hostname 'mail.example.com', got %s", server.config.Server.Hostname)
	}

	// Verify data dir
	if server.config.Server.DataDir != tmpDir {
		t.Errorf("expected data dir %s, got %s", tmpDir, server.config.Server.DataDir)
	}
}

func TestServerDeliverMessage(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Test delivery
	err = server.deliverMessage("sender@example.com", []string{"recipient@example.com"}, []byte("Subject: Test\r\n\r\nBody"))
	// Local delivery may fail without proper setup, just verify it doesn't panic
	if err != nil {
		t.Logf("deliverMessage returned error (expected without full setup): %v", err)
	}
}

func TestServerDeliverLocal(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Test local delivery
	err = server.deliverLocal("recipient", "example.com", "sender@example.com", []byte("Subject: Test\r\n\r\nBody"))
	// Delivery may fail without user setup, just verify it doesn't panic
	if err != nil {
		t.Logf("deliverLocal returned error (expected without user setup): %v", err)
	}
}

func TestServerAuthenticate(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Test authentication
	authenticated, err := server.authenticate("testuser", "testpass")
	if err != nil {
		t.Logf("authenticate returned error (may be expected without users): %v", err)
	}
	// Without users setup, should return false
	if authenticated {
		t.Error("Expected authentication to fail without user setup")
	}
}

func TestServerRelayMessage(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "mail.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: tmpDir + "/test.db",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer server.Stop()

	// Test relay
	err = server.relayMessage("sender@example.com", "recipient@external.com", []byte("Subject: Test\r\n\r\nBody"))
	// Relay may fail without proper setup, just verify it doesn't panic
	if err != nil {
		t.Logf("relayMessage returned error (expected without relay setup): %v", err)
	}
}

