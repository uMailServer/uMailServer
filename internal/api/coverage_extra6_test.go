package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// --- Tests for zero-downtime deployment draining ---

func TestStartDrain_SetsDrainingTrue(t *testing.T) {
	s := &Server{}
	s.StartDrain()
	if !s.draining.Load() {
		t.Error("Expected draining to be true after StartDrain")
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

func TestHandleReady_DatabaseError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	server := NewServer(database, nil, Config{})
	server.draining.Store(false)

	// Close database to cause ListDomains to fail
	database.Close()

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	server.handleReady(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 for database error, got %d", rec.Code)
	}
}
