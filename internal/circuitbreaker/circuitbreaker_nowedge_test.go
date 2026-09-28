package circuitbreaker

import (
	"fmt"
	"testing"
	"time"
)

var noWedgeFail = fmt.Errorf("no-wedge probe failure")

// noWedgeBuild drives the breaker to open through the production Execute path,
// expires the cooldown deterministically, then feeds a result sequence (bit 1 =
// success, bit 0 = failure). It uses only the public Execute/State surface, so
// it exercises exactly what production callers (e.g. queue's mxBreaker) use.
func noWedgeBuild(n int, bits []int) *CircuitBreaker {
	cb := New(Config{MaxFailures: 1, Timeout: time.Hour, SuccessThreshold: n, FailureThreshold: n})
	_ = cb.Execute(func() error { return noWedgeFail }) // MaxFailures=1 -> open
	cb.mutex.Lock()
	cb.lastFailure = time.Now().Add(-2 * time.Hour) // expire cooldown
	cb.mutex.Unlock()
	for _, bit := range bits {
		ok := bit == 1
		_ = cb.Execute(func() error {
			if ok {
				return nil
			}
			return noWedgeFail
		})
	}
	return cb
}

// noWedgeIsWedged reports a permanent half-open deadlock: the breaker sits in
// half-open yet a fully recovered service can never close it, because every
// recovery probe is rejected by the saturated admission cap.
func noWedgeIsWedged(cb *CircuitBreaker) bool {
	if cb.State() != StateHalfOpen {
		return false
	}
	for i := 0; i < 100; i++ {
		_ = cb.Execute(func() error { return nil }) // service fully recovered
		if cb.State() != StateHalfOpen {
			return false
		}
	}
	return true
}

// TestCircuitBreaker_NoWedgeAtAnyThreshold exhaustively drives every result
// sequence up to length 5 at several SuccessThreshold/FailureThreshold pairs
// and asserts the breaker never deadlocks in half-open.
//
// Threshold 2/2 is the production default (circuitbreaker.DefaultConfig, used
// by queue's mxBreaker). 3/3 is the threshold at which the historical
// slot-leak bug produced a reproducible permanent wedge, so it is the
// positive control that proves this harness can actually detect a wedge.
func TestCircuitBreaker_NoWedgeAtAnyThreshold(t *testing.T) {
	for _, n := range []int{2, 3, 4} {
		wedged := 0
		for L := 1; L <= 5; L++ {
			for mask := 0; mask < (1 << L); mask++ {
				bits := make([]int, L)
				for i := 0; i < L; i++ {
					bits[i] = (mask >> i) & 1
				}
				if noWedgeIsWedged(noWedgeBuild(n, bits)) {
					wedged++
				}
			}
		}
		if wedged != 0 {
			t.Errorf("thresholds %d/%d: %d result sequence(s) left the breaker "+
				"permanently wedged in half-open; a recovered service can never close it",
				n, n, wedged)
		}
	}
}
