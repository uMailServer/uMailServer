package server

// Stop never marked the API as draining (api.Server.StartDrain had no
// caller), so /health/ready kept answering 200 while the mail listeners
// were already closing.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
)

func TestStopReportsNotReady(t *testing.T) {
	dir := t.TempDir()
	srv, err := New(&config.Config{
		Server:   config.ServerConfig{Hostname: "test.example.com", DataDir: dir},
		Database: config.DatabaseConfig{Path: dir + "/test.db"},
		Logging:  config.LoggingConfig{Level: "error"},
	})
	if err != nil {
		t.Fatal(err)
	}
	apiDB, err := db.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { apiDB.Close() })
	a := api.NewServer(apiDB, nil, api.Config{JWTSecret: "stop-ready-secret-0123456789abcdef", TokenExpiry: time.Hour})
	srv.apiServer = a

	ready := func() int {
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		return rec.Code
	}
	if code := ready(); code != http.StatusOK {
		t.Fatalf("INVALID: ready before Stop = %d", code)
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if code := ready(); code != http.StatusServiceUnavailable {
		t.Fatalf("ready after Stop = %d, want 503", code)
	}
}
