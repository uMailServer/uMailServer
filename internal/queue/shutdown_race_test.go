package queue

// Regression tests for the shutdown race: Manager.Stop() used to close
// deliveryChan while senders were still active — Enqueue (called by
// generateBounce on workers and by mail acceptance), EnqueueWithNotify,
// RetryEntry, and sweepPendingEntries all send on it. A send on a closed
// channel panics even inside select-with-default, so graceful shutdown could
// crash the whole process ("panic: send on closed channel", observed via
// generateBounce→Enqueue during Stop).
//
// Contract: Stop() must be safe at any point; mail accepted around a
// shutdown is kept pending in the queue database and retried on the next
// Start — never lost, never crashing.

import (
	"context"
	"errors"
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

// TestEnqueueAfterStopKeepsMailPending serializes the race deterministically:
// Stop() fully completes, then Enqueue runs. Pre-fix this panicked with
// "send on closed channel"; post-fix the entry is stored pending and picked
// up on the next Start.
func TestEnqueueAfterStopKeepsMailPending(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	m := NewManager(database, store.NewMaildirStore(dir), dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Fail every MX connection attempt locally: deterministic, no network.
	m.dialSMTP = func(addr string) (net.Conn, error) {
		return nil, errors.New("connection refused (test)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	m.Stop()

	id, err := m.Enqueue("sender@example.com", []string{"victim@elsewhere.example"}, []byte("Subject: t\r\n\r\nb\r\n"))
	if err != nil {
		t.Fatalf("FAIL: Enqueue after Stop = %v — accepted mail must be preserved", err)
	}
	// Enqueue stores one entry per recipient, keyed baseID-0, baseID-1, ...
	entry, err := m.GetQueueEntry(id + "-0")
	if err != nil || entry == nil {
		t.Fatalf("FAIL: entry enqueued after Stop not retrievable (err=%v) — accepted mail was lost", err)
	}
}

// TestConcurrentEnqueueRacingStop hammers Enqueue from several goroutines
// across a Stop; post-fix nothing may panic (run under -race for full effect).
func TestConcurrentEnqueueRacingStop(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	m := NewManager(database, store.NewMaildirStore(dir), dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Fail every MX connection attempt locally: deterministic, no network.
	m.dialSMTP = func(addr string) (net.Conn, error) {
		return nil, errors.New("connection refused (test)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)

	var wg sync.WaitGroup
	stopOnce := sync.Once{}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_, _ = m.Enqueue("sender@example.com", []string{"victim@elsewhere.example"}, []byte("Subject: hammer\r\n\r\nbody\r\n"))
				time.Sleep(time.Millisecond)
			}
			stopOnce.Do(func() {
				m.Stop()
			})
		}(g)
	}
	wg.Wait()
	cancel()

	// The hammer sent at least one entry after Stop; it must be preserved in
	// the queue database for the next start.
	pending, err := m.db.GetPendingQueue(time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("GetPendingQueue: %v", err)
	}
	if len(pending) == 0 {
		t.Fatalf("FAIL: mail accepted around shutdown was lost — no pending entries remain")
	}
}
