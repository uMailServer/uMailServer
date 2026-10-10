package queue

import (
	"github.com/umailserver/umailserver/internal/db"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// F6020: at most maxPerDomain deliveries to one domain run at once; other
// domains keep flowing.
func TestRegressionF6020_PerDomainConcurrencyCap(t *testing.T) {
	var accepted int32
	var cur, peak int32
	release := make(chan struct{})
	dial := func(addr string) (net.Conn, error) {
		if addr == "mx.slow.test:25" {
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			<-release
			atomic.AddInt32(&cur, -1)
		}
		c, s := net.Pipe()
		routePeer(s, "250 ok", &accepted)
		return c, nil
	}
	m := routeManager(t, routeResolver{
		"slow.test": {"mx.slow.test"}, "fast.test": {"mx.fast.test"},
	}, dial)
	m.SetMaxConcurrentPerDomain(2)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); routeSend(t, m, "u@slow.test") }()
	}
	time.Sleep(300 * time.Millisecond)
	if p := atomic.LoadInt32(&peak); p > 2 {
		t.Fatalf("peak concurrent deliveries to one domain = %d, want <= 2", p)
	}
	if e := routeSend(t, m, "u@fast.test"); e.Status == "pending" || e.Status == "sending" {
		t.Fatalf("other domain blocked, status %q", e.Status)
	}
	close(release)
	wg.Wait()
}

// F6021: a peer that never answers must fail the attempt, not hold a worker.
func TestRegressionF6021_TarpitHitsIOTimeout(t *testing.T) {
	dial := func(string) (net.Conn, error) {
		c, _ := net.Pipe() // silent peer
		return c, nil
	}
	m := routeManager(t, routeResolver{"tar.test": {"mx.tar.test"}}, dial)
	m.SetIOTimeout(200 * time.Millisecond)
	done := make(chan struct{})
	go func() { routeSend(t, m, "u@tar.test"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery to a silent peer never timed out")
	}
}

// F6022: an unverified TLS session pooled by an opportunistic delivery must not
// carry a delivery that requires verified TLS.
func TestRegressionF6022_StrictPolicyRejectsPooledUnverifiedTLS(t *testing.T) {
	m, dials := newPoolTestManager(selfSignedMXCert(t))
	if err := deliverPoolTest(m, "b@example.test"); err != nil {
		t.Fatal(err)
	}
	m.SetRequireTLS(true)
	if err := deliverPoolTest(m, "b@example.test"); err != nil {
		t.Fatal(err)
	}
	if d := atomic.LoadInt32(dials); d != 2 {
		t.Fatalf("unverified pooled TLS session reused under requireTLS (dials=%d, want 2)", d)
	}
}

// F6024: a failed multi-recipient Enqueue must not have dispatched any
// recipient to a worker.
func TestRegressionF6024_FailedEnqueueDispatchesNothing(t *testing.T) {
	m, _, database := setupManager(t)
	defer database.Close()
	m.deliveryChan = make(chan *db.QueueEntry, 10)
	m.SetMaxQueueSize(2)
	if _, err := m.Enqueue("a@sender.test", []string{"1@x.test", "2@x.test", "3@x.test"}, []byte("Subject: x\r\n\r\nb\r\n")); err == nil {
		t.Fatal("expected queue-full error")
	}
	if n := len(m.deliveryChan); n != 0 {
		t.Fatalf("%d entries dispatched although Enqueue failed", n)
	}
}
