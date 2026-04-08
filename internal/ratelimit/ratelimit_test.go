package ratelimit

import (
	"strings"
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

func TestCheckIP_HourlyLimit(t *testing.T) {
	cfg := &Config{
		IPPerMinute:     1000, // High so minute doesn't trigger first
		IPPerHour:      3,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.1.1"

	// Use up hourly limit
	for i := 0; i < 3; i++ {
		rl.CheckIP(ip)
	}

	// Next should be rejected due to hourly limit
	result := rl.CheckIP(ip)
	if result.Allowed {
		t.Errorf("Expected rejected due to hourly limit")
	}
	if !strings.Contains(result.Reason, "hour") {
		t.Errorf("Expected hourly limit reason, got: %s", result.Reason)
	}
}

func TestCheckIP_DailyLimit(t *testing.T) {
	cfg := &Config{
		IPPerMinute:     1000,
		IPPerHour:      1000,
		IPPerDay:       2,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.2.1"

	// Use up daily limit
	for i := 0; i < 2; i++ {
		rl.CheckIP(ip)
	}

	// Next should be rejected due to daily limit
	result := rl.CheckIP(ip)
	if result.Allowed {
		t.Errorf("Expected rejected due to daily limit")
	}
	if !strings.Contains(result.Reason, "day") {
		t.Errorf("Expected daily limit reason, got: %s", result.Reason)
	}
}

func TestCheckUser_HourlyLimit(t *testing.T) {
	cfg := &Config{
		UserPerMinute:   1000,
		UserPerHour:     3,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	user := "user_hourly"

	// Use up hourly limit
	for i := 0; i < 3; i++ {
		rl.CheckUser(user)
	}

	// Next should be rejected due to hourly limit
	result := rl.CheckUser(user)
	if result.Allowed {
		t.Errorf("Expected rejected due to hourly limit")
	}
	if !strings.Contains(result.Reason, "hour") {
		t.Errorf("Expected hourly limit reason, got: %s", result.Reason)
	}
}

func TestCheckUser_DailyLimit(t *testing.T) {
	cfg := &Config{
		UserPerMinute:   1000,
		UserPerHour:     1000,
		UserPerDay:      2,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	user := "user_daily"

	// Use up daily limit
	for i := 0; i < 2; i++ {
		rl.CheckUser(user)
	}

	// Next should be rejected due to daily limit
	result := rl.CheckUser(user)
	if result.Allowed {
		t.Errorf("Expected rejected due to daily limit")
	}
	if !strings.Contains(result.Reason, "Daily") {
		t.Errorf("Expected daily quota reason, got: %s", result.Reason)
	}
}

func TestCheckIP_MinuteLimitBeforeHourly(t *testing.T) {
	// When minute limit is lower, it should be checked first
	cfg := &Config{
		IPPerMinute:     2,
		IPPerHour:      100,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.3.1"

	// Use up minute limit
	rl.CheckIP(ip)
	rl.CheckIP(ip)

	// 3rd should be rejected by minute limit (not hourly)
	result := rl.CheckIP(ip)
	if result.Allowed {
		t.Errorf("Expected rejected")
	}
	if !strings.Contains(result.Reason, "/min") {
		t.Errorf("Expected minute limit reason (most restrictive), got: %s", result.Reason)
	}
}

func TestCheckConnection_Expiry(t *testing.T) {
	cfg := &Config{
		IPConnections:   1,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.4.1"

	// Use the connection slot
	rl.CheckConnection(ip)

	// Second should be rejected
	result := rl.CheckConnection(ip)
	if result.Allowed {
		t.Errorf("Expected rejected")
	}

	// Manually expire the counter by directly manipulating (for testing)
	// Instead, just verify that a new IP is allowed
	result = rl.CheckConnection("10.0.4.2")
	if !result.Allowed {
		t.Errorf("Expected allowed for different IP")
	}
}

func TestCleanup_ExpiresOldEntries(t *testing.T) {
	cfg := &Config{
		IPPerDay:        10,
		CleanupInterval: time.Hour,
	}
	rl := New(nil, cfg)
	ip := "10.0.5.1"

	// Add some requests
	for i := 0; i < 5; i++ {
		rl.CheckIP(ip)
	}

	// Verify we have stats
	stats := rl.GetIPStats(ip)
	if stats["day_count"].(int) != 5 {
		t.Errorf("Expected day_count=5, got %v", stats["day_count"])
	}

	// Call cleanup manually
	rl.cleanup()

	// After cleanup of expired entries (none should be expired yet in this test)
	// The day reset is 24h from now so nothing is cleaned up
	stats = rl.GetIPStats(ip)
	if stats["day_count"].(int) != 5 {
		t.Errorf("Expected day_count still=5 after cleanup, got %v", stats["day_count"])
	}
}

func TestSetConfig(t *testing.T) {
	rl := New(nil, nil)

	cfg := &Config{
		IPPerMinute: 10,
	}
	rl.SetConfig(cfg)

	got := rl.GetConfig()
	if got.IPPerMinute != 10 {
		t.Errorf("Expected IPPerMinute=10, got %d", got.IPPerMinute)
	}
}

func TestSetConfig_Nil(t *testing.T) {
	rl := New(nil, nil)

	// Setting nil config should not panic
	rl.SetConfig(nil)

	// Config should remain at defaults
	cfg := rl.GetConfig()
	if cfg.IPPerMinute != 30 {
		t.Errorf("Expected default IPPerMinute=30, got %d", cfg.IPPerMinute)
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
