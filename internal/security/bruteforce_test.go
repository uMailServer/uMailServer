package security

import (
	"fmt"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestBruteForceProtector(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}
	config := BruteForceConfig{
		MaxAttempts:       3,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}

	bfp := NewBruteForceProtector(config, database, blocklist)

	t.Run("RecordSuccessfulAttempt", func(t *testing.T) {
		key := "test_success"

		locked, remaining := bfp.RecordAttempt(key, true)
		if locked {
			t.Error("expected successful attempt to not be locked")
		}
		if remaining != config.MaxAttempts {
			t.Errorf("expected remaining attempts = %d, got %d", config.MaxAttempts, remaining)
		}
	})

	t.Run("RecordFailedAttempts", func(t *testing.T) {
		key := "test_failed"

		// First two attempts should not lock
		for i := 0; i < 2; i++ {
			locked, remaining := bfp.RecordAttempt(key, false)
			if locked {
				t.Errorf("attempt %d should not be locked", i+1)
			}
			expectedRemaining := config.MaxAttempts - i - 1
			if remaining != expectedRemaining {
				t.Errorf("attempt %d: expected remaining = %d, got %d", i+1, expectedRemaining, remaining)
			}
		}
	})

	t.Run("LockoutAfterMaxAttempts", func(t *testing.T) {
		key := "test_lockout"

		// Record max attempts
		for i := 0; i < config.MaxAttempts; i++ {
			bfp.RecordAttempt(key, false)
		}

		// Next attempt should be locked
		locked, remaining := bfp.RecordAttempt(key, false)
		if !locked {
			t.Error("expected account to be locked after max attempts")
		}
		if remaining != 0 {
			t.Errorf("expected remaining = 0 when locked, got %d", remaining)
		}

		// Should be locked
		if !bfp.IsLocked(key) {
			t.Error("expected IsLocked to return true")
		}
	})

	t.Run("Reset", func(t *testing.T) {
		key := "test_reset"

		// Record some attempts
		for i := 0; i < 2; i++ {
			bfp.RecordAttempt(key, false)
		}

		// Reset
		bfp.Reset(key)

		// Should be unlocked
		if bfp.IsLocked(key) {
			t.Error("expected account to be unlocked after reset")
		}

		// Should be able to attempt again
		locked, _ := bfp.RecordAttempt(key, false)
		if locked {
			t.Error("expected to be able to attempt after reset")
		}
	})

	t.Run("GetLockoutTime", func(t *testing.T) {
		key := "test_lockout_time"

		// Should be nil initially
		if bfp.GetLockoutTime(key) != nil {
			t.Error("expected nil lockout time initially")
		}

		// Lock the account
		for i := 0; i < config.MaxAttempts; i++ {
			bfp.RecordAttempt(key, false)
		}

		// Should have a lockout time
		lockoutTime := bfp.GetLockoutTime(key)
		if lockoutTime == nil {
			t.Error("expected non-nil lockout time after locking")
		}
	})

	t.Run("IsLockedNotExists", func(t *testing.T) {
		key := "test_not_exists"

		if bfp.IsLocked(key) {
			t.Error("expected non-existent key to not be locked")
		}
	})

	t.Run("Cleanup", func(t *testing.T) {
		key := "test_cleanup"

		// Record attempts
		bfp.RecordAttempt(key, false)

		// Cleanup with 0 duration should remove all
		bfp.Cleanup(0)

		// Key should be gone
		if bfp.IsLocked(key) {
			t.Error("expected key to be removed after cleanup")
		}
	})

	t.Run("GetStats", func(t *testing.T) {
		// Create unique keys for this test to avoid interference from other tests
		baseKey := fmt.Sprintf("stats_test_%d", time.Now().UnixNano())

		// Create some tracked keys
		for i := 0; i < 3; i++ {
			bfp.RecordAttempt(fmt.Sprintf("%s_%d", baseKey, i), false)
		}

		// Lock one (this will create an additional key)
		lockedKey := fmt.Sprintf("%s_locked", baseKey)
		for i := 0; i < config.MaxAttempts; i++ {
			bfp.RecordAttempt(lockedKey, false)
		}

		stats := bfp.GetStats()

		// We should have at least 4 tracked keys: 3 from loop + 1 locked
		// (may be more from other tests, so we check >= 4)
		trackedKeys := stats["tracked_keys"].(int)
		currentlyLocked := stats["currently_locked"].(int)
		if trackedKeys < 4 {
			t.Errorf("expected at least 4 tracked keys, got %d", trackedKeys)
		}
		if currentlyLocked < 1 {
			t.Errorf("expected at least 1 locked, got %d", currentlyLocked)
		}
	})
}

func TestBruteForceStartCleanup(t *testing.T) {
	database, err := db.Open(t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := DefaultBruteForceConfig()
	bfp := NewBruteForceProtector(config, database, blocklist)

	// Record some attempts
	bfp.RecordAttempt("192.168.1.1", false)

	// StartCleanup should not panic
	bfp.StartCleanup(50*time.Millisecond, 10*time.Millisecond)
	time.Sleep(120 * time.Millisecond)
}

func TestDefaultBruteForceConfig(t *testing.T) {
	config := DefaultBruteForceConfig()

	if config.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts = 5, got %d", config.MaxAttempts)
	}
	if config.LockoutDuration != 15*time.Minute {
		t.Errorf("expected LockoutDuration = 15m, got %s", config.LockoutDuration)
	}
	if config.AttemptWindow != 30*time.Minute {
		t.Errorf("expected AttemptWindow = 30m, got %s", config.AttemptWindow)
	}
	if config.AutoBlockDuration != 1*time.Hour {
		t.Errorf("expected AutoBlockDuration = 1h, got %s", config.AutoBlockDuration)
	}
}

// TestRecordAttempt_SuccessOnExistingKey tests that a successful attempt clears
// the tracking for an existing key.
func TestRecordAttempt_SuccessOnExistingKey(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       3,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_success_existing"

	// Record a failed attempt first
	locked, remaining := bfp.RecordAttempt(key, false)
	if locked {
		t.Error("expected first failed attempt to not be locked")
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining attempts, got %d", remaining)
	}

	// Now record a successful attempt -- should clear the tracking
	locked, remaining = bfp.RecordAttempt(key, true)
	if locked {
		t.Error("expected successful attempt to not be locked")
	}
	if remaining != config.MaxAttempts {
		t.Errorf("expected remaining = %d after success, got %d", config.MaxAttempts, remaining)
	}

	// Key should be gone from attempts map (reset to full attempts)
	locked, remaining = bfp.RecordAttempt(key, false)
	if locked {
		t.Error("expected new failed attempt after success to not be locked")
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining after fresh failed attempt, got %d", remaining)
	}
}

// TestRecordAttempt_SuccessOnNewKey tests that a successful attempt on a new
// (non-existing) key returns unlocked with full attempts remaining.
func TestRecordAttempt_SuccessOnNewKey(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       5,
		LockoutDuration:   15 * time.Minute,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_success_new_key"

	// Success on a brand-new key (not in map yet)
	locked, remaining := bfp.RecordAttempt(key, true)
	if locked {
		t.Error("expected successful attempt on new key to not be locked")
	}
	if remaining != config.MaxAttempts {
		t.Errorf("expected remaining = %d for success on new key, got %d", config.MaxAttempts, remaining)
	}
}

// TestRecordAttempt_LockedKeyReturnsLocked tests that once a key is locked,
// subsequent attempts return locked immediately.
func TestRecordAttempt_LockedKeyReturnsLocked(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       2,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_locked_returns_locked"

	// Record max attempts to trigger lockout
	bfp.RecordAttempt(key, false)
	bfp.RecordAttempt(key, false)

	// Now key should be locked; subsequent attempts return locked
	locked, remaining := bfp.RecordAttempt(key, false)
	if !locked {
		t.Error("expected key to be locked after max attempts")
	}
	if remaining != 0 {
		t.Errorf("expected 0 remaining when locked, got %d", remaining)
	}

	// Verify IsLocked also returns true
	if !bfp.IsLocked(key) {
		t.Error("expected IsLocked to return true for locked key")
	}
}

// TestRecordAttempt_WindowReset tests that attempts are reset when outside the
// attempt window.
func TestRecordAttempt_WindowReset(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       3,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     100 * time.Millisecond, // Very short window
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_window_reset"

	// Record 2 failed attempts
	bfp.RecordAttempt(key, false)
	bfp.RecordAttempt(key, false)

	// Wait for the attempt window to expire
	time.Sleep(200 * time.Millisecond)

	// Record another failed attempt -- window should have reset, count starts fresh
	locked, remaining := bfp.RecordAttempt(key, false)
	if locked {
		t.Error("expected not locked after window reset")
	}
	if remaining != 2 {
		t.Errorf("expected 2 remaining after window reset (count=1), got %d", remaining)
	}
}

// TestRecordAttempt_AutoBlockIP tests that reaching max attempts auto-blocks
// the IP in the blocklist.
func TestRecordAttempt_AutoBlockIP(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       2,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "10.99.99.99"

	// Record max attempts to trigger auto-block
	bfp.RecordAttempt(key, false)
	bfp.RecordAttempt(key, false)

	// The key should be auto-blocked in the blocklist
	if !blocklist.IsBlocked(key) {
		t.Error("expected key to be auto-blocked in blocklist after max attempts")
	}
}

// TestRecordAttempt_NilBlocklist tests that RecordAttempt works correctly when
// blocklist is nil (no auto-blocking).
func TestRecordAttempt_NilBlocklist(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := BruteForceConfig{
		MaxAttempts:       2,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, nil)

	key := "test_nil_blocklist"

	// Record max attempts with nil blocklist -- should not panic
	bfp.RecordAttempt(key, false)
	locked, remaining := bfp.RecordAttempt(key, false)
	if !locked {
		t.Error("expected key to be locked even with nil blocklist")
	}
	if remaining != 0 {
		t.Errorf("expected 0 remaining, got %d", remaining)
	}
}

// TestCleanup_ExpiredLockouts tests that Cleanup removes entries whose lockout
// has expired and sufficient time (5 minutes) has passed.
func TestCleanup_ExpiredLockouts(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       2,
		LockoutDuration:   1 * time.Hour,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_cleanup_expired_lockout"

	// Record max attempts to trigger lockout
	bfp.RecordAttempt(key, false)
	bfp.RecordAttempt(key, false)

	if !bfp.IsLocked(key) {
		t.Error("expected key to be locked initially")
	}

	// Manipulate the internal state: set lockedUntil to 10 minutes ago
	// (well past the 5-minute threshold in Cleanup)
	bfp.mu.Lock()
	if attempts, ok := bfp.attempts[key]; ok {
		past := time.Now().Add(-10 * time.Minute)
		attempts.lockedUntil = &past
		// Keep lastSeen recent so the first Cleanup condition (lastSeen > maxAge) does not trigger
		attempts.lastSeen = time.Now()
	}
	bfp.mu.Unlock()

	// Cleanup with large maxAge so only the lockout-expiry path triggers
	bfp.Cleanup(1 * time.Hour)

	// Entry should be removed because lockout expired > 5 minutes ago
	if bfp.IsLocked(key) {
		t.Error("expected entry with long-expired lockout to be cleaned up")
	}
}

// TestCleanup_LastSeenExpired tests that Cleanup removes entries whose lastSeen
// is older than maxAge.
func TestCleanup_LastSeenExpired(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	blocklist, err := NewBlocklist(database.BoltDB())
	if err != nil {
		t.Fatalf("failed to create blocklist: %v", err)
	}

	config := BruteForceConfig{
		MaxAttempts:       5,
		LockoutDuration:   15 * time.Minute,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
	bfp := NewBruteForceProtector(config, database, blocklist)

	key := "test_cleanup_old_lastseen"

	// Record an attempt
	bfp.RecordAttempt(key, false)

	// Manually set lastSeen to the distant past
	bfp.mu.Lock()
	if attempts, ok := bfp.attempts[key]; ok {
		attempts.lastSeen = time.Now().Add(-2 * time.Hour)
	}
	bfp.mu.Unlock()

	// Cleanup with 1 hour maxAge should remove the entry
	bfp.Cleanup(1 * time.Hour)

	if bfp.IsLocked(key) {
		t.Error("expected old entry to be cleaned up")
	}

	// Verify the entry is completely gone by checking remaining attempts
	locked, remaining := bfp.RecordAttempt(key, false)
	if locked {
		t.Error("expected fresh attempt on cleaned-up key to not be locked")
	}
	if remaining != config.MaxAttempts-1 {
		t.Errorf("expected %d remaining on fresh key, got %d", config.MaxAttempts-1, remaining)
	}
}

// TestGetLockoutTime_NotExists tests GetLockoutTime for a non-existent key.
func TestGetLockoutTime_NotExists(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	config := DefaultBruteForceConfig()
	bfp := NewBruteForceProtector(config, database, nil)

	result := bfp.GetLockoutTime("nonexistent_key")
	if result != nil {
		t.Error("expected nil lockout time for non-existent key")
	}
}
