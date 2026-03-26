package security

import (
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// BruteForceProtector implements brute-force attack protection
type BruteForceProtector struct {
	mu        sync.RWMutex
	attempts  map[string]*LoginAttempts
	config    BruteForceConfig
	db        *db.DB
	blocklist *Blocklist
}

// BruteForceConfig holds brute-force protection configuration
type BruteForceConfig struct {
	MaxAttempts       int           // Maximum failed attempts before lockout
	LockoutDuration   time.Duration // Duration of lockout
	AttemptWindow     time.Duration // Window for counting attempts
	AutoBlockDuration time.Duration // Duration of automatic IP block
}

// DefaultBruteForceConfig returns default brute-force protection configuration
func DefaultBruteForceConfig() BruteForceConfig {
	return BruteForceConfig{
		MaxAttempts:       5,
		LockoutDuration:   15 * time.Minute,
		AttemptWindow:     30 * time.Minute,
		AutoBlockDuration: 1 * time.Hour,
	}
}

// LoginAttempts tracks login attempts for a key
type LoginAttempts struct {
	count      int
	firstSeen  time.Time
	lastSeen   time.Time
	lockedUntil *time.Time
}

// NewBruteForceProtector creates a new brute-force protector
func NewBruteForceProtector(config BruteForceConfig, database *db.DB, blocklist *Blocklist) *BruteForceProtector {
	return &BruteForceProtector{
		attempts:  make(map[string]*LoginAttempts),
		config:    config,
		db:        database,
		blocklist: blocklist,
	}
}

// RecordAttempt records a login attempt and returns whether the key is locked
func (bfp *BruteForceProtector) RecordAttempt(key string, success bool) (locked bool, remainingAttempts int) {
	bfp.mu.Lock()
	defer bfp.mu.Unlock()

	now := time.Now()
	attempts, exists := bfp.attempts[key]

	if !exists {
		if success {
			return false, bfp.config.MaxAttempts
		}
		bfp.attempts[key] = &LoginAttempts{
			count:     1,
			firstSeen: now,
			lastSeen:  now,
		}
		return false, bfp.config.MaxAttempts - 1
	}

	// Check if currently locked
	if attempts.lockedUntil != nil && now.Before(*attempts.lockedUntil) {
		return true, 0
	}

	// Reset if outside the attempt window
	if now.Sub(attempts.firstSeen) > bfp.config.AttemptWindow {
		attempts.count = 0
		attempts.firstSeen = now
		attempts.lockedUntil = nil
	}

	// Record the attempt
	if success {
		// Clear attempts on success
		delete(bfp.attempts, key)
		return false, bfp.config.MaxAttempts
	}

	attempts.count++
	attempts.lastSeen = now

	// Check if we should lock
	if attempts.count >= bfp.config.MaxAttempts {
		lockoutEnd := now.Add(bfp.config.LockoutDuration)
		attempts.lockedUntil = &lockoutEnd

		// Auto-block IP if it's an IP-based key
		if bfp.blocklist != nil && len(key) > 0 {
			bfp.blocklist.AddTemporary(key, bfp.config.AutoBlockDuration, "brute-force protection")
		}

		return true, 0
	}

	return false, bfp.config.MaxAttempts - attempts.count
}

// IsLocked checks if a key is currently locked
func (bfp *BruteForceProtector) IsLocked(key string) bool {
	bfp.mu.RLock()
	defer bfp.mu.RUnlock()

	attempts, exists := bfp.attempts[key]
	if !exists {
		return false
	}

	if attempts.lockedUntil != nil && time.Now().Before(*attempts.lockedUntil) {
		return true
	}

	return false
}

// GetLockoutTime returns when the lockout expires
func (bfp *BruteForceProtector) GetLockoutTime(key string) *time.Time {
	bfp.mu.RLock()
	defer bfp.mu.RUnlock()

	attempts, exists := bfp.attempts[key]
	if !exists {
		return nil
	}

	return attempts.lockedUntil
}

// Reset resets the attempts for a key
func (bfp *BruteForceProtector) Reset(key string) {
	bfp.mu.Lock()
	defer bfp.mu.Unlock()
	delete(bfp.attempts, key)
}

// Cleanup removes old attempt records
func (bfp *BruteForceProtector) Cleanup(maxAge time.Duration) {
	bfp.mu.Lock()
	defer bfp.mu.Unlock()

	now := time.Now()
	for key, attempts := range bfp.attempts {
		// Remove if the last seen is older than maxAge
		if now.Sub(attempts.lastSeen) > maxAge {
			delete(bfp.attempts, key)
			continue
		}

		// Remove if lockout has expired and enough time has passed
		if attempts.lockedUntil != nil && now.After(*attempts.lockedUntil) {
			if now.Sub(*attempts.lockedUntil) > 5*time.Minute {
				delete(bfp.attempts, key)
			}
		}
	}
}

// StartCleanup starts a background goroutine to clean up old records
func (bfp *BruteForceProtector) StartCleanup(interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			bfp.Cleanup(maxAge)
		}
	}()
}

// GetStats returns statistics about brute-force protection
func (bfp *BruteForceProtector) GetStats() map[string]interface{} {
	bfp.mu.RLock()
	defer bfp.mu.RUnlock()

	locked := 0
	for _, attempts := range bfp.attempts {
		if attempts.lockedUntil != nil && time.Now().Before(*attempts.lockedUntil) {
			locked++
		}
	}

	return map[string]interface{}{
		"tracked_keys":    len(bfp.attempts),
		"currently_locked": locked,
		"max_attempts":    bfp.config.MaxAttempts,
		"lockout_duration": bfp.config.LockoutDuration.String(),
	}
}
