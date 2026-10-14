package ratelimit

import (
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// F7520: a persisted daily window ending more than 24h ahead (written while the
// clock was wrong) must be discarded, not honoured for years.
func TestR278PersistedFutureWindowDiscarded(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		reset   time.Time
		allowed bool
	}{
		{"years ahead", now.AddDate(9, 0, 0), true},
		{"just over 24h", now.Add(24*time.Hour + time.Second), true},
		{"exactly 24h", now.Add(24 * time.Hour), false},
		{"within window", now.Add(time.Hour), false},
		{"expired", now.Add(-time.Minute), true},
	}
	for _, c := range cases {
		db, err := bbolt.Open(filepath.Join(t.TempDir(), "rl.db"), 0o600, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg := DefaultConfig()
		cfg.UserPerMinute, cfg.UserPerHour, cfg.UserPerDay = 0, 0, 2
		rl := New(db, cfg)
		rl.nowFn = func() time.Time { return now }
		rl.saveUserSentToday("u", 2, c.reset)
		if r := rl.CheckUser("u"); r.Allowed != c.allowed {
			t.Errorf("%s: allowed=%v want %v", c.name, r.Allowed, c.allowed)
		}
		rl.Stop()
		_ = db.Close()
	}
}
