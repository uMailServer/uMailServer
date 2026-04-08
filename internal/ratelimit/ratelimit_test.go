package ratelimit

import (
	"testing"
	"time"
)

func TestCheckIP_UnderLimit(t *testing.T) {
	rl := New(nil, nil) // in-memory with defaults
	ip := "192.168.1.1"

	result := rl.CheckIP(ip)
	if !result.Allowed {
		t.Errorf("Expected allowed, got rejected: %s", result.Reason)
	}
}

func TestCheckIP_OverLimit(t *testing.T) {
	cfg := &Config{
		IPPerMinute:     5,
		CleanupInterval: time.Hour, // Don't cleanup during test
	}
	rl := New(nil, cfg)
	ip := "192.168.1.2"

	// Use up the limit
	for i := 0; i < 5; i++ {
		rl.CheckIP(ip)
	}

	// Next one should be rejected
	result := rl.CheckIP(ip)
	if result.Allowed {
		t.Errorf("Expected rejected, got allowed")
	}
	if result.Reason == "" {
		t.Errorf("Expected reason to be set")
	}
}

func TestCheckUser_UnderLimit(t *testing.T) {
	rl := New(nil, nil)
	user := "testuser"

	result := rl.CheckUser(user)
	if !result.Allowed {
		t.Errorf("Expected allowed, got rejected: %s", result.Reason)
	}
}

func TestCheckUser_OverLimit(t *testing.T) {
	cfg := &Config{
		UserPerMinute:   3,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	user := "testuser"

	// Use up the limit
	for i := 0; i < 3; i++ {
		rl.CheckUser(user)
	}

	// Next one should be rejected
	result := rl.CheckUser(user)
	if result.Allowed {
		t.Errorf("Expected rejected, got allowed")
	}
}

func TestCheckRecipients_TooMany(t *testing.T) {
	cfg := &Config{
		UserMaxRecipients: 5,
	}
	rl := New(nil, cfg)

	result := rl.CheckRecipients("user", 10)
	if result.Allowed {
		t.Errorf("Expected rejected for too many recipients")
	}
}

func TestCheckRecipients_OK(t *testing.T) {
	cfg := &Config{
		UserMaxRecipients: 10,
	}
	rl := New(nil, cfg)

	result := rl.CheckRecipients("user", 5)
	if !result.Allowed {
		t.Errorf("Expected allowed for 5 recipients (limit 10)")
	}
}

func TestCheckConnection_OK(t *testing.T) {
	rl := New(nil, nil)
	ip := "10.0.0.1"

	result := rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("Expected allowed, got rejected: %s", result.Reason)
	}
}

func TestCheckConnection_TooMany(t *testing.T) {
	cfg := &Config{
		IPConnections:   3,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.0.2"

	// Use up connections
	for i := 0; i < 3; i++ {
		rl.CheckConnection(ip)
	}

	// Next should be rejected
	result := rl.CheckConnection(ip)
	if result.Allowed {
		t.Errorf("Expected rejected when too many connections")
	}
}

func TestReleaseConnection(t *testing.T) {
	cfg := &Config{
		IPConnections:   2,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.0.3"

	// Use up connections
	rl.CheckConnection(ip)
	rl.CheckConnection(ip)

	// Should be rejected
	result := rl.CheckConnection(ip)
	if result.Allowed {
		t.Errorf("Expected rejected")
	}

	// Release one
	rl.ReleaseConnection(ip)

	// Should be allowed now
	result = rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("Expected allowed after release")
	}
}

func TestGetIPStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = time.Hour // Avoid cleanup during test
	rl := New(nil, cfg)
	ip := "10.0.0.5"

	// Generate some traffic
	for i := 0; i < 5; i++ {
		rl.CheckIP(ip)
	}

	stats := rl.GetIPStats(ip)
	if stats["minute_count"] != 5 {
		t.Errorf("Expected minute_count=5, got %v", stats["minute_count"])
	}
}

func TestGetUserStats(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = time.Hour
	rl := New(nil, cfg)
	user := "testuser"

	// Generate some traffic
	for i := 0; i < 3; i++ {
		rl.CheckUser(user)
	}

	stats := rl.GetUserStats(user)
	if stats["minute_count"] != 3 {
		t.Errorf("Expected minute_count=3, got %v", stats["minute_count"])
	}
	if stats["daily_limit"] != 5000 {
		t.Errorf("Expected daily_limit=5000, got %v", stats["daily_limit"])
	}
}

func TestResult_RetryAfter(t *testing.T) {
	cfg := &Config{
		IPPerMinute:     1,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.0.6"

	// Use up the single allowed request
	rl.CheckIP(ip)

	// Should be rejected with retry info
	result := rl.CheckIP(ip)
	if result.Allowed {
		t.Errorf("Expected rejected")
	}
	if result.RetryAfter <= 0 {
		t.Errorf("Expected positive RetryAfter")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.IPPerMinute != 30 {
		t.Errorf("Expected IPPerMinute=30, got %d", cfg.IPPerMinute)
	}
	if cfg.UserPerDay != 5000 {
		t.Errorf("Expected UserPerDay=5000, got %d", cfg.UserPerDay)
	}
	if cfg.CleanupInterval != 5*time.Minute {
		t.Errorf("Expected CleanupInterval=5m, got %v", cfg.CleanupInterval)
	}
}
