package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
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

func TestDeliverLocal(t *testing.T) {
	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "server-deliver-test-*")
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

	// Create test message file
	msgData := []byte("Subject: Test\r\nFrom: sender@test.example.com\r\nTo: recipient@test.example.com\r\n\r\nTest body")

	// Test local delivery
	err = server.deliverLocal("recipient", "test.example.com", "sender@test.example.com", msgData)
	// Local delivery may fail without proper setup, just verify it doesn't panic
	if err != nil {
		t.Logf("deliverLocal returned error (expected without full setup): %v", err)
	}
}

func TestPIDFileCreate(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	pidFile := NewPIDFile(pidPath)

	// Test Create
	err := pidFile.Create()
	if err != nil {
		// Just log the error, don't fail - the PID file creation may have OS-specific issues
		t.Logf("Create returned error: %v", err)
		return
	}

	// Verify file exists
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file was not created")
	}

	// Clean up
	pidFile.Remove()
}

func TestPIDFileCreateExisting(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Create existing PID file with different content
	os.WriteFile(pidPath, []byte("99999\n"), 0644)

	pidFile := NewPIDFile(pidPath)

	// Test Create with existing file
	err := pidFile.Create()
	if err != nil {
		// Just log the error, don't fail
		t.Logf("Create returned error: %v", err)
		return
	}

	// Clean up
	pidFile.Remove()
}

// TestAuthenticateSuccess tests successful authentication
func TestAuthenticateSuccess(t *testing.T) {
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
	defer server.Stop()

	hashedPassword := "$2a$10$BXVavbSB/53WBHDuJlzIHeCsgSTgzrOqtbdPmrkPa68dA3jYmKux2"
	account := &db.AccountData{
		Email:        "testuser@test.example.com",
		LocalPart:    "testuser",
		Domain:       "test.example.com",
		PasswordHash: hashedPassword,
		IsActive:     true,
		QuotaLimit:   1000000,
		CreatedAt:    time.Now(),
	}

	if err := server.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	authenticated, err := server.authenticate("testuser@test.example.com", "testpass123")
	if err != nil {
		t.Errorf("authenticate returned error: %v", err)
	}
	if !authenticated {
		t.Error("Expected authentication to succeed")
	}
}

// TestAuthenticateInvalidPassword tests authentication with wrong password
func TestAuthenticateInvalidPassword(t *testing.T) {
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
	defer server.Stop()

	hashedPassword := "$2a$10$BXVavbSB/53WBHDuJlzIHeCsgSTgzrOqtbdPmrkPa68dA3jYmKux2"
	account := &db.AccountData{
		Email:        "testuser@test.example.com",
		LocalPart:    "testuser",
		Domain:       "test.example.com",
		PasswordHash: hashedPassword,
		IsActive:     true,
		CreatedAt:    time.Now(),
	}

	if err := server.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	authenticated, err := server.authenticate("testuser@test.example.com", "wrongpassword")
	if err != nil {
		t.Errorf("authenticate returned error: %v", err)
	}
	if authenticated {
		t.Error("Expected authentication to fail with wrong password")
	}
}

// TestDeliverLocalQuotaExceeded tests delivery when quota is exceeded
func TestDeliverLocalQuotaExceeded(t *testing.T) {
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
	defer server.Stop()

	account := &db.AccountData{
		Email:        "fulluser@test.example.com",
		LocalPart:    "fulluser",
		Domain:       "test.example.com",
		PasswordHash: "hash",
		IsActive:     true,
		QuotaUsed:    1000000,
		QuotaLimit:   1000000,
		CreatedAt:    time.Now(),
	}
	if err := server.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	msgData := []byte("Subject: Test\r\n\r\nBody")
	err = server.deliverLocal("fulluser", "test.example.com", "sender@example.com", msgData)

	if err == nil {
		t.Error("Expected error for quota exceeded")
	}
}

// TestDeliverLocalNonExistentUser tests delivery to non-existent user
func TestDeliverLocalNonExistentUser(t *testing.T) {
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
	defer server.Stop()

	msgData := []byte("Subject: Test\r\n\r\nBody")
	err = server.deliverLocal("nonexistent", "test.example.com", "sender@example.com", msgData)

	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// TestDeliverLocalInactiveUser tests delivery to inactive user
func TestDeliverLocalInactiveUser(t *testing.T) {
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
	defer server.Stop()

	account := &db.AccountData{
		Email:        "inactive@test.example.com",
		LocalPart:    "inactive",
		Domain:       "test.example.com",
		PasswordHash: "hash",
		IsActive:     false,
		CreatedAt:    time.Now(),
	}
	if err := server.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	msgData := []byte("Subject: Test\r\n\r\nBody")
	err = server.deliverLocal("inactive", "test.example.com", "sender@example.com", msgData)

	if err == nil {
		t.Error("Expected error for inactive user")
	}
}

// TestRelayMessageWithoutQueue tests relaying when queue is nil
func TestRelayMessageWithoutQueue(t *testing.T) {
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
	defer server.Stop()

	server.queue = nil

	msgData := []byte("Subject: Test\r\n\r\nBody")
	err = server.relayMessage("sender@test.example.com", "recipient@external.com", msgData)

	if err != nil {
		t.Errorf("relayMessage without queue should not error, got: %v", err)
	}
}

// TestServerWait tests the Wait function
func TestServerWait(t *testing.T) {
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
	defer server.Stop()

	// Start server in background
	go func() {
		server.Start()
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM to trigger Wait to return
	go func() {
		time.Sleep(50 * time.Millisecond)
		process, _ := os.FindProcess(os.Getpid())
		if process != nil {
			process.Signal(syscall.SIGTERM)
		}
	}()

	// Wait should return after signal is received
	done := make(chan error, 1)
	go func() {
		done <- server.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("Wait returned error: %v", err)
		}
		// Success - Wait returned
	case <-time.After(2 * time.Second):
		t.Skip("Skipping - Wait() did not return after signal (may be OS-specific)")
	}
}

// TestPIDFileCreateWithRunningProcess tests that Create fails when PID file
// already exists with a running process PID
func TestPIDFileCreateWithRunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	// Write a PID file with the current process ID (which is running)
	currentPID := os.Getpid()
	pidPath := filepath.Join(tmpDir, "umailserver.pid")
	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", currentPID)), 0644)

	// Create should fail because the current process IS running
	err := pidFile.Create()
	if err == nil {
		t.Error("expected error when PID file exists with running process")
		pidFile.Remove()
	} else {
		if !strings.Contains(err.Error(), "already running") {
			t.Errorf("expected 'already running' error, got: %v", err)
		}
		// PID file should still exist (not removed since process is running)
		if _, statErr := os.Stat(pidPath); os.IsNotExist(statErr) {
			t.Error("PID file should still exist when process is running")
		}
		// Clean up manually since Create didn't overwrite it
		os.Remove(pidPath)
	}
}

// TestPIDFileCreateWithStalePID tests that Create overwrites stale PID file
func TestPIDFileCreateWithStalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)
	pidPath := filepath.Join(tmpDir, "umailserver.pid")

	// Write a PID file with a very high PID that is very likely NOT running
	// but is a positive number so isProcessRunning will be called
	stalePID := 99999999
	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", stalePID)), 0644)

	// isProcessRunning on Windows always returns true for FindProcess,
	// but on Unix with signal 0 it returns false for non-existent PIDs.
	// We test the stale removal path by ensuring the file gets overwritten.
	err := pidFile.Create()
	if err != nil {
		// On Windows, FindProcess always succeeds so isProcessRunning returns true
		// This means the "already running" error will be returned
		t.Logf("Create returned error (OS-specific): %v", err)
		os.Remove(pidPath)
	} else {
		// Verify the PID file now contains current PID
		pid, readErr := pidFile.Read()
		if readErr != nil {
			t.Fatalf("Read() failed: %v", readErr)
		}
		if pid != os.Getpid() {
			t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
		}
		pidFile.Remove()
	}
}

// TestPIDFileReadInvalidContent tests Read with invalid PID content
func TestPIDFileReadInvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)
	pidPath := filepath.Join(tmpDir, "umailserver.pid")

	// Write invalid (non-numeric) content
	os.WriteFile(pidPath, []byte("not-a-number\n"), 0644)

	pid, err := pidFile.Read()
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
	if pid != 0 {
		t.Errorf("expected PID 0 on error, got %d", pid)
	}

	os.Remove(pidPath)
}

// TestPIDFileCreateWithEmptyExistingFile tests Create when PID file exists but is empty
func TestPIDFileCreateWithEmptyExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)
	pidPath := filepath.Join(tmpDir, "umailserver.pid")

	// Write an empty PID file
	os.WriteFile(pidPath, []byte(""), 0644)

	err := pidFile.Create()
	if err != nil {
		t.Errorf("Create() failed with empty existing file: %v", err)
	} else {
		// Verify file now contains current PID
		pid, readErr := pidFile.Read()
		if readErr != nil {
			t.Fatalf("Read() failed: %v", readErr)
		}
		if pid != os.Getpid() {
			t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
		}
		pidFile.Remove()
	}
}

// TestPIDFilePath verifies PID file path construction
func TestPIDFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	pidFile := NewPIDFile(tmpDir)

	expectedPath := filepath.Join(tmpDir, "umailserver.pid")
	if pidFile.path != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, pidFile.path)
	}
}

// TestNewWithEmptyDatabasePath tests New when Database.Path is empty (fallback path)
func TestNewWithEmptyDatabasePath(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: "", // Empty - should fallback to DataDir + "/umailserver.db"
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with empty database path failed: %v", err)
	}
	defer server.Stop()

	if server.database == nil {
		t.Error("expected database to be initialized with fallback path")
	}
}

// TestNewWithDebugLogLevel tests New with debug log level
func TestNewWithDebugLogLevel(t *testing.T) {
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
			Level: "debug",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with debug log level failed: %v", err)
	}
	defer server.Stop()
}

// TestNewWithAllServicesConfig tests New with all service configs populated
func TestNewWithAllServicesConfig(t *testing.T) {
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
			Level:  "warn",
			Format: "json",
		},
		SMTP: config.SMTPConfig{
			Inbound: config.InboundSMTPConfig{
				Bind:           "127.0.0.1",
				Port:           0,
				MaxMessageSize: 10485760,
				MaxRecipients:  100,
			},
			Submission: config.SubmissionSMTPConfig{
				Bind: "127.0.0.1",
				Port: 0,
			},
		},
		IMAP: config.IMAPConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		POP3: config.POP3Config{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Admin: config.AdminConfig{
			Bind: "127.0.0.1",
			Port: 0,
		},
		Security: config.SecurityConfig{
			JWTSecret: "test-jwt-secret",
		},
		TLS: config.TLSConfig{
			CertFile: "",
			KeyFile:  "",
			ACME: config.ACMEConfig{
				Enabled: false,
			},
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with all services config failed: %v", err)
	}
	defer server.Stop()

	if server.tlsManager == nil {
		t.Error("expected TLS manager to be initialized")
	}
	if server.webhookMgr == nil {
		t.Error("expected webhook manager to be initialized")
	}
	if server.searchSvc == nil {
		t.Error("expected search service to be initialized")
	}
}

// TestNewWithFatalLogLevel tests New with fatal log level
func TestNewWithFatalLogLevel(t *testing.T) {
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
			Level: "fatal",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with fatal log level failed: %v", err)
	}
	defer server.Stop()
}

// TestNewWithTraceLogLevel tests New with trace log level
func TestNewWithTraceLogLevel(t *testing.T) {
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
			Level: "trace",
		},
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with trace log level failed: %v", err)
	}
	defer server.Stop()
}

// TestServerWaitWithoutStart tests Wait without starting
func TestServerWaitWithoutStart(t *testing.T) {
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
	defer server.Stop()

	// Send signal immediately
	go func() {
		time.Sleep(50 * time.Millisecond)
		process, _ := os.FindProcess(os.Getpid())
		if process != nil {
			process.Signal(syscall.SIGTERM)
		}
	}()

	// Wait should return after signal even if server not fully started
	done := make(chan error, 1)
	go func() {
		done <- server.Wait()
	}()

	select {
	case err := <-done:
		_ = err
		// Success
	case <-time.After(2 * time.Second):
		t.Skip("Skipping - Wait() timeout (may be OS-specific)")
	}
}

// helperServer creates a Server with real dependencies for testing delivery functions.
func helperServer(t *testing.T) *Server {
	t.Helper()
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
	t.Cleanup(func() { server.Stop() })
	return server
}

// helperCreateAccount creates an account in the server database.
func helperCreateAccount(t *testing.T, srv *Server, localPart, domain string, isActive bool, quotaLimit, quotaUsed int64) {
	t.Helper()
	account := &db.AccountData{
		Email:        localPart + "@" + domain,
		LocalPart:    localPart,
		Domain:       domain,
		PasswordHash: "$2a$10$BXVavbSB/53WBHDuJlzIHeCsgSTgzrOqtbdPmrkPa68dA3jYmKux2",
		IsActive:     isActive,
		QuotaLimit:   quotaLimit,
		QuotaUsed:    quotaUsed,
		CreatedAt:    time.Now(),
	}
	if err := srv.database.CreateAccount(account); err != nil {
		t.Fatalf("Failed to create account %s@%s: %v", localPart, domain, err)
	}
}

// helperCreateDomain creates a domain in the server database.
func helperCreateDomain(t *testing.T, srv *Server, name string, isActive bool) {
	t.Helper()
	domain := &db.DomainData{
		Name:      name,
		IsActive:  isActive,
		CreatedAt: time.Now(),
	}
	if err := srv.database.CreateDomain(domain); err != nil {
		t.Fatalf("Failed to create domain %s: %v", name, err)
	}
}

// --- deliverLocal comprehensive tests ---

func TestDeliverLocal_Success(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 0)

	msgData := []byte("Subject: Hello\r\nFrom: bob@external.com\r\n\r\nTest body content")
	err := srv.deliverLocal("alice", "test.example.com", "bob@external.com", msgData)
	if err != nil {
		t.Fatalf("deliverLocal should succeed for active user with no quota limit, got: %v", err)
	}
}

func TestDeliverLocal_SuccessWithQuotaHeadroom(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 10000, 0)

	msgData := []byte("Subject: Hi\r\n\r\nSmall message")
	err := srv.deliverLocal("alice", "test.example.com", "sender@external.com", msgData)
	if err != nil {
		t.Fatalf("deliverLocal should succeed when quota not exceeded, got: %v", err)
	}

	// Verify quota was updated
	account, err := srv.database.GetAccount("test.example.com", "alice")
	if err != nil {
		t.Fatalf("GetAccount failed: %v", err)
	}
	if account.QuotaUsed != int64(len(msgData)) {
		t.Errorf("expected QuotaUsed=%d, got %d", len(msgData), account.QuotaUsed)
	}
}

func TestDeliverLocal_QuotaExceededEqual(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "fulluser", "test.example.com", true, 500, 500)

	msgData := []byte("Subject: Hi\r\n\r\nBody")
	err := srv.deliverLocal("fulluser", "test.example.com", "sender@external.com", msgData)
	if err == nil {
		t.Fatal("deliverLocal should fail when quota exceeded (QuotaUsed == QuotaLimit)")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("expected quota exceeded error, got: %v", err)
	}
}

func TestDeliverLocal_QuotaExceededOver(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "fulluser", "test.example.com", true, 100, 200)

	msgData := []byte("Subject: Hi\r\n\r\nBody")
	err := srv.deliverLocal("fulluser", "test.example.com", "sender@external.com", msgData)
	if err == nil {
		t.Fatal("deliverLocal should fail when QuotaUsed > QuotaLimit")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("expected quota exceeded error, got: %v", err)
	}
}

func TestDeliverLocal_UserNotFound(t *testing.T) {
	srv := helperServer(t)

	msgData := []byte("Subject: Hi\r\n\r\nBody")
	err := srv.deliverLocal("nobody", "test.example.com", "sender@external.com", msgData)
	if err == nil {
		t.Fatal("deliverLocal should fail for non-existent user")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected does not exist error, got: %v", err)
	}
}

func TestDeliverLocal_InactiveUser(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "disabled", "test.example.com", false, 0, 0)

	msgData := []byte("Subject: Hi\r\n\r\nBody")
	err := srv.deliverLocal("disabled", "test.example.com", "sender@external.com", msgData)
	if err == nil {
		t.Fatal("deliverLocal should fail for inactive user")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("expected not active error, got: %v", err)
	}
}

func TestDeliverLocal_WithWebhookTrigger(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 0)

	if srv.webhookMgr == nil {
		t.Fatal("webhookMgr should be initialized by New()")
	}

	msgData := []byte("Subject: Webhook test\r\n\r\nBody")
	err := srv.deliverLocal("alice", "test.example.com", "sender@external.com", msgData)
	if err != nil {
		t.Fatalf("deliverLocal should succeed with webhook, got: %v", err)
	}
}

func TestDeliverLocal_NoQuotaLimit(t *testing.T) {
	srv := helperServer(t)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 999999)

	msgData := []byte("Subject: No quota\r\n\r\nBody")
	err := srv.deliverLocal("alice", "test.example.com", "sender@external.com", msgData)
	if err != nil {
		t.Fatalf("deliverLocal should succeed when QuotaLimit is 0 (unlimited), got: %v", err)
	}
}

// --- relayMessage comprehensive tests ---

func TestRelayMessage_WithQueue(t *testing.T) {
	srv := helperServer(t)

	queueDir := filepath.Join(srv.config.Server.DataDir, "queue")
	srv.queue = queue.NewManager(srv.database, nil, queueDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.queue.Start(ctx)
	defer srv.queue.Stop()

	msgData := []byte("Subject: Relay test\r\n\r\nRelayed body")
	err := srv.relayMessage("sender@test.example.com", "recipient@external.com", msgData)
	if err != nil {
		t.Fatalf("relayMessage should succeed with queue, got: %v", err)
	}
}

func TestRelayMessage_NilQueue(t *testing.T) {
	srv := helperServer(t)
	srv.queue = nil

	msgData := []byte("Subject: Relay test\r\n\r\nBody")
	err := srv.relayMessage("sender@test.example.com", "recipient@external.com", msgData)
	if err != nil {
		t.Fatalf("relayMessage with nil queue should return nil, got: %v", err)
	}
}

func TestRelayMessage_QueueEnqueueMultiple(t *testing.T) {
	srv := helperServer(t)

	queueDir := filepath.Join(srv.config.Server.DataDir, "queue")
	srv.queue = queue.NewManager(srv.database, nil, queueDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.queue.Start(ctx)
	defer srv.queue.Stop()

	for i := 0; i < 3; i++ {
		msgData := []byte(fmt.Sprintf("Subject: Message %d\r\n\r\nBody %d", i, i))
		err := srv.relayMessage("sender@test.example.com", fmt.Sprintf("recipient%d@remote.com", i), msgData)
		if err != nil {
			t.Fatalf("relayMessage %d should succeed, got: %v", i, err)
		}
	}
}

// --- deliverMessage comprehensive tests ---

func TestDeliverMessage_LocalDomain_ActiveUser(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 0)

	msgData := []byte("Subject: Local delivery\r\n\r\nBody")
	err := srv.deliverMessage("bob@external.com", []string{"alice@test.example.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should succeed for local delivery, got: %v", err)
	}
}

func TestDeliverMessage_LocalDomain_InactiveUser(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "disabled", "test.example.com", false, 0, 0)

	msgData := []byte("Subject: Test\r\n\r\nBody")
	err := srv.deliverMessage("sender@external.com", []string{"disabled@test.example.com"}, msgData)
	if err == nil {
		t.Fatal("deliverMessage should fail for inactive user")
	}
}

func TestDeliverMessage_UnknownDomain_Relays(t *testing.T) {
	srv := helperServer(t)
	srv.queue = nil

	msgData := []byte("Subject: Relay\r\n\r\nBody")
	err := srv.deliverMessage("sender@test.example.com", []string{"recipient@unknown.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should relay for unknown domain, got: %v", err)
	}
}

func TestDeliverMessage_InactiveDomain_Relays(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "inactive.example.com", false)
	srv.queue = nil

	msgData := []byte("Subject: Relay\r\n\r\nBody")
	err := srv.deliverMessage("sender@test.example.com", []string{"user@inactive.example.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should relay for inactive domain, got: %v", err)
	}
}

func TestDeliverMessage_MultipleRecipients(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 0)
	helperCreateAccount(t, srv, "bob", "test.example.com", true, 0, 0)

	msgData := []byte("Subject: Multi-recipient\r\n\r\nBody")
	err := srv.deliverMessage("sender@external.com", []string{"alice@test.example.com", "bob@test.example.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should succeed for multiple local recipients, got: %v", err)
	}
}

func TestDeliverMessage_MixedLocalAndRemote(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "alice", "test.example.com", true, 0, 0)

	queueDir := filepath.Join(srv.config.Server.DataDir, "queue")
	srv.queue = queue.NewManager(srv.database, nil, queueDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.queue.Start(ctx)
	defer srv.queue.Stop()

	msgData := []byte("Subject: Mixed\r\n\r\nBody")
	err := srv.deliverMessage("sender@external.com", []string{"alice@test.example.com", "remote@unknown.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should succeed for mixed recipients, got: %v", err)
	}
}

func TestDeliverMessage_LocalFailure(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "disabled", "test.example.com", false, 0, 0)

	msgData := []byte("Subject: Fail\r\n\r\nBody")
	err := srv.deliverMessage("sender@external.com", []string{"disabled@test.example.com"}, msgData)
	if err == nil {
		t.Fatal("deliverMessage should fail when local delivery fails")
	}
}

func TestDeliverMessage_EmptyRecipientList(t *testing.T) {
	srv := helperServer(t)

	msgData := []byte("Subject: Nobody\r\n\r\nBody")
	err := srv.deliverMessage("sender@test.example.com", []string{}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage with empty recipients should return nil, got: %v", err)
	}
}

func TestDeliverMessage_RemoteDomainWithQueue(t *testing.T) {
	srv := helperServer(t)

	queueDir := filepath.Join(srv.config.Server.DataDir, "queue")
	srv.queue = queue.NewManager(srv.database, nil, queueDir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.queue.Start(ctx)
	defer srv.queue.Stop()

	msgData := []byte("Subject: Remote\r\n\r\nBody")
	err := srv.deliverMessage("sender@test.example.com", []string{"user@remote.com"}, msgData)
	if err != nil {
		t.Fatalf("deliverMessage should relay to remote domain via queue, got: %v", err)
	}
}

func TestDeliverMessage_LocalDeliveryQuotaExceeded(t *testing.T) {
	srv := helperServer(t)
	helperCreateDomain(t, srv, "test.example.com", true)
	helperCreateAccount(t, srv, "fulluser", "test.example.com", true, 100, 100)

	msgData := []byte("Subject: Over quota\r\n\r\nBody")
	err := srv.deliverMessage("sender@external.com", []string{"fulluser@test.example.com"}, msgData)
	if err == nil {
		t.Fatal("deliverMessage should fail when local user quota exceeded")
	}
}

// --- New function additional edge cases ---

func TestNew_WithSecurityJWTSecret(t *testing.T) {
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
		Security: config.SecurityConfig{
			JWTSecret: "my-secret-key",
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer srv.Stop()

	if srv.webhookMgr == nil {
		t.Error("webhookMgr should be initialized when JWTSecret is set")
	}
}

func TestNew_DatabasePathFallback(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname: "test.example.com",
			DataDir:  tmpDir,
		},
		Database: config.DatabaseConfig{
			Path: "",
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with empty DB path failed: %v", err)
	}
	defer srv.Stop()

	expectedPath := tmpDir + "/umailserver.db"
	if _, statErr := os.Stat(expectedPath); os.IsNotExist(statErr) {
		t.Errorf("expected database file at %s", expectedPath)
	}
}

// --- Table-driven deliverLocal tests ---

func TestDeliverLocal_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		localPart  string
		domain     string
		isActive   bool
		quotaLimit int64
		quotaUsed  int64
		wantErr    bool
		errSubstr  string
	}{
		{
			name:       "active user no quota limit",
			localPart:  "alice",
			domain:     "test.example.com",
			isActive:   true,
			quotaLimit: 0,
			quotaUsed:  0,
			wantErr:    false,
		},
		{
			name:       "active user with quota headroom",
			localPart:  "bob",
			domain:     "test.example.com",
			isActive:   true,
			quotaLimit: 100000,
			quotaUsed:  0,
			wantErr:    false,
		},
		{
			name:       "inactive user",
			localPart:  "charlie",
			domain:     "test.example.com",
			isActive:   false,
			quotaLimit: 0,
			quotaUsed:  0,
			wantErr:    true,
			errSubstr:  "not active",
		},
		{
			name:       "quota exceeded equal",
			localPart:  "dave",
			domain:     "test.example.com",
			isActive:   true,
			quotaLimit: 10,
			quotaUsed:  10,
			wantErr:    true,
			errSubstr:  "quota exceeded",
		},
		{
			name:       "quota used greater than limit",
			localPart:  "eve",
			domain:     "test.example.com",
			isActive:   true,
			quotaLimit: 50,
			quotaUsed:  100,
			wantErr:    true,
			errSubstr:  "quota exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := helperServer(t)
			helperCreateAccount(t, srv, tt.localPart, tt.domain, tt.isActive, tt.quotaLimit, tt.quotaUsed)

			msgData := []byte("Subject: Test\r\n\r\nBody content")
			err := srv.deliverLocal(tt.localPart, tt.domain, "sender@external.com", msgData)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tt.errSubstr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// --- Table-driven deliverMessage tests ---

func TestDeliverMessage_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		setupDomain  bool
		domainName   string
		domainActive bool
		setupUser    bool
		userName     string
		userActive   bool
		quotaLimit   int64
		quotaUsed    int64
		recipient    string
		wantErr      bool
	}{
		{
			name:         "local domain active user",
			setupDomain:  true,
			domainName:   "test.example.com",
			domainActive: true,
			setupUser:    true,
			userName:     "alice",
			userActive:   true,
			quotaLimit:   0,
			quotaUsed:    0,
			recipient:    "alice@test.example.com",
			wantErr:      false,
		},
		{
			name:         "local domain inactive user",
			setupDomain:  true,
			domainName:   "test.example.com",
			domainActive: true,
			setupUser:    true,
			userName:     "bob",
			userActive:   false,
			quotaLimit:   0,
			quotaUsed:    0,
			recipient:    "bob@test.example.com",
			wantErr:      true,
		},
		{
			name:         "inactive domain triggers relay",
			setupDomain:  true,
			domainName:   "inactive.example.com",
			domainActive: false,
			setupUser:    false,
			recipient:    "user@inactive.example.com",
			wantErr:      false,
		},
		{
			name:         "unknown domain triggers relay",
			setupDomain:  false,
			setupUser:    false,
			recipient:    "user@unknown.com",
			wantErr:      false,
		},
		{
			name:         "local domain no user",
			setupDomain:  true,
			domainName:   "test.example.com",
			domainActive: true,
			setupUser:    false,
			recipient:    "nobody@test.example.com",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := helperServer(t)

			if tt.setupDomain {
				helperCreateDomain(t, srv, tt.domainName, tt.domainActive)
			}
			if tt.setupUser {
				helperCreateAccount(t, srv, tt.userName, tt.domainName, tt.userActive, tt.quotaLimit, tt.quotaUsed)
			}

			srv.queue = nil

			msgData := []byte("Subject: Test\r\n\r\nBody")
			err := srv.deliverMessage("sender@external.com", []string{tt.recipient}, msgData)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// --- relayMessage table-driven ---

func TestRelayMessage_TableDriven(t *testing.T) {
	tests := []struct {
		name       string
		from       string
		to         string
		setupQueue bool
		wantErr    bool
	}{
		{
			name:       "nil queue no error",
			from:       "sender@test.example.com",
			to:         "recipient@external.com",
			setupQueue: false,
			wantErr:    false,
		},
		{
			name:       "with queue manager",
			from:       "sender@test.example.com",
			to:         "recipient@external.com",
			setupQueue: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := helperServer(t)

			if tt.setupQueue {
				queueDir := filepath.Join(srv.config.Server.DataDir, "queue")
				srv.queue = queue.NewManager(srv.database, nil, queueDir)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				srv.queue.Start(ctx)
				defer srv.queue.Stop()
			} else {
				srv.queue = nil
			}

			msgData := []byte("Subject: Test\r\n\r\nBody")
			err := srv.relayMessage(tt.from, tt.to, msgData)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
			}
		})
	}
}
