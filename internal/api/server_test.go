package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestParseEmail(t *testing.T) {
	tests := []struct {
		input         string
		expectedUser  string
		expectedDomain string
	}{
		{"user@example.com", "user", "example.com"},
		{"test@mail.example.org", "test", "mail.example.org"},
		{"invalid", "invalid", ""},
		{"@nodomain.com", "", "nodomain.com"},
	}

	for _, tt := range tests {
		user, domain := parseEmail(tt.input)
		if user != tt.expectedUser || domain != tt.expectedDomain {
			t.Errorf("parseEmail(%q) = %q, %q; want %q, %q",
				tt.input, user, domain, tt.expectedUser, tt.expectedDomain)
		}
	}
}

func TestDomainToJSON(t *testing.T) {
	domain := &db.DomainData{
		Name:        "example.com",
		MaxAccounts: 100,
		IsActive:    true,
	}

	result := domainToJSON(domain)

	if result["name"] != "example.com" {
		t.Errorf("Expected name example.com, got %v", result["name"])
	}
	if result["max_accounts"] != 100 {
		t.Errorf("Expected max_accounts 100, got %v", result["max_accounts"])
	}
	if result["is_active"] != true {
		t.Errorf("Expected is_active true, got %v", result["is_active"])
	}
}

func TestAccountToJSON(t *testing.T) {
	account := &db.AccountData{
		Email:     "user@example.com",
		IsAdmin:   true,
		IsActive:  true,
		QuotaUsed: 1024,
	}

	result := accountToJSON(account)

	if result["email"] != "user@example.com" {
		t.Errorf("Expected email user@example.com, got %v", result["email"])
	}
	if result["is_admin"] != true {
		t.Errorf("Expected is_admin true, got %v", result["is_admin"])
	}
	if result["quota_used"] != int64(1024) {
		t.Errorf("Expected quota_used 1024, got %v", result["quota_used"])
	}
}

func TestCORSMiddleware(t *testing.T) {
	// This test verifies the CORS middleware is set up correctly
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create a test server with CORS
	server := &Server{}
	wrapped := server.corsMiddleware(handler)

	// Test preflight request
	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", rec.Code)
	}

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected CORS header Access-Control-Allow-Origin: *")
	}
}

func TestSendJSON(t *testing.T) {
	server := &Server{}
	rec := httptest.NewRecorder()
	data := map[string]string{"message": "test"}

	server.sendJSON(rec, http.StatusOK, data)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", rec.Header().Get("Content-Type"))
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Errorf("Failed to decode JSON response: %v", err)
	}

	if result["message"] != "test" {
		t.Errorf("Expected message 'test', got %s", result["message"])
	}
}

func TestSendError(t *testing.T) {
	server := &Server{}
	rec := httptest.NewRecorder()

	server.sendError(rec, http.StatusBadRequest, "bad request")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Errorf("Failed to decode JSON response: %v", err)
	}

	if result["error"] != "bad request" {
		t.Errorf("Expected error 'bad request', got %s", result["error"])
	}
}

func TestServerConfig(t *testing.T) {
	cfg := Config{
		Addr:        ":8080",
		JWTSecret:   "test-secret",
		TokenExpiry: time.Hour,
	}

	if cfg.Addr != ":8080" {
		t.Errorf("Expected addr :8080, got %s", cfg.Addr)
	}

	if cfg.TokenExpiry != time.Hour {
		t.Errorf("Expected token expiry 1 hour, got %v", cfg.TokenExpiry)
	}
}

func TestServerDefaultTokenExpiry(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	if server.config.TokenExpiry != 24*time.Hour {
		t.Errorf("Expected default token expiry 24h, got %v", server.config.TokenExpiry)
	}
}

func TestHandleHealth(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Errorf("Failed to decode JSON: %v", err)
	}

	if result["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %s", result["status"])
	}
}

func TestHandleLoginMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()

	server.handleLogin(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleLoginInvalidBody(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("invalid")))
	rec := httptest.NewRecorder()

	server.handleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleDomainsMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/domains", nil)
	rec := httptest.NewRecorder()

	server.handleDomains(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleAccountsMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/accounts", nil)
	rec := httptest.NewRecorder()

	server.handleAccounts(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleQueueMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/queue", nil)
	rec := httptest.NewRecorder()

	server.handleQueue(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleMetricsMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()

	server.handleMetrics(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}

func TestHandleStatsMethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()

	server.handleStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rec.Code)
	}
}
