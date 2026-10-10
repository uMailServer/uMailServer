package ratelimit

import (
	"testing"
	"time"
)

// F5810: cleanup must evict user buckets whose daily window has expired,
// otherwise userCounters grows without bound.
func TestF5810_CleanupEvictsExpiredUserBuckets(t *testing.T) {
	rl := New(nil, DefaultConfig())
	defer rl.Stop()

	rl.CheckUser("old@example.com")
	rl.CheckUser("live@example.com")

	rl.userMu.Lock()
	rl.userCounters["old@example.com"].dayReset = time.Now().Add(-time.Minute)
	rl.userMu.Unlock()

	rl.cleanup()

	rl.userMu.RLock()
	defer rl.userMu.RUnlock()
	if _, ok := rl.userCounters["old@example.com"]; ok {
		t.Fatal("expired user bucket not evicted")
	}
	if _, ok := rl.userCounters["live@example.com"]; !ok {
		t.Fatal("live user bucket evicted")
	}
}
