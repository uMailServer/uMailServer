package circuitbreaker

import (
	"testing"
)

func TestExecute_NilFn(t *testing.T) {
	cb := New(Config{FailureThreshold: 3, SuccessThreshold: 2})
	
	// Calling Execute with nil fn should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Execute(nil) panicked: %v", r)
		}
	}()
	
	err := cb.Execute(nil)
	// Should return an error indicating nil fn was invalid
	if err == nil {
		t.Error("Execute(nil) should return an error, got nil")
	}
}
