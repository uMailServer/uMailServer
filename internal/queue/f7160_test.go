package queue

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
)

// F7160: authenticated TLSA records make TLS mandatory; a refused STARTTLS must
// not fall back to plaintext.
func TestRegressionF7160_DANENoPlaintextFallback(t *testing.T) {
	var accepted int32
	m := routeManager(t, routeResolver{"dane.test": {"mx.dane.test"}}, func(string) (net.Conn, error) {
		c, s := net.Pipe()
		routePeer(s, "250 ok", &accepted)
		return c, nil
	})
	m.daneValidator = auth.NewDANEValidator(&tlsaStub{})
	m.SetDANESecureHook(func(string) bool { return true })
	e := routeSend(t, m, "u@dane.test")
	if atomic.LoadInt32(&accepted) != 0 {
		t.Fatal("message delivered in plaintext despite DANE")
	}
	if e.Status != "pending" {
		t.Fatalf("status = %q, want pending (retry)", e.Status)
	}
	// Without the secure hook DANE stays advisory and delivery proceeds.
	m.SetDANESecureHook(nil)
	routeSend(t, m, "u2@dane.test")
	if atomic.LoadInt32(&accepted) != 1 {
		t.Fatal("advisory DANE must still deliver")
	}
}

// F7161: a missing spool file bounces at once instead of retrying for days.
func TestRegressionF7161_MissingSpoolFilePermanent(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	id, err := m.Enqueue("a@sender.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.db.GetQueueEntry(id + "-0")
	deleteFile(e.MessagePath)
	m.deliver(t.Context(), e)
	e, _ = m.db.GetQueueEntry(id + "-0")
	if e.Status != "bounced" {
		t.Fatalf("status = %q, want bounced", e.Status)
	}
}

// F7162: maxRetries is read under the lock (run with -race).
func TestRegressionF7162_MaxRetriesRace(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			m.SetMaxRetries(5 + i%3)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			e := &db.QueueEntry{ID: "r" + string(rune('a'+i%20)), To: []string{"x@y.test"}, Status: "sending", NextRetry: time.Now()}
			_ = m.db.UpdateQueueEntry(e)
			m.failDelivery(e, "boom", false)
		}
	}()
	wg.Wait()
}

// F7163: NOTIFY=DELAY yields one delay DSN on the first temporary failure.
func TestRegressionF7163_DelayDSN(t *testing.T) {
	m := routeManager(t, mxErrResolver{err: &net.DNSError{Err: "timeout", IsTimeout: true}}, nil)
	id, err := m.EnqueueWithNotify("a@sender.test", []string{"b@y.test"}, []string{"DELAY"}, []byte("Subject: s\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := m.db.GetQueueEntry(id + "-0")
	m.deliver(t.Context(), e)
	if n := routeFailureDSNs(m); n != 1 {
		t.Fatalf("delay DSNs = %d, want 1", n)
	}
	e, _ = m.db.GetQueueEntry(id + "-0")
	e.NextRetry = time.Now().Add(-time.Second)
	_ = m.db.UpdateQueueEntry(e)
	m.deliver(t.Context(), e)
	if n := routeFailureDSNs(m); n != 1 {
		t.Fatalf("second failure must not send another delay DSN, got %d", n)
	}
}
