package queue

import (
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F6390: addresses with CR/LF must not inject headers or DSN fields.
func TestRegressionF6390_DSNNoHeaderInjection(t *testing.T) {
	dsn := &DSN{
		ReportedDomain: "umailserver",
		ArrivalDate:    time.Now(),
		OriginalFrom:   "a@x.test\r\nBcc: evil@evil.test",
		OriginalTo:     "b@y.test\r\nAction: delivered",
		Recipient:      DSNRecipient{Original: "b@y.test\r\nStatus: 2.0.0", DSNAddress: "c@z.test\nX-Evil: 1"},
		Action:         "failed", Status: "5.0.0",
	}
	out, err := GenerateDSN(dsn, []byte("Subject: s\r\n\r\nb"), DSNRetFull)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(out), "\r\n") {
		if strings.HasPrefix(l, "Bcc:") || strings.HasPrefix(l, "X-Evil:") || l == "Action: delivered" || l == "Status: 2.0.0" {
			t.Fatalf("injected line %q", l)
		}
	}
}

// F6391: fallback bounce must not take CR/LF from sender or LastError.
func TestRegressionF6391_FallbackBounceNoInjection(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	e := &db.QueueEntry{
		From: "a@x.test\r\nBcc: evil@evil.test", To: []string{"b@y.test"},
		LastError: "550 no\r\nX-Evil: 1", CreatedAt: time.Now(),
	}
	out := string(m.createFallbackBounce(e, []byte("Subject: s\r\n\r\nb")))
	for _, l := range strings.Split(out, "\r\n") {
		if strings.HasPrefix(l, "Bcc:") || strings.HasPrefix(l, "X-Evil:") {
			t.Fatalf("injected line %q", l)
		}
	}
}

// F6392: duplicate MX hosts must be tried once.
func TestRegressionF6392_MXDuplicatesTriedOnce(t *testing.T) {
	var dials int32
	m := routeManager(t, routeResolver{"d.test": {"mx.d.test", "MX.d.test.", "mx.d.test", "mx2.d.test"}},
		func(host string) (net.Conn, error) {
			atomic.AddInt32(&dials, 1)
			return nil, errors.New("down")
		})
	routeSend(t, m, "u@d.test")
	if got := atomic.LoadInt32(&dials); got != 2 {
		t.Fatalf("dials = %d, want 2", got)
	}
}

// F6393: concurrent claims of one entry must yield exactly one winner.
func TestRegressionF6393_ClaimEntryExactlyOnce(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	id, err := m.Enqueue("a@x.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	const n = 32
	start := make(chan struct{})
	var wins int32
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			if _, ok := m.claimEntry(id + "-0"); ok {
				atomic.AddInt32(&wins, 1)
			}
			done <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < n; i++ {
		<-done
	}
	if wins != 1 {
		t.Fatalf("claim winners = %d, want 1", wins)
	}
}

// F6394: per-domain cap is enforced and slots are released.
func TestRegressionF6394_DomainSlotCap(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	m.SetMaxConcurrentPerDomain(2)
	a, b, c := m.acquireDomainSlot("d"), m.acquireDomainSlot("d"), m.acquireDomainSlot("d")
	if !a || !b || c {
		t.Fatal("cap of 2 not enforced")
	}
	if !m.acquireDomainSlot("other") {
		t.Fatal("other domain blocked")
	}
	m.releaseDomainSlot("d")
	if !m.acquireDomainSlot("d") {
		t.Fatal("slot not released")
	}
}

// F6395: retry jitter stays within +-20% of the schedule and entries stuck in
// "sending" are requeued at Start.
func TestRegressionF6395_JitterBoundsAndRecovery(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	for i := 0; i < 200; i++ {
		e := &db.QueueEntry{ID: "j", From: "a@x.test", To: []string{"b@y.test"}, Status: "sending"}
		if err := m.db.EnqueueWithLimit(e, 100); err != nil {
			t.Fatal(err)
		}
		before := time.Now()
		m.failDelivery(e, "451 try later", false)
		d := e.NextRetry.Sub(before)
		if d < 4*time.Minute || d > 6*time.Minute+time.Second {
			t.Fatalf("delay %v outside 5m +-20%%", d)
		}
		_ = m.db.Dequeue("j")
	}
	e := &db.QueueEntry{ID: "stuck", From: "a@x.test", To: []string{"b@y.test"}, Status: "sending", NextRetry: time.Now().Add(time.Hour)}
	if err := m.db.EnqueueWithLimit(e, 100); err != nil {
		t.Fatal(err)
	}
	m.requeueInterruptedEntries()
	got, _ := m.db.GetQueueEntry("stuck")
	if got.Status != "pending" || got.NextRetry.After(time.Now()) {
		t.Fatalf("stuck entry not recovered: %+v", got)
	}
}
