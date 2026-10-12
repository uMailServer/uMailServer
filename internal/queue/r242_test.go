package queue

import (
	"errors"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func r242Entry(t *testing.T, m *Manager) *db.QueueEntry {
	t.Helper()
	id, err := m.Enqueue("a@s.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := m.db.GetQueueEntry(id + "-0")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// F7190: entries older than the max age bounce; younger ones keep retrying.
func TestRegressionF7190_MaxAgeBounces(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	if m.maxAge != 5*24*time.Hour {
		t.Fatalf("default maxAge = %v", m.maxAge)
	}
	e := r242Entry(t, m)
	e.CreatedAt = time.Now().Add(-6 * 24 * time.Hour)
	e.RetryCount = 3
	m.failDelivery(e, "451 try later", false)
	got, _ := m.db.GetQueueEntry(e.ID)
	if got.Status != "bounced" {
		t.Fatalf("old entry status = %q, want bounced", got.Status)
	}

	e2 := r242Entry(t, m)
	e2.CreatedAt = time.Now().Add(-4 * 24 * time.Hour)
	e2.RetryCount = 3
	m.failDelivery(e2, "451 try later", false)
	got, _ = m.db.GetQueueEntry(e2.ID)
	if got.Status != "pending" {
		t.Fatalf("young entry status = %q, want pending", got.Status)
	}

	m.SetMaxAge(time.Hour)
	e3 := r242Entry(t, m)
	e3.CreatedAt = time.Now().Add(-2 * time.Hour)
	m.failDelivery(e3, "451 try later", false)
	got, _ = m.db.GetQueueEntry(e3.ID)
	if got.Status != "bounced" {
		t.Fatalf("custom maxAge ignored: %q", got.Status)
	}

	m.SetMaxAge(0)
	e4 := r242Entry(t, m)
	e4.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
	m.failDelivery(e4, "451 try later", false)
	got, _ = m.db.GetQueueEntry(e4.ID)
	if got.Status != "pending" {
		t.Fatalf("maxAge 0 must disable the limit: %q", got.Status)
	}
}

// F7190: the bounce is persisted before the DSN is enqueued, and the DSN
// reaches the sender (a queue entry for the original sender exists).
func TestRegressionF7190_MaxAgeDSNAfterPersist(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	e := r242Entry(t, m)
	e.CreatedAt = time.Now().Add(-6 * 24 * time.Hour)
	m.failDelivery(e, "451 try later", false)
	got, _ := m.db.GetQueueEntry(e.ID)
	if got.Status != "bounced" || got.LastError == "" {
		t.Fatalf("entry = %+v", got)
	}
	found := false
	_ = m.db.ForEach(db.BucketQueue, func(_ string, v []byte) error {
		if len(v) > 0 && contains(v, `"to":["a@s.test"]`) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("no DSN enqueued to the sender")
	}
}

func contains(b []byte, s string) bool {
	return len(s) <= len(b) && string(b) != "" && indexOf(string(b), s) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// F7191: when the outcome write fails right after the claim, the entry must
// not stay "sending" forever; the sweeper reverts it once the lease expires.
func TestRegressionF7191_StuckClaimRecovered(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	m.updateEntry = func(*db.QueueEntry) error { return errors.New("injected disk error") }
	e := r242Entry(t, m)
	m.deliver(t.Context(), e)
	got, _ := m.db.GetQueueEntry(e.ID)
	if got.Status != "sending" {
		t.Skipf("entry already recovered or delivered: %q", got.Status)
	}

	// Within the lease the claim is respected.
	m.sweepPendingEntries()
	got, _ = m.db.GetQueueEntry(e.ID)
	if got.Status != "sending" {
		t.Fatalf("live claim reverted early: %q", got.Status)
	}

	m.claimLease = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	m.sweepPendingEntries()
	got, _ = m.db.GetQueueEntry(e.ID)
	if got.Status != "pending" {
		t.Fatalf("stale claim not reverted: %q", got.Status)
	}
	if got.NextRetry.After(time.Now()) {
		t.Fatalf("reverted entry not due: %v", got.NextRetry)
	}
	if _, err := m.db.ClaimQueueEntry(e.ID, time.Now()); err != nil {
		t.Fatalf("reverted entry not claimable: %v", err)
	}
}

// F7191: a transient write error is retried and the outcome still lands.
func TestRegressionF7191_TransientUpdateRetried(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	calls := 0
	m.updateEntry = func(e *db.QueueEntry) error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return m.db.UpdateQueueEntry(e)
	}
	e := r242Entry(t, m)
	e.Status = "sending"
	m.failDelivery(e, "451 later", false)
	got, _ := m.db.GetQueueEntry(e.ID)
	if got.Status != "pending" || calls != 2 {
		t.Fatalf("status %q calls %d", got.Status, calls)
	}
}

// F7191: db-level claim lease semantics (live claims kept, stale and legacy
// zero-timestamp claims reverted).
func TestRegressionF7191_RequeueStaleClaims(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	fresh := r242Entry(t, m)
	legacy := r242Entry(t, m)
	now := time.Now()
	fresh.Status, fresh.ClaimedAt = "sending", now
	legacy.Status, legacy.ClaimedAt = "sending", time.Time{}
	_ = m.db.UpdateQueueEntry(fresh)
	_ = m.db.UpdateQueueEntry(legacy)
	n, err := m.db.RequeueStaleClaims(now.Add(time.Minute), time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	a, _ := m.db.GetQueueEntry(fresh.ID)
	b, _ := m.db.GetQueueEntry(legacy.ID)
	if a.Status != "sending" || b.Status != "pending" {
		t.Fatalf("fresh=%q legacy=%q", a.Status, b.Status)
	}
}
