package circuitbreaker

import (
	"errors"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	config := DefaultConfig()
	cb := New(config)

	if cb == nil {
		t.Fatal("New returned nil")
	}

	if cb.State() != StateClosed {
		t.Errorf("expected initial state closed, got %s", cb.State())
	}
}

func TestNewDefault(t *testing.T) {
	cb := NewDefault()

	if cb == nil {
		t.Fatal("NewDefault returned nil")
	}

	if cb.State() != StateClosed {
		t.Errorf("expected initial state closed, got %s", cb.State())
	}
}

func TestCircuitBreaker_Allow(t *testing.T) {
	cb := New(Config{
		MaxFailures: 3,
		Timeout:     100 * time.Millisecond,
	})

	// Should allow initially
	if !cb.Allow() {
		t.Error("should allow requests initially")
	}

	// Record failures to open circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Should not allow when open
	if cb.Allow() {
		t.Error("should not allow requests when open")
	}

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Should allow in half-open
	if !cb.Allow() {
		t.Error("should allow requests in half-open after timeout")
	}
}

func TestCircuitBreaker_Execute(t *testing.T) {
	cb := New(Config{
		MaxFailures: 2,
		Timeout:     100 * time.Millisecond,
	})

	// Execute successful function
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Execute failing function multiple times
	for i := 0; i < 2; i++ {
		err = cb.Execute(func() error {
			return errors.New("test error")
		})
		if err == nil || err == ErrCircuitOpen {
			t.Error("expected function error")
		}
	}

	// Circuit should be open now
	err = cb.Execute(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := New(Config{
		MaxFailures:      2,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 2,
		FailureThreshold: 1,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state open, got %s", cb.State())
	}

	// Wait for half-open
	time.Sleep(100 * time.Millisecond)

	// Call Allow() to transition to half-open
	if !cb.Allow() {
		t.Fatal("should allow requests after timeout")
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("expected state half-open, got %s", cb.State())
	}

	// First success
	cb.RecordSuccess()

	// Second success should close
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Errorf("expected state closed after successes, got %s", cb.State())
	}
}

func TestCircuitBreaker_Metrics(t *testing.T) {
	cb := New(Config{
		MaxFailures: 3,
	})

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()

	metrics := cb.Metrics()

	if metrics.State != "closed" {
		t.Errorf("expected state closed in metrics, got %s", metrics.State)
	}

	if metrics.Failures != 2 {
		t.Errorf("expected 2 failures, got %d", metrics.Failures)
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestManager(t *testing.T) {
	m := NewManager()

	// Get or create
	cb1 := m.Get("test")
	if cb1 == nil {
		t.Fatal("Get returned nil")
	}

	// Should return same instance
	cb2 := m.Get("test")
	if cb1 != cb2 {
		t.Error("Get should return same instance")
	}

	// Different name should return different instance
	cb3 := m.Get("other")
	if cb1 == cb3 {
		t.Error("Get with different name should return different instance")
	}

	// AllMetrics
	metrics := m.AllMetrics()
	if len(metrics) != 2 {
		t.Errorf("expected 2 metrics, got %d", len(metrics))
	}

	// Remove
	m.Remove("test")
	metrics = m.AllMetrics()
	if len(metrics) != 1 {
		t.Errorf("expected 1 metric after remove, got %d", len(metrics))
	}
}

func TestManager_GetWithConfig(t *testing.T) {
	m := NewManager()

	config := Config{
		MaxFailures: 10,
		Timeout:     time.Minute,
	}

	cb := m.Get("custom", config)
	if cb == nil {
		t.Fatal("Get with config returned nil")
	}

	// Verify config was applied by checking behavior
	for i := 0; i < 9; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateClosed {
		t.Error("circuit should still be closed after 9 failures with max=10")
	}

	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Error("circuit should be open after 10 failures")
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := NewDefault()

	// Run concurrent operations
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			if cb.Allow() {
				if cb.State() == StateHalfOpen {
					// Simulate work
					time.Sleep(time.Millisecond)
				}
				cb.RecordSuccess()
			}
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	// Should still be healthy
	if cb.State() != StateClosed {
		t.Errorf("expected closed after concurrent successes, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := New(Config{
		MaxFailures:      2,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 3,
		FailureThreshold: 1,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}

	// Wait for half-open
	time.Sleep(100 * time.Millisecond)

	// Transition to half-open
	if !cb.Allow() {
		t.Fatal("should allow first request in half-open")
	}

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state half-open, got %s", cb.State())
	}

	// Record failure in half-open - should transition back to open
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("expected state open after failure in half-open, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenRequestLimit(t *testing.T) {
	cb := New(Config{
		MaxFailures:      2,
		Timeout:          50 * time.Millisecond,
		SuccessThreshold: 2,
		FailureThreshold: 1,
	})

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Fatal("expected open state")
	}

	// Wait for half-open transition
	time.Sleep(100 * time.Millisecond)

	// First Allow(): Open -> HalfOpen (sets halfOpenReq=0, returns true, does NOT increment)
	allowed1 := cb.Allow()
	t.Logf("First Allow: %v (Open->HalfOpen, halfOpenReq=0)", allowed1)

	// Second Allow(): HalfOpen, halfOpenReq 0<2 → increments to 1, returns true
	allowed2 := cb.Allow()
	t.Logf("Second Allow: %v (halfOpenReq=1)", allowed2)

	// Third Allow(): HalfOpen, halfOpenReq 1<2 → increments to 2, returns true
	allowed3 := cb.Allow()
	t.Logf("Third Allow: %v (halfOpenReq=2)", allowed3)

	// Fourth Allow(): HalfOpen, halfOpenReq 2<2 is false → returns false
	allowed4 := cb.Allow()
	t.Logf("Fourth Allow: %v (should be false)", allowed4)

	if allowed4 {
		t.Error("should reject fourth request when half-open limit reached")
	}
}

func TestCircuitBreaker_RecordFailureClosed(t *testing.T) {
	cb := New(Config{
		MaxFailures: 5,
		Timeout:     time.Minute,
	})

	// Record failures up to but not including max
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
		if cb.State() != StateClosed {
			t.Errorf("should still be closed after %d failures", i+1)
		}
	}

	// 5th failure should open
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Error("should be open after max failures")
	}
}

func TestCircuitBreaker_RecordFailureInOpenState(t *testing.T) {
	cb := New(Config{
		MaxFailures: 2,
		Timeout:     50 * time.Millisecond,
	})

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Fatal("expected open state")
	}

	// RecordFailure in Open state - should be no-op (no state change)
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Error("RecordFailure in Open should not change state")
	}
}
