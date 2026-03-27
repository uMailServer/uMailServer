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
