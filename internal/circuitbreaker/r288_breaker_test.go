package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errR288 = errors.New("boom")

func r288Open(t *testing.T, cfg Config) *CircuitBreaker {
	t.Helper()
	cb := New(cfg)
	for i := 0; i < cfg.MaxFailures; i++ {
		_ = cb.Execute(func() error { return errR288 })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state=%v want open", cb.State())
	}
	// No clock seam: age the failure past Timeout.
	cb.mutex.Lock()
	cb.lastFailure = time.Now().Add(-cfg.Timeout - time.Second)
	cb.mutex.Unlock()
	return cb
}

// HOLDS: concurrent callers in half-open never exceed SuccessThreshold probes.
// Gated: probes park on a gate until every excess caller has been rejected.
func TestR288HalfOpenAdmitsAtMostConfiguredProbes(t *testing.T) {
	cfg := Config{MaxFailures: 2, Timeout: time.Minute, SuccessThreshold: 3, FailureThreshold: 2}
	cb := r288Open(t, cfg)
	const callers = 24
	var running, maxRunning, rejected atomic.Int32
	gate := make(chan struct{})
	entered := make(chan struct{}, callers)
	rejectedCh := make(chan struct{}, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := cb.Execute(func() error {
				n := running.Add(1)
				for {
					m := maxRunning.Load()
					if n <= m || maxRunning.CompareAndSwap(m, n) {
						break
					}
				}
				entered <- struct{}{}
				<-gate
				running.Add(-1)
				return nil
			})
			if errors.Is(err, ErrCircuitOpen) {
				rejected.Add(1)
				rejectedCh <- struct{}{}
			}
		}()
	}
	for i := 0; i < callers-cfg.SuccessThreshold; i++ {
		<-rejectedCh
	}
	close(gate)
	wg.Wait()
	if got := maxRunning.Load(); got != int32(cfg.SuccessThreshold) {
		t.Fatalf("max concurrent probes=%d want %d", got, cfg.SuccessThreshold)
	}
	if cb.State() != StateClosed {
		t.Fatalf("state=%v want closed after %d successes", cb.State(), cfg.SuccessThreshold)
	}
	if m := cb.Metrics(); m.Failures != 0 || m.Successes != 0 || m.HalfOpenRequests != 0 {
		t.Fatalf("counters not reset on close: %+v", m)
	}
}

// HOLDS: a panic in fn releases the half-open slot (counted as a failure).
func TestR288PanicReleasesHalfOpenSlot(t *testing.T) {
	cfg := Config{MaxFailures: 1, Timeout: time.Minute, SuccessThreshold: 1, FailureThreshold: 5}
	cb := r288Open(t, cfg)
	func() {
		defer func() { _ = recover() }()
		_ = cb.Execute(func() error { panic("x") })
	}()
	if m := cb.Metrics(); m.HalfOpenRequests != 0 || m.Failures != 1 {
		t.Fatalf("slot not released after panic: %+v", m)
	}
	if err := cb.Execute(func() error { return nil }); err != nil {
		t.Fatalf("next probe rejected after panic: %v", err)
	}
	if cb.State() != StateClosed {
		t.Fatalf("state=%v want closed", cb.State())
	}
}

// HOLDS: exact thresholds (>=), and a failed half-open probe set re-opens.
func TestR288ExactThresholds(t *testing.T) {
	cfg := Config{MaxFailures: 3, Timeout: time.Minute, SuccessThreshold: 2, FailureThreshold: 2}
	cb := New(cfg)
	for i := 1; i <= 2; i++ {
		_ = cb.Execute(func() error { return errR288 })
		if cb.State() != StateClosed {
			t.Fatalf("opened early at %d failures", i)
		}
	}
	_ = cb.Execute(func() error { return nil }) // success resets the failure run
	for i := 1; i <= 2; i++ {
		_ = cb.Execute(func() error { return errR288 })
	}
	if cb.State() != StateClosed {
		t.Fatal("success did not reset consecutive failures")
	}
	_ = cb.Execute(func() error { return errR288 })
	if cb.State() != StateOpen {
		t.Fatal("not open at exactly MaxFailures")
	}
	if err := cb.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("open circuit admitted a call: %v", err)
	}
	cb.mutex.Lock()
	cb.lastFailure = time.Now().Add(-time.Minute - time.Second)
	cb.mutex.Unlock()
	_ = cb.Execute(func() error { return errR288 }) // probe 1 fails
	if cb.State() != StateHalfOpen {
		t.Fatalf("state=%v: one failure < FailureThreshold must stay half-open", cb.State())
	}
	_ = cb.Execute(func() error { return errR288 }) // probe 2 fails
	if cb.State() != StateOpen {
		t.Fatalf("state=%v: FailureThreshold failures must re-open", cb.State())
	}
	// Still inside Timeout since the last failure: rejected.
	if err := cb.Execute(func() error { return nil }); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("re-opened circuit admitted a call: %v", err)
	}
}
