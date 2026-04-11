package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Tests for zero-downtime deployment draining ---

func TestStartDrain_SetsDrainingTrue(t *testing.T) {
	s := &Server{}
	s.draining.Store(false)

	stopFunc := s.StartDrain()
	if !s.draining.Load() {
		t.Error("Expected draining to be true after StartDrain")
	}

	// Cleanup
	if stopFunc != nil {
		stopFunc()
	}
}

func TestDrainWait_CompletesWhenNoActiveRequests(t *testing.T) {
	s := &Server{}
	s.draining.Store(true)

	// Should complete quickly since activeRequests returns 0
	done := make(chan struct{})
	go func() {
		s.DrainWait(1 * time.Second)
		close(done)
	}()

	select {
	case <-done:
		// Success - completed without hanging
	case <-time.After(2 * time.Second):
		t.Error("DrainWait did not complete within expected time")
	}
}

func TestHandleReady_NotDraining(t *testing.T) {
	s := &Server{}
	s.draining.Store(false)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	s.handleReady(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestHandleReady_Draining(t *testing.T) {
	s := &Server{}
	s.draining.Store(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	s.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", rec.Code)
	}
}

func TestHandleReady_DrainingWithNilDB(t *testing.T) {
	s := &Server{}
	s.draining.Store(true)
	// db field is private, so we can't set it to nil directly in test
	// This test validates the draining behavior without DB

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	s.handleReady(rec, req)

	// When draining, should return 503 even without checking DB
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 when draining, got %d", rec.Code)
	}
}

func TestDrainWait_WithTimeout(t *testing.T) {
	s := &Server{}
	s.draining.Store(true)

	// Test that DrainWait respects timeout
	// Since activeRequests() always returns 0, it should complete quickly
	start := time.Now()
	s.DrainWait(100 * time.Millisecond)
	elapsed := time.Since(start)

	// Since activeRequests returns 0, DrainWait should return immediately
	// We just verify it doesn't hang
	if elapsed > 500*time.Millisecond {
		t.Error("DrainWait took too long")
	}
}
