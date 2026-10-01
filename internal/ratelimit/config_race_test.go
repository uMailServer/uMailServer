package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// These tests pin the locking discipline for RateLimiter.config.
//
// SetConfig swaps rl.config under configMu, and cleanupLoop already documents
// that rl.config "must be read under the same lock that SetConfig holds" —
// otherwise the goroutine started by New() races with a runtime config update.
// Every other reader of rl.config has to obey the same rule, including the
// check helpers on the SMTP hot path, which are called for every inbound
// message (internal/smtp/pipeline.go) while an operator can trigger SetConfig
// at any moment through the admin API (internal/api/ratelimit.go).

// round2Config returns a config with distinctive values so that repeated
// writes are distinguishable from one another.
func round2Config(seed int) *Config {
	return &Config{
		IPPerMinute:       10 + seed,
		IPPerHour:         20 + seed,
		IPPerDay:          30 + seed,
		IPConnections:     4 + seed,
		UserPerMinute:     40 + seed,
		UserPerHour:       50 + seed,
		UserPerDay:        60 + seed,
		UserMaxRecipients: 5 + seed,
		GlobalPerMinute:   70 + seed,
		GlobalPerHour:     80 + seed,
		// A long interval keeps the cleanup goroutine parked, so the only
		// config reader in flight is the one this test starts.
		CleanupInterval: time.Hour,
	}
}

// round2RunSwapWithReaders runs concurrent SetConfig calls (the admin API path)
// alongside the supplied read function, which represents one reader of
// rl.config. Run under `go test -race`; a reader that bypasses configMu makes
// the detector report a WRITE in SetConfig racing a READ in that reader.
func round2RunSwapWithReaders(t *testing.T, read func(rl *RateLimiter)) {
	t.Helper()

	rl := New(nil, round2Config(0))
	defer rl.Stop()

	// Let the cleanup goroutine finish the one config read it does at startup,
	// so it cannot be mistaken for the reader under test.
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: operator updates the rate limit config at runtime.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			rl.SetConfig(round2Config(i % 1000))
		}
	}()

	// Readers: the checks the SMTP pipeline runs for every message.
	readers := 4
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				read(rl)
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestConfigReadsAreLockProtected is the failing proof: the check path reads
// rl.config while SetConfig replaces it.
func TestConfigReadsAreLockProtected(t *testing.T) {
	round2RunSwapWithReaders(t, func(rl *RateLimiter) {
		rl.CheckUser("user@example.com")
		rl.CheckIP("192.0.2.1")
		rl.CheckGlobal()
		rl.CheckRecipients("user@example.com", 3)
		rl.CheckConnection("198.51.100.7")
		_ = rl.GetUserStats("user@example.com")
	})
}

// TestGetConfigIsRaceFree is the control: identical concurrency, but the reader
// goes through GetConfig, which already holds configMu. It must stay race-free
// before and after the fix, and it shows the harness really does run a config
// reader concurrently with the writer.
func TestGetConfigIsRaceFree(t *testing.T) {
	round2RunSwapWithReaders(t, func(rl *RateLimiter) {
		_ = rl.GetConfig()
	})
}
