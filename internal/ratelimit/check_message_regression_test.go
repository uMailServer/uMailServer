package ratelimit

import (
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// F5205: CheckMessage counts a message against the user, IP and global
// limits only when all of them allow it; a refusal by one spends nothing
// from the others, in particular not the persisted daily quota.
func TestCheckMessage_RefusalSpendsNothing(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "rl.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rl := New(db, &Config{UserPerDay: 10, IPPerMinute: 1, GlobalPerMinute: 100})
	defer rl.Stop()
	for i := 0; i < 3; i++ {
		if got := rl.CheckMessage("u", "192.0.2.1").Allowed; got != (i == 0) {
			t.Fatalf("message %d allowed=%v", i, got)
		}
	}
	if c, _ := rl.loadUserSentToday("u"); c != 1 {
		t.Fatalf("persisted sent_today = %d, want 1", c)
	}
	if rl.globalBucket.minuteCount != 1 {
		t.Fatalf("global count = %d, want 1", rl.globalBucket.minuteCount)
	}

	// A global refusal leaves the IP counter alone.
	rl2 := New(nil, &Config{IPPerMinute: 100, GlobalPerMinute: 1})
	defer rl2.Stop()
	rl2.CheckMessage("", "192.0.2.2")
	if r := rl2.CheckMessage("", "192.0.2.2"); r.Allowed {
		t.Fatal("global limit not enforced")
	}
	if got := rl2.GetIPStats("192.0.2.2")["minute_count"]; got != 1 {
		t.Fatalf("ip minute_count = %v, want 1", got)
	}

	// A user refusal leaves IP and global alone.
	rl3 := New(nil, &Config{UserPerDay: 1, IPPerMinute: 100, GlobalPerMinute: 100})
	defer rl3.Stop()
	rl3.CheckMessage("v", "192.0.2.3")
	if r := rl3.CheckMessage("v", "192.0.2.3"); r.Allowed || r.Reason != "Daily sending quota exceeded: 1/day" {
		t.Fatalf("got %+v", r)
	}
	if got := rl3.GetIPStats("192.0.2.3")["minute_count"]; got != 1 {
		t.Fatalf("ip minute_count = %v, want 1", got)
	}
}
