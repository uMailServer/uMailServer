package alert

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// r288RT is a fake webhook transport. If failFirst is set the first request
// parks until aRelease closes and then fails; every other request returns 200
// (or fails when failAll is set).
type r288RT struct {
	mu        sync.Mutex
	calls     int
	aIn       chan struct{}
	aRelease  chan struct{}
	failFirst bool
	failAll   bool
}

func (r *r288RT) RoundTrip(*http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.calls++
	n := r.calls
	r.mu.Unlock()
	if r.failFirst && n == 1 {
		close(r.aIn)
		<-r.aRelease
		return nil, fmt.Errorf("connection reset")
	}
	if r.failAll {
		return nil, fmt.Errorf("down")
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}
func (r *r288RT) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }

func r288Mgr(r *r288RT, mutate func(*Config)) *Manager {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.WebhookURL = "https://hooks.example.com/x"
	cfg.MinInterval = time.Hour
	if mutate != nil {
		mutate(&cfg)
	}
	m := NewManager(cfg, nil)
	m.httpClient.Transport = r
	return m
}

// F7622 regression: A reserves, A's slow delivery outlives MinInterval (simulated
// by backdating the entry), B sends successfully, then A fails. A's rollback
// must not erase B's cooldown.
func TestR288FailedSlowSendDoesNotEraseNewerCooldown(t *testing.T) {
	r := &r288RT{failFirst: true, aIn: make(chan struct{}), aRelease: make(chan struct{})}
	m := r288Mgr(r, nil)
	aErr := make(chan error, 1)
	go func() { aErr <- m.Send("k", SeverityWarning, "m", nil) }()
	<-r.aIn
	m.mu.Lock()
	m.lastAlert["k"] = time.Now().Add(-2 * time.Hour)
	m.mu.Unlock()
	if err := m.Send("k", SeverityWarning, "m", nil); err != nil {
		t.Fatal(err)
	}
	close(r.aRelease)
	if err := <-aErr; err == nil {
		t.Fatal("A should fail")
	}
	before := r.count()
	_ = m.Send("k", SeverityWarning, "m", nil)
	if r.count() != before {
		t.Fatalf("B's cooldown was erased: calls %d -> %d", before, r.count())
	}
}

// HOLDS: a failing dispatch (every channel failed) releases the cooldown so the
// next attempt is allowed; a success keeps it; different keys do not suppress
// each other.
func TestR288CooldownKeyingAndFailureRelease(t *testing.T) {
	r := &r288RT{failAll: true}
	m := r288Mgr(r, nil)
	_ = m.Send("a", SeverityInfo, "m", nil)
	_ = m.Send("a", SeverityInfo, "m", nil)
	if r.count() != 2 {
		t.Fatalf("failed send consumed the cooldown: calls=%d", r.count())
	}
	r2 := &r288RT{}
	m2 := r288Mgr(r2, nil)
	_ = m2.Send("a", SeverityInfo, "m", nil)
	_ = m2.Send("b", SeverityInfo, "m", nil)
	_ = m2.Send("a", SeverityInfo, "m", nil)
	if r2.count() != 2 {
		t.Fatalf("calls=%d want 2 (a, b; second a suppressed)", r2.count())
	}
	m2.CheckDiskSpace(95, "/a")
	m2.CheckDiskSpace(95, "/b")
	if r2.count() != 4 {
		t.Fatalf("per-path disk alerts suppressed each other: calls=%d", r2.count())
	}
}

// HOLDS: hourly cap boundary (MaxAlerts pass, MaxAlerts+1 blocked, window reset
// re-admits) and concurrent same-key Sends dispatch exactly once.
func TestR288HourlyCapAndConcurrentDedup(t *testing.T) {
	r := &r288RT{}
	m := r288Mgr(r, func(c *Config) { c.MaxAlerts = 3 })
	for i := 0; i < 5; i++ {
		_ = m.Send(fmt.Sprintf("k%d", i), SeverityInfo, "m", nil)
	}
	if r.count() != 3 {
		t.Fatalf("cap: calls=%d want 3", r.count())
	}
	m.mu.Lock()
	m.hourStart = time.Now().Add(-time.Hour - time.Second)
	m.mu.Unlock()
	_ = m.Send("k9", SeverityInfo, "m", nil)
	if r.count() != 4 {
		t.Fatalf("window reset did not re-admit: calls=%d", r.count())
	}

	r2 := &r288RT{}
	m2 := r288Mgr(r2, nil)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = m2.Send("same", SeverityInfo, "m", nil) }()
	}
	wg.Wait()
	if r2.count() != 1 {
		t.Fatalf("concurrent same-key sends dispatched %d times", r2.count())
	}
}
