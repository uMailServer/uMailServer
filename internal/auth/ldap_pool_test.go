package auth

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeLDAPConn is a minimal pooledLDAPConn double for unit tests.
// closing tracks user-controlled "is this conn dead?" state, closeCount
// records how many times Close was invoked (to assert pool didn't double-close).
type fakeLDAPConn struct {
	id         int
	closing    bool
	closeCount int32
	closeErr   error
}

func (f *fakeLDAPConn) Close() error {
	atomic.AddInt32(&f.closeCount, 1)
	return f.closeErr
}

func (f *fakeLDAPConn) IsClosing() bool {
	return f.closing
}

// newFakeDialer returns a dialer that hands out fresh fakeLDAPConns with
// incrementing ids. The returned slice tracks every conn ever produced so
// tests can assert on close counts.
func newFakeDialer() (ldapDialer, *[]*fakeLDAPConn, *int32) {
	var (
		mu       sync.Mutex
		conns    []*fakeLDAPConn
		dialHits int32
	)
	dialer := func() (pooledLDAPConn, error) {
		atomic.AddInt32(&dialHits, 1)
		mu.Lock()
		defer mu.Unlock()
		c := &fakeLDAPConn{id: len(conns)}
		conns = append(conns, c)
		return c, nil
	}
	return dialer, &conns, &dialHits
}

func TestLDAPPool_DefaultSizeAppliedWhenZero(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 0)
	if p.maxSize != defaultLDAPPoolSize {
		t.Fatalf("expected default size %d, got %d", defaultLDAPPoolSize, p.maxSize)
	}
	if cap(p.conns) != defaultLDAPPoolSize {
		t.Fatalf("expected channel cap %d, got %d", defaultLDAPPoolSize, cap(p.conns))
	}
}

func TestLDAPPool_AcquireDialsWhenEmpty(t *testing.T) {
	dialer, _, dialHits := newFakeDialer()
	p := newLDAPPool(dialer, 3)

	c, err := p.acquire()
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil conn")
	}
	if got := atomic.LoadInt32(dialHits); got != 1 {
		t.Fatalf("expected 1 dial, got %d", got)
	}
}

func TestLDAPPool_ReleaseReusesConn(t *testing.T) {
	dialer, _, dialHits := newFakeDialer()
	p := newLDAPPool(dialer, 3)

	c1, _ := p.acquire()
	p.release(c1)

	c2, err := p.acquire()
	if err != nil {
		t.Fatalf("second acquire error: %v", err)
	}
	if c1 != c2 {
		t.Fatal("expected to receive the same conn back from the pool")
	}
	if got := atomic.LoadInt32(dialHits); got != 1 {
		t.Fatalf("expected only 1 dial after reuse, got %d", got)
	}
}

func TestLDAPPool_AcquireSkipsClosingConns(t *testing.T) {
	dialer, conns, dialHits := newFakeDialer()
	p := newLDAPPool(dialer, 5)

	first, _ := p.acquire()
	first.(*fakeLDAPConn).closing = true
	p.release(first) // release path closes immediately because IsClosing=true

	if atomic.LoadInt32(&first.(*fakeLDAPConn).closeCount) != 1 {
		t.Fatal("expected release to close a closing conn")
	}

	// Inject a stale (IsClosing=true) conn directly into the pool channel
	// to exercise acquire's drain path.
	stale := &fakeLDAPConn{id: 99, closing: true}
	p.conns <- stale

	got, err := p.acquire()
	if err != nil {
		t.Fatalf("acquire error: %v", err)
	}
	if got == stale {
		t.Fatal("acquire returned a stale conn it should have drained")
	}
	if atomic.LoadInt32(&stale.closeCount) != 1 {
		t.Fatal("expected stale conn to be closed during acquire drain")
	}
	if got := atomic.LoadInt32(dialHits); got != 2 {
		t.Fatalf("expected 2 dials (1 reused-closed + 1 fresh after drain), got %d", got)
	}
	_ = conns // keep ref for debugger inspection
}

func TestLDAPPool_ReleaseClosesWhenFull(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 1)

	c1, _ := p.acquire()
	c2, _ := p.acquire() // dialed; pool channel still empty
	p.release(c1)        // fills the pool
	p.release(c2)        // pool full → must close c2

	if got := atomic.LoadInt32(&c2.(*fakeLDAPConn).closeCount); got != 1 {
		t.Fatalf("expected c2 to be closed once when pool full, got %d", got)
	}
	if got := atomic.LoadInt32(&c1.(*fakeLDAPConn).closeCount); got != 0 {
		t.Fatalf("c1 should still be in the pool, but was closed %d times", got)
	}
}

func TestLDAPPool_ReleaseNilIsSafe(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 1)
	p.release(nil) // must not panic
}

func TestLDAPPool_DiscardClosesAndDoesNotReturn(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 2)

	c, _ := p.acquire()
	p.discard(c)
	if got := atomic.LoadInt32(&c.(*fakeLDAPConn).closeCount); got != 1 {
		t.Fatalf("expected discard to close conn once, got %d", got)
	}

	// Pool should still be empty so acquire dials again rather than returning the discarded conn.
	c2, _ := p.acquire()
	if c == c2 {
		t.Fatal("discard returned conn to the pool")
	}
}

func TestLDAPPool_DiscardNilIsSafe(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 1)
	p.discard(nil) // must not panic
}

func TestLDAPPool_CloseDrainsAndBlocksFurtherAcquires(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 3)

	c1, _ := p.acquire()
	c2, _ := p.acquire()
	p.release(c1)
	p.release(c2)

	p.close()

	if got := atomic.LoadInt32(&c1.(*fakeLDAPConn).closeCount); got != 1 {
		t.Fatalf("expected c1 to be closed during pool close, got %d", got)
	}
	if got := atomic.LoadInt32(&c2.(*fakeLDAPConn).closeCount); got != 1 {
		t.Fatalf("expected c2 to be closed during pool close, got %d", got)
	}

	if _, err := p.acquire(); !errors.Is(err, errPoolClosed) {
		t.Fatalf("expected errPoolClosed after close, got %v", err)
	}
}

func TestLDAPPool_CloseIsIdempotent(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 1)
	p.close()
	p.close() // must not panic on closed channel
}

func TestLDAPPool_ReleaseAfterCloseClosesConn(t *testing.T) {
	dialer, _, _ := newFakeDialer()
	p := newLDAPPool(dialer, 1)

	c, _ := p.acquire()
	p.close()
	p.release(c) // must not panic; should close c

	if got := atomic.LoadInt32(&c.(*fakeLDAPConn).closeCount); got != 1 {
		t.Fatalf("expected release-after-close to close conn, got %d", got)
	}
}
