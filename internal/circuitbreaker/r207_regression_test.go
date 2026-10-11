package circuitbreaker

import (
	"errors"
	"testing"
	"time"
)

// F6847: the half-open round id must be captured together with the admission.
func TestAllowReturnsAdmissionRound_F6847(t *testing.T) {
	cb := New(Config{MaxFailures: 1, Timeout: 10 * time.Millisecond, SuccessThreshold: 2, FailureThreshold: 2})
	cb.RecordFailure() // open
	time.Sleep(20 * time.Millisecond)
	ok, gen := cb.allow()
	if !ok || gen != cb.halfOpenGen || gen == 0 {
		t.Fatalf("ok=%v gen=%d halfOpenGen=%d", ok, gen, cb.halfOpenGen)
	}
}

// F6848: unset (zero) thresholds fall back to defaults instead of meaning
// "open on the first failure" / "admit no half-open probes".
func TestNewZeroThresholdsUseDefaults_F6848(t *testing.T) {
	cb := New(Config{Timeout: time.Second})
	cb.RecordFailure()
	if cb.State() != StateClosed {
		t.Fatalf("one failure opened a default-threshold breaker: %v", cb.State())
	}
	for i := 0; i < DefaultConfig().MaxFailures; i++ {
		_ = cb.Execute(func() error { return errors.New("x") })
	}
	if cb.State() != StateOpen {
		t.Fatalf("state = %v, want open", cb.State())
	}
	if cfg := New(Config{}).config; cfg != DefaultConfig() {
		t.Fatalf("zero Config -> %+v", cfg)
	}
}
