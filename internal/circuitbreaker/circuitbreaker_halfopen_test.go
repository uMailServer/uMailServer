package circuitbreaker

import (
	"errors"
	"testing"
	"time"
)

// The half-open state is documented to limit *concurrent* in-flight probe
// requests (see the comment in Allow). Regression coverage: after a mixed
// half-open round that crosses neither SuccessThreshold nor FailureThreshold,
// the breaker must remain able to admit probes so a recovered service can drive
// it to closed. When halfOpenReq was treated as a lifetime cap (incremented in
// Allow, never decremented on completion), such a round permanently wedged the
// circuit: it could neither close nor re-open because both require Allow() to
// admit a request.
//
// half-open Allow() has no time dependence, so the wedge path is deterministic.

var errProbeFailure = errors.New("probe failure")

func probeOK() error   { return nil }
func probeFail() error { return errProbeFailure }

func newWedgeCB() *CircuitBreaker {
	// SuccessThreshold=FailureThreshold=3 so a mixed round can finish without
	// crossing either threshold.
	return New(Config{
		MaxFailures:      1,
		Timeout:          1 * time.Millisecond,
		SuccessThreshold: 3,
		FailureThreshold: 3,
	})
}

func openBreaker(t *testing.T, cb *CircuitBreaker) {
	t.Helper()
	_ = cb.Execute(probeFail) // MaxFailures=1 -> open
	if cb.State() != StateOpen {
		t.Fatalf("setup: state = %v after initial failure, want open", cb.State())
	}
	// Expire the cooldown deterministically without a scheduling-dependent sleep.
	cb.mutex.Lock()
	cb.lastFailure = time.Now().Add(-cb.config.Timeout - time.Second)
	cb.mutex.Unlock()
}

// A mixed half-open round must not permanently wedge the breaker.
func TestCircuitBreaker_MixedRoundDoesNotWedge(t *testing.T) {
	cb := newWedgeCB()
	openBreaker(t, cb)

	// Drive a MIXED half-open round. Results: successes=2 (<3), failures=2 (<3),
	// so the breaker stays half-open without crossing either threshold.
	for _, probe := range []func() error{probeOK, probeFail, probeFail, probeOK} {
		_ = cb.Execute(probe)
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("setup: state = %v after mixed round, want half-open", cb.State())
	}

	// The service has fully recovered: every probe now succeeds. A correct
	// breaker admits them and closes.
	for i := 0; i < 20; i++ {
		_ = cb.Execute(probeOK)
		if cb.State() == StateClosed {
			return // recovered
		}
	}
	t.Fatalf("breaker wedged in %v after a mixed probe round; a fully recovered "+
		"service was rejected across 20 successful probes and the circuit never closed",
		cb.State())
}

// A fully-successful recovery round closes the breaker (documented happy path).
func TestCircuitBreaker_AllSuccessRecoveryCloses(t *testing.T) {
	cb := New(Config{
		MaxFailures: 1, Timeout: 1 * time.Millisecond,
		SuccessThreshold: 2, FailureThreshold: 2,
	})
	openBreaker(t, cb)
	for i := 0; i < 2; i++ {
		if err := cb.Execute(probeOK); err != nil {
			t.Fatalf("recovery probe %d rejected: %v", i+1, err)
		}
	}
	if cb.State() != StateClosed {
		t.Fatalf("state = %v after full successful recovery, want closed", cb.State())
	}
}

// A fully-failing recovery round re-opens the breaker.
func TestCircuitBreaker_AllFailureReopens(t *testing.T) {
	cb := New(Config{
		MaxFailures: 1, Timeout: 1 * time.Millisecond,
		SuccessThreshold: 2, FailureThreshold: 2,
	})
	openBreaker(t, cb)
	for i := 0; i < 2; i++ {
		_ = cb.Execute(probeFail)
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v after full failing recovery, want open", cb.State())
	}
}

// A completed probe that was admitted in the current half-open round releases
// its slot, so the in-flight counter returns to zero once all probes complete
// and the breaker can admit another probe. The completion is recorded through
// the round-aware path (recordSuccess/recordFailure): the bare
// RecordSuccess/RecordFailure API carries no round id and is therefore
// conservative — it cannot distinguish a current-round probe from a straggler
// still in flight from the previous round, so it does not release (see the
// round-13 stale-completion proof). The Execute-based regressions above
// (MixedRoundDoesNotWedge, AllSuccessRecoveryCloses) still guard the original
// wedge root cause.
func TestCircuitBreaker_HalfOpenSlotReleasedOnCompletion(t *testing.T) {
	for _, success := range []bool{true, false} {
		name := "failure"
		if success {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			cb := New(Config{MaxFailures: 1, Timeout: time.Millisecond, SuccessThreshold: 4, FailureThreshold: 4})
			openBreaker(t, cb)
			// Complete the transition probe, then fill the half-open probe budget.
			if err := cb.Execute(probeOK); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 4; i++ {
				if !cb.Allow() {
					t.Fatalf("probe %d unexpectedly rejected", i)
				}
			}
			if cb.Allow() {
				t.Fatal("admitted a probe while all slots were occupied")
			}
			// Complete one probe that was admitted in the current round.
			gen := cb.currentHalfOpenGen()
			if success {
				cb.recordSuccess(gen, true)
			} else {
				cb.recordFailure(gen, true)
			}
			if cb.State() != StateHalfOpen {
				t.Fatal("completion unexpectedly crossed a state threshold")
			}
			if !cb.Allow() {
				t.Fatal("completed probe did not release its slot")
			}
			if cb.Allow() {
				t.Fatal("completion released more than one slot")
			}
		})
	}
}
