package queue

// Regression test for the bounce loop: generateBounce used to enqueue bounce
// messages with the envelope sender "MAILER-DAEMON@umailserver" instead of
// the null sender (RFC 5321 §4.5.5 requires bounces to use a null return
// path, precisely so that a failing bounce can never generate another
// bounce). When a bounce itself failed all retries, handleDeliveryFailure
// generated a bounce for the bounce — addressed to our own
// MAILER-DAEMON@umailserver, an address that can never succeed — producing an
// unbounded self-addressed bounce chain in the queue/database.
//
// The codebase already has the correct guard: generateBounce drops bounces
// whose entry has a null sender ("cannot send bounce: original message had
// null sender"). Making the bounce's envelope sender null lets that guard
// terminate the chain by construction.
//
// Determinism: no real network is used — all MX lookups go through a
// recording fake resolver, all MX connections through the failing dialSMTP
// hook, and MTA-STS through a failing stub resolver. The original message's
// sender domain is not configured in the DB, so DKIM signing is skipped
// before any DNS query.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/store"
)

// recordingResolver implements DNSResolver and records every domain looked up.
type recordingResolver struct {
	mu      sync.Mutex
	domains []string
}

func (r *recordingResolver) LookupMX(domain string) ([]string, error) {
	r.mu.Lock()
	r.domains = append(r.domains, domain)
	r.mu.Unlock()
	return nil, fmt.Errorf("no MX (proof)")
}

func (r *recordingResolver) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.domains...)
}

func (r *recordingResolver) recordedAny(domain string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.domains {
		if d == domain {
			return true
		}
	}
	return false
}

// failingMTASTS satisfies MTASTSDNSResolver and fails every query, so the
// MTA-STS check is a no-op without real DNS traffic.
type failingMTASTS struct{}

func (failingMTASTS) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return nil, errors.New("proof stub")
}

func (failingMTASTS) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return nil, errors.New("proof stub")
}

func (failingMTASTS) LookupMX(ctx context.Context, domain string) ([]*net.MX, error) {
	return nil, errors.New("proof stub")
}

// TestFailedBounceIsNotReBouncedToOurselves pins the contract that a failing
// bounce is never re-bounced: after the original message and its bounce both
// fail, the queue must reach quiescence without ever attempting delivery to
// its own MAILER-DAEMON address.
func TestFailedBounceIsNotReBouncedToOurselves(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	m := NewManager(database, store.NewMaildirStore(dir), dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.SetMaxRetries(1) // bounce on the first failure — keeps the test fast
	// Without a queue budget the Manager runs with maxQueueSize=0 and, under
	// full-suite load, the bounce enqueue can land on the dropped-full path
	// before the sweeper retries it. Give the pipeline room to run.
	m.SetMaxQueueSize(100)
	// Retry backoff is wall-clock based; under full-suite load the real
	// retryDelays could outlive the drive loop below and end the test before
	// the bounce chain ran (observed as a CONTROL FAILED flake). Collapse the
	// backoff to millisecond scale so the chain is timing-independent, and
	// restore the package default afterwards.
	oldDelays := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = oldDelays })
	rec := &recordingResolver{}
	m.resolver = rec                                   // in-package hook: intercept every MX lookup
	m.dialSMTP = func(addr string) (net.Conn, error) { // in-package hook: fail every MX connection
		return nil, errors.New("connection refused (proof)")
	}
	m.SetMTASTSDNSResolver(failingMTASTS{})

	// Start workers exactly like Start(), but drive sweeps manually for
	// determinism (Start's sweeper ticks every 30s).
	ctx, cancel := context.WithCancel(context.Background())
	m.running.Store(true)
	m.deliveryChan = make(chan *db.QueueEntry, 64)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.deliveryWorker(ctx, i)
		}()
	}

	if _, err := m.Enqueue("sender@failing.example", []string{"victim@elsewhere.example"}, []byte("Subject: proof\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Drive sweeps for a fixed window: sweeps hand entries to asynchronous
	// workers, and a worker marks its entry "sending" while it runs — so a
	// pending-only quiescence check can observe an empty queue mid-chain and
	// break before the bounce ever runs (the load-dependent flake). With
	// millisecond-scale retry backoff the whole chain settles in tens of
	// milliseconds; the window is pure margin. The early break below still
	// ends pre-fix runs as soon as the self-addressed lookup is recorded.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.processPendingEntries()
		if rec.recordedAny("umailserver") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Teardown without closing deliveryChan: workers exit via ctx.Done, and
	// generateBounce (running on workers) must never observe a closed channel.
	cancel()
	wg.Wait()

	// CONTROL: the bounce pipeline ran — the recipient domain was looked up
	// for the original entry (and, once generated, for the bounce entry too).
	// Passes before AND after the fix; a broken harness cannot masquerade as
	// the defect.
	if !rec.recordedAny("failing.example") {
		t.Fatalf("CONTROL FAILED (harness): bounce pipeline never ran — failing.example was never looked up; recorded=%v", rec.recorded())
	}

	// THE CONTRACT (RFC 5321 §4.5.5): a failing bounce must never be
	// re-bounced. Before the fix, the bounce carried envelope sender
	// MAILER-DAEMON@umailserver; when it failed, a bounce addressed to our
	// own MAILER-DAEMON@umailserver was created and processed, performing an
	// MX lookup for our own name and continuing the chain forever.
	if rec.recordedAny("umailserver") {
		t.Fatalf("FAIL: self-addressed bounce chain detected — the queue performed an MX lookup for its own MAILER-DAEMON name (umailserver); recorded=%v", rec.recorded())
	}
}
