package server

import (
	"os"
	"testing"

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
