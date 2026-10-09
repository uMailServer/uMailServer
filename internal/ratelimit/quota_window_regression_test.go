package ratelimit

// Regression tests for finding F5105: a restart must restore the persisted daily quota window instead of starting a new 24h window.

import (
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func v5105DB(t *testing.T) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "rl.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func v5105Cfg() *Config {
	c := DefaultConfig()
	c.UserPerMinute, c.UserPerHour, c.UserPerDay = 0, 0, 2
	return c
}

func TestF5105Reproduction(t *testing.T) {
	db := v5105DB(t)
	a := New(db, v5105Cfg())
	a.CheckUser("u")
	a.userMu.Lock()
	a.userCounters["u"].dayReset = time.Now().Add(time.Hour)
	a.userMu.Unlock()
	a.CheckUser("u")
	a.Stop()
	b := New(db, v5105Cfg())
	defer b.Stop()
	if r := b.CheckUser("u"); r.Allowed || r.RetryAfter > 3600 {
		t.Fatalf("window extended by restart: %+v", r)
	}
}

// Edge: a persisted window that already ended must not block after restart.
func TestF5105ExpiredWindow(t *testing.T) {
	db := v5105DB(t)
	a := New(db, v5105Cfg())
	a.saveUserSentToday("u", 2, time.Now().Add(-time.Minute))
	a.Stop()
	b := New(db, v5105Cfg())
	defer b.Stop()
	if r := b.CheckUser("u"); !r.Allowed {
		t.Fatalf("expired persisted window still blocks: %+v", r)
	}
	if c, reset := b.loadUserSentToday("u"); c != 1 || time.Until(reset) < 23*time.Hour {
		t.Fatalf("new window not persisted: count=%d reset=%v", c, reset)
	}
}

// Edge: legacy 8-byte records without a window end keep the old behaviour.
func TestF5105LegacyRecord(t *testing.T) {
	db := v5105DB(t)
	a := New(db, v5105Cfg())
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], 2)
	if err := db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte("ratelimit_users")).Put([]byte("u:sent_today"), buf[:])
	}); err != nil {
		t.Fatal(err)
	}
	a.Stop()
	b := New(db, v5105Cfg())
	defer b.Stop()
	if r := b.CheckUser("u"); r.Allowed {
		t.Fatalf("legacy spent quota ignored: %+v", r)
	}
}

// Edge: quota still enforced across restart within the window, other users unaffected.
func TestF5105OtherUser(t *testing.T) {
	db := v5105DB(t)
	a := New(db, v5105Cfg())
	a.CheckUser("u")
	a.CheckUser("u")
	a.Stop()
	b := New(db, v5105Cfg())
	defer b.Stop()
	if b.CheckUser("u").Allowed || !b.CheckUser("w").Allowed {
		t.Fatal("quota isolation broken")
	}
}
