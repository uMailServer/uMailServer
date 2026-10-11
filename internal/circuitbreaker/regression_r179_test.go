package circuitbreaker

import (
	"testing"
	"time"
)

// F6572: callers using Allow + RecordSuccess/RecordFailure (no Execute) never
// release half-open slots. A mixed round (1 success, 1 failure, both
// thresholds=2) left the breaker in half-open forever, rejecting everything.
func TestR179HalfOpenMixedRoundDoesNotWedge(t *testing.T) {
	cb := New(Config{MaxFailures: 1, Timeout: time.Minute, SuccessThreshold: 2, FailureThreshold: 2})
	cb.RecordFailure() // open
	cb.lastFailure = time.Now().Add(-2 * time.Minute)
	if !cb.Allow() || !cb.Allow() || cb.Allow() {
		t.Fatal("expected exactly SuccessThreshold probes")
	}
	cb.RecordSuccess()
	cb.RecordFailure() // mixed: neither threshold crossed
	if cb.State() != StateHalfOpen {
		t.Fatalf("state=%v want half-open", cb.State())
	}
	if cb.Allow() {
		t.Fatal("slots still held before the round times out")
	}
	// Round exceeded Timeout without resolving: a new round must start.
	cb.halfOpenSince = time.Now().Add(-2 * time.Minute)
	if !cb.Allow() {
		t.Fatal("breaker wedged in half-open: no new probe admitted after Timeout")
	}
}
