package queue

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F7292/F7293: a successful remote delivery whose outcome write fails must not
// be delivered a second time when the claim lease expires and the sweeper
// requeues the entry.
func TestRegressionF7292_NoRedeliveryAfterOutcomeWriteFailure(t *testing.T) {
	m, accepted := newDispatchTestManager(t)
	var failDelivered atomic.Bool
	failDelivered.Store(true)
	m.updateEntry = func(e *db.QueueEntry) error {
		if e.Status == "delivered" && failDelivered.Load() {
			return errors.New("injected disk error")
		}
		return m.db.UpdateQueueEntry(e)
	}
	id, err := m.Enqueue("a@s.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	eid := id + "-0"
	if n := drainDispatched(m); n != 1 {
		t.Fatalf("dispatched %d", n)
	}
	if got := atomic.LoadInt32(accepted); got != 1 {
		t.Fatalf("accepted %d, want 1", got)
	}
	if s := queueStatus(t, m, eid); s != "sending" {
		t.Fatalf("status %q, want sending (outcome write failed)", s)
	}

	// Lease expires; sweeper requeues; the entry must be finalized, not resent.
	m.claimLease = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	m.sweepPendingEntries()
	drainDispatched(m)
	if got := atomic.LoadInt32(accepted); got != 1 {
		t.Fatalf("message redelivered: accepted %d, want 1", got)
	}

	// Disk recovers: the entry finalizes as delivered, still one delivery.
	failDelivered.Store(false)
	time.Sleep(5 * time.Millisecond)
	m.sweepPendingEntries()
	drainDispatched(m)
	if got := atomic.LoadInt32(accepted); got != 1 {
		t.Fatalf("accepted %d after recovery, want 1", got)
	}
	if s := queueStatus(t, m, eid); s != "delivered" {
		t.Fatalf("status %q, want delivered", s)
	}
	if _, err := readFile(m.deliveredMarkerPath(eid)); err == nil {
		t.Fatal("delivered marker not cleaned up")
	}
}

// F7292: a transient outcome write failure is retried until it lands.
func TestRegressionF7292_DeliveredWriteRetriedLonger(t *testing.T) {
	m, accepted := newDispatchTestManager(t)
	calls := 0
	m.updateEntry = func(e *db.QueueEntry) error {
		if e.Status == "delivered" {
			calls++
			if calls <= 5 {
				return errors.New("transient")
			}
		}
		return m.db.UpdateQueueEntry(e)
	}
	m.persistBackoff = time.Millisecond
	id, _ := m.Enqueue("a@s.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb\r\n"))
	drainDispatched(m)
	if s := queueStatus(t, m, id+"-0"); s != "delivered" || atomic.LoadInt32(accepted) != 1 {
		t.Fatalf("status %q accepted %d calls %d", s, atomic.LoadInt32(accepted), calls)
	}
}

// F7294: the failure DSN must be queued before the entry is marked bounced,
// so a crash right after the bounced write cannot lose it.
func TestRegressionF7294_DSNSurvivesCrashAfterBouncedWrite(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	e := r242Entry(t, m)
	e.Status = "sending"
	m.updateEntry = func(x *db.QueueEntry) error {
		err := m.db.UpdateQueueEntry(x)
		if x.Status == "bounced" {
			panic("simulated crash after bounced write")
		}
		return err
	}
	func() {
		defer func() { _ = recover() }()
		m.failDelivery(e, "550 5.1.1 no such user", true)
	}()
	found := false
	_ = m.db.ForEach(db.BucketQueue, func(_ string, v []byte) error {
		var q db.QueueEntry
		if err := json.Unmarshal(v, &q); err == nil && len(q.To) == 1 && q.To[0] == e.From && q.From == "" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("DSN lost: bounced entry persisted but no DSN queued")
	}
}

// F7294: if the bounced write itself fails, the entry stays recoverable and
// the message file is kept for the retry.
func TestRegressionF7294_BouncedWriteFailureKeepsMessage(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	e := r242Entry(t, m)
	e.Status = "sending"
	m.updateEntry = func(*db.QueueEntry) error { return errors.New("injected") }
	m.persistBackoff = time.Millisecond
	m.failDelivery(e, "550 5.1.1 no such user", true)
	if _, err := readFile(e.MessagePath); err != nil {
		t.Fatalf("message file removed though bounce not persisted: %v", err)
	}
}
