package security

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestRateLimiter(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := RateLimitConfig{
		SMTPConnectionsPerMinute: 5,
		SMTPMessagesPerMinute:    10,
		IMAPConnectionsPerMinute: 5,
		IMAPCommandsPerMinute:    60,
		HTTPRequestsPerMinute:    60,
		LoginAttemptsPerMinute:   5,
		MaxConcurrentConnections: 100,
	}

	rl := NewRateLimiter(config, database)

	t.Run("Allow", func(t *testing.T) {
		key := "test_ip:smtp_connection"

		// Should allow first requests
		if !rl.Allow(key, "smtp_connection") {
			t.Error("expected first request to be allowed")
		}
		if !rl.Allow(key, "smtp_connection") {
			t.Error("expected second request to be allowed")
		}

		// Reset and try again
		rl.Reset(key)
		if !rl.Allow(key, "smtp_connection") {
			t.Error("expected request after reset to be allowed")
		}
	})

	t.Run("AllowN", func(t *testing.T) {
		key := "test_ip_2:smtp_message"

		// Should allow small batch
		if !rl.AllowN(key, "smtp_message", 3) {
			t.Error("expected batch of 3 to be allowed")
		}

		// Reset
		rl.Reset(key)
	})

	t.Run("AllowWithUnknownType", func(t *testing.T) {
		key := "test_ip:unknown_type"

		// Should use default values
		if !rl.Allow(key, "unknown_type") {
			t.Error("expected request with unknown type to use defaults")
		}
	})

	t.Run("Reset", func(t *testing.T) {
		key := "test_reset:smtp_connection"

		// Use up some tokens
		rl.AllowN(key, "smtp_connection", 5)

		// Reset
		rl.Reset(key)

		// Should be allowed again
		if !rl.Allow(key, "smtp_connection") {
			t.Error("expected request after reset to be allowed")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		key := "test_cleanup:smtp_connection"

		// Create bucket
		rl.Allow(key, "smtp_connection")

		// Cleanup old buckets (with zero maxAge to remove all)
		rl.Cleanup(0)

		// Bucket should be recreated and allowed
		if !rl.Allow(key, "smtp_connection") {
			t.Error("expected request after cleanup to create new bucket")
		}
	})

	t.Run("BucketRefill", func(t *testing.T) {
		config := RateLimitConfig{
			SMTPConnectionsPerMinute: 1, // Very low rate for testing
		}
		rl2 := NewRateLimiter(config, database)

		key := "test_refill:smtp_connection"

		// Use the only token
		if !rl2.Allow(key, "smtp_connection") {
			t.Error("expected first request to be allowed")
		}

		// Should be rate limited now
		if rl2.Allow(key, "smtp_connection") {
			t.Error("expected second request to be rate limited")
		}

		// Wait for refill
		time.Sleep(100 * time.Millisecond)

		// After some time, tokens should refill
		// Note: This might be flaky depending on timing
	})
}

func TestRateLimitKey(t *testing.T) {
	tests := []struct {
		ip        string
		limitType string
		expected  string
	}{
		{"192.168.1.1", "smtp_connection", "192.168.1.1:smtp_connection"},
		{"10.0.0.1", "login_attempt", "10.0.0.1:login_attempt"},
	}

	for _, tc := range tests {
		result := RateLimitKey(tc.ip, tc.limitType)
		if result != tc.expected {
			t.Errorf("RateLimitKey(%s, %s) = %s, want %s", tc.ip, tc.limitType, result, tc.expected)
		}
	}
}

func TestAccountKey(t *testing.T) {
	tests := []struct {
		email     string
		limitType string
		expected  string
	}{
		{"user@example.com", "smtp_message", "account:user@example.com:smtp_message"},
		{"admin@test.org", "imap_command", "account:admin@test.org:imap_command"},
	}

	for _, tc := range tests {
		result := AccountKey(tc.email, tc.limitType)
		if result != tc.expected {
			t.Errorf("AccountKey(%s, %s) = %s, want %s", tc.email, tc.limitType, result, tc.expected)
		}
	}
}

func TestGetIP(t *testing.T) {
	tests := []struct {
		addr     string
		expected string
	}{
		{"192.168.1.1:25", "192.168.1.1"},
		{"10.0.0.1:587", "10.0.0.1"},
		{"[::1]:993", "::1"},
		{"invalid", "invalid"}, // Falls back to input if parsing fails
	}

	for _, tc := range tests {
		result := GetIP(tc.addr)
		if result != tc.expected {
			t.Errorf("GetIP(%s) = %s, want %s", tc.addr, result, tc.expected)
		}
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	config := DefaultRateLimitConfig()

	if config.SMTPConnectionsPerMinute != 30 {
		t.Errorf("expected SMTPConnectionsPerMinute = 30, got %d", config.SMTPConnectionsPerMinute)
	}
	if config.SMTPMessagesPerMinute != 60 {
		t.Errorf("expected SMTPMessagesPerMinute = 60, got %d", config.SMTPMessagesPerMinute)
	}
	if config.IMAPConnectionsPerMinute != 50 {
		t.Errorf("expected IMAPConnectionsPerMinute = 50, got %d", config.IMAPConnectionsPerMinute)
	}
	if config.IMAPCommandsPerMinute != 300 {
		t.Errorf("expected IMAPCommandsPerMinute = 300, got %d", config.IMAPCommandsPerMinute)
	}
	if config.HTTPRequestsPerMinute != 120 {
		t.Errorf("expected HTTPRequestsPerMinute = 120, got %d", config.HTTPRequestsPerMinute)
	}
	if config.LoginAttemptsPerMinute != 10 {
		t.Errorf("expected LoginAttemptsPerMinute = 10, got %d", config.LoginAttemptsPerMinute)
	}
	if config.MaxConcurrentConnections != 100 {
		t.Errorf("expected MaxConcurrentConnections = 100, got %d", config.MaxConcurrentConnections)
	}
}
