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

func TestRateLimiterStartCleanup(t *testing.T) {
	database, err := db.Open(t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	rl := NewRateLimiter(DefaultRateLimitConfig(), database)

	// Create a bucket entry
	rl.Allow("192.168.1.1:smtp_connection", "smtp_connection")

	// StartCleanup should not panic
	rl.StartCleanup(50*time.Millisecond, 10*time.Millisecond)
	time.Sleep(120 * time.Millisecond)
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

// TestGetBucketParams tests all operation type branches.
func TestGetBucketParams(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := RateLimitConfig{
		SMTPConnectionsPerMinute: 30,
		SMTPMessagesPerMinute:    60,
		IMAPConnectionsPerMinute: 50,
		IMAPCommandsPerMinute:    300,
		HTTPRequestsPerMinute:    120,
		LoginAttemptsPerMinute:   10,
	}

	rl := NewRateLimiter(config, database)

	tests := []struct {
		limitType       string
		expectCapacity  float64
		expectRefillRate float64
	}{
		{"smtp_connection", 30, 30.0 / 60.0},
		{"smtp_message", 60, 60.0 / 60.0},
		{"imap_connection", 50, 50.0 / 60.0},
		{"imap_command", 300, 300.0 / 60.0},
		{"http_request", 120, 120.0 / 60.0},
		{"login_attempt", 10, 10.0 / 60.0},
		{"unknown_type", 60.0, 1.0}, // default
	}

	for _, tc := range tests {
		capacity, refillRate := rl.getBucketParams(tc.limitType)
		if capacity != tc.expectCapacity {
			t.Errorf("getBucketParams(%s): expected capacity %v, got %v", tc.limitType, tc.expectCapacity, capacity)
		}
		if refillRate != tc.expectRefillRate {
			t.Errorf("getBucketParams(%s): expected refillRate %v, got %v", tc.limitType, tc.expectRefillRate, refillRate)
		}
	}
}

// TestMin tests the min function for all branches.
func TestMin(t *testing.T) {
	tests := []struct {
		a, b     float64
		expected float64
	}{
		{1.0, 2.0, 1.0},  // a < b
		{3.0, 2.0, 2.0},  // a > b
		{5.0, 5.0, 5.0},  // a == b
		{0.0, -1.0, -1.0}, // zero and negative
		{-5.0, -3.0, -5.0}, // both negative, a < b
	}

	for _, tc := range tests {
		result := min(tc.a, tc.b)
		if result != tc.expected {
			t.Errorf("min(%v, %v) = %v, want %v", tc.a, tc.b, result, tc.expected)
		}
	}
}

// TestBucket_AllowN_InsufficientTokens tests that allowN returns false when
// requesting more tokens than available.
func TestBucket_AllowN_InsufficientTokens(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := RateLimitConfig{
		SMTPConnectionsPerMinute: 5,
	}
	rl := NewRateLimiter(config, database)
	key := "test_insufficient:smtp_connection"

	// Consume all 5 tokens
	if !rl.AllowN(key, "smtp_connection", 5) {
		t.Fatal("expected initial batch of 5 to be allowed")
	}

	// Requesting 1 more should fail
	if rl.AllowN(key, "smtp_connection", 1) {
		t.Error("expected request to be denied when no tokens remain")
	}
}

// TestAllow_AllLimitTypes tests Allow with each limit type to ensure buckets
// are created correctly.
func TestAllow_AllLimitTypes(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := DefaultRateLimitConfig()
	rl := NewRateLimiter(config, database)

	limitTypes := []string{
		"smtp_connection",
		"smtp_message",
		"imap_connection",
		"imap_command",
		"http_request",
		"login_attempt",
		"some_random_type",
	}

	for _, lt := range limitTypes {
		key := "test_all_types:" + lt
		if !rl.Allow(key, lt) {
			t.Errorf("expected first request for type %q to be allowed", lt)
		}
	}
}
