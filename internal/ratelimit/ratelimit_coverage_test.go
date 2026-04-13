package ratelimit

import (
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestRateLimiter_WithBoltDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ratelimit_test.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := DefaultConfig()
	rl := New(db, cfg)

	// Test CheckUser with persistence
	for i := 0; i < 3; i++ {
		result := rl.CheckUser("testuser")
		if !result.Allowed {
			t.Errorf("CheckUser iteration %d: expected allowed", i)
		}
	}

	// Verify persisted data
	count := rl.loadUserSentToday("testuser")
	t.Logf("loadUserSentToday count: %d", count)

	// Save some sent today count
	rl.saveUserSentToday("testuser", 100)
	count = rl.loadUserSentToday("testuser")
	if count != 100 {
		t.Errorf("Expected 100, got %d", count)
	}

	// Reset
	rl.saveUserSentToday("testuser", 0)
	count = rl.loadUserSentToday("testuser")
	if count != 0 {
		t.Errorf("Expected 0 after reset, got %d", count)
	}
}

func TestRateLimiter_CheckUser_WithDailyQuota(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ratelimit_daily.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := &Config{
		UserPerMinute: 1000,
		UserPerHour:   1000,
		UserPerDay:    5, // Very low limit for testing
	}
	rl := New(db, cfg)

	user := "dailyuser"

	// Use up daily quota
	for i := 0; i < 5; i++ {
		result := rl.CheckUser(user)
		if !result.Allowed {
			t.Errorf("CheckUser iteration %d: expected allowed", i)
		}
	}

	// Next should be rejected due to daily limit
	result := rl.CheckUser(user)
	if result.Allowed {
		t.Errorf("Expected rejected due to daily limit")
	}
	if result.Reason == "" {
		t.Error("Expected reason to be set")
	}
}

func TestRateLimiter_CheckUser_WithNilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg) // nil bolt DB

	// Should work without panic
	result := rl.CheckUser("testuser")
	if !result.Allowed {
		t.Errorf("Expected allowed with nil bolt, got rejected: %s", result.Reason)
	}
}

func TestRateLimiter_CheckIP_WithNilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg) // nil bolt DB

	// Should work without panic
	result := rl.CheckIP("192.168.1.1")
	if !result.Allowed {
		t.Errorf("Expected allowed with nil bolt, got rejected: %s", result.Reason)
	}
}

func TestRateLimiter_SaveAndLoadUserSentToday(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ratelimit_increment.db")

	db, err := bbolt.Open(dbPath, 0o600, nil)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	cfg := DefaultConfig()
	rl := New(db, cfg)

	// Directly test user quota persistence
	rl.saveUserSentToday("user1", 50)
	rl.saveUserSentToday("user1", 75) // Should overwrite

	count := rl.loadUserSentToday("user1")
	if count != 75 {
		t.Errorf("Expected 75, got %d", count)
	}
}

func TestRateLimiter_LoadUserSentToday_NilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	count := rl.loadUserSentToday("anyuser")
	if count != 0 {
		t.Errorf("Expected 0 for nil bolt, got %d", count)
	}
}

func TestRateLimiter_SaveUserSentToday_NilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	// Should not panic with nil bolt
	rl.saveUserSentToday("anyuser", 100)
}

func TestRateLimiter_CheckRecipients(t *testing.T) {
	cfg := &Config{
		UserMaxRecipients: 10,
	}
	rl := New(nil, cfg)

	result := rl.CheckRecipients("user", 5)
	if !result.Allowed {
		t.Errorf("Expected allowed for 5 recipients (limit 10)")
	}

	result = rl.CheckRecipients("user", 15)
	if result.Allowed {
		t.Errorf("Expected rejected for 15 recipients (limit 10)")
	}
}

func TestRateLimiter_CheckRecipients_NoLimit(t *testing.T) {
	cfg := &Config{
		UserMaxRecipients: 0, // No limit
	}
	rl := New(nil, cfg)

	result := rl.CheckRecipients("user", 1000)
	if !result.Allowed {
		t.Errorf("Expected allowed when no limit set")
	}
}

func TestRateLimiter_CheckConnection(t *testing.T) {
	cfg := &Config{
		IPConnections: 2,
	}
	rl := New(nil, cfg)

	ip := "10.0.5.1"

	// Use connections
	result := rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("First CheckConnection should be allowed")
	}

	result = rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("Second CheckConnection should be allowed")
	}

	// Third should be rejected
	result = rl.CheckConnection(ip)
	if result.Allowed {
		t.Errorf("Third CheckConnection should be rejected")
	}
}

func TestRateLimiter_ReleaseConnection(t *testing.T) {
	cfg := &Config{
		IPConnections: 1,
	}
	rl := New(nil, cfg)

	ip := "10.0.6.1"

	// Use connection
	result := rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("First should be allowed")
	}

	// Release
	rl.ReleaseConnection(ip)

	// Should be allowed again
	result = rl.CheckConnection(ip)
	if !result.Allowed {
		t.Errorf("Should be allowed after release")
	}
}

func TestRateLimiter_ReleaseConnection_Unknown(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	// Should not panic for unknown IP
	rl.ReleaseConnection("192.168.255.255")
}

func TestRateLimiter_GetIPStats_NilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	stats := rl.GetIPStats("192.168.1.1")
	if stats == nil {
		t.Error("Expected non-nil stats")
	}
}

func TestRateLimiter_GetUserStats_NilBolt(t *testing.T) {
	cfg := DefaultConfig()
	rl := New(nil, cfg)

	stats := rl.GetUserStats("testuser")
	if stats == nil {
		t.Error("Expected non-nil stats")
	}
}

func TestRateLimiter_SetConfig(t *testing.T) {
	rl := New(nil, nil)

	cfg := &Config{
		IPPerMinute:   50,
		UserPerMinute: 100,
	}
	rl.SetConfig(cfg)

	got := rl.GetConfig()
	if got.IPPerMinute != 50 {
		t.Errorf("Expected IPPerMinute=50, got %d", got.IPPerMinute)
	}
	if got.UserPerMinute != 100 {
		t.Errorf("Expected UserPerMinute=100, got %d", got.UserPerMinute)
	}
}

func TestRateLimiter_SetConfig_Nil(t *testing.T) {
	rl := New(nil, nil)

	// Set nil config - should use defaults
	rl.SetConfig(nil)

	cfg := rl.GetConfig()
	if cfg.IPPerMinute != 30 {
		t.Errorf("Expected default IPPerMinute=30, got %d", cfg.IPPerMinute)
	}
}

func TestRateLimiter_cleanupLoop(t *testing.T) {
	cfg := &Config{
		CleanupInterval: time.Hour,
		IPPerDay:        10,
	}
	rl := New(nil, cfg)

	// Add some IPs
	for i := 0; i < 5; i++ {
		rl.CheckIP("10.1.1." + string(rune('0'+i)))
	}

	// Run cleanup
	rl.cleanup()

	// Stats should still work
	stats := rl.GetIPStats("10.1.1.0")
	if stats == nil {
		t.Error("Expected non-nil stats after cleanup")
	}
}
