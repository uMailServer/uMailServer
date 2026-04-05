package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// Test SetAuthToken
func TestSetAuthToken(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Set auth token
	server.SetAuthToken("test-token-123")

	// Make request without token - should fail
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 without token, got %d", rr.Code)
	}

	// Make request with token - should succeed
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token-123")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 with token, got %d", rr.Code)
	}
}

// Test SetCorsOrigin
func TestSetCorsOrigin(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Set CORS origin
	server.SetCorsOrigin("https://example.com")

	// Make OPTIONS request
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", rr.Code)
	}

	// Check CORS headers
	origin := rr.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Errorf("Expected CORS origin https://example.com, got %s", origin)
	}
}

// Test SetRateLimit and checkRateLimit
func TestSetRateLimit(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Set rate limit: 2 requests per minute
	server.SetRateLimit(2)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	body, _ := json.Marshal(reqBody)

	handler := http.HandlerFunc(server.HandleHTTP)

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.RemoteAddr = "192.168.1.1:12345"
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, rr.Code)
		}
	}

	// Third request should be rate limited
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 (rate limited), got %d", rr.Code)
	}
}

// Test checkRateLimit with different IPs
func TestCheckRateLimit_DifferentIPs(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Set rate limit: 1 request per minute
	server.SetRateLimit(1)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	body, _ := json.Marshal(reqBody)

	handler := http.HandlerFunc(server.HandleHTTP)

	// First IP makes a request
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.1:12345"
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("First IP first request: Expected status 200, got %d", rr.Code)
	}

	// Second IP makes a request - should succeed (different IP)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.2:12345"
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Second IP first request: Expected status 200, got %d", rr.Code)
	}
}

// Test checkRateLimit with disabled rate limiting
func TestCheckRateLimit_Disabled(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Set rate limit to 0 (disabled)
	server.SetRateLimit(0)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	}
	body, _ := json.Marshal(reqBody)

	handler := http.HandlerFunc(server.HandleHTTP)

	// Multiple requests should all succeed when rate limiting is disabled
	for i := 0; i < 100; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.RemoteAddr = "192.168.1.1:12345"
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, rr.Code)
		}
	}
}

// Test CORS preflight handling
func TestCorsPreflight(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)
	server.SetCorsOrigin("*")

	// Make OPTIONS request
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", rr.Code)
	}

	// Check CORS headers
	if allowOrigin := rr.Header().Get("Access-Control-Allow-Origin"); allowOrigin != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *, got %s", allowOrigin)
	}

	if allowMethods := rr.Header().Get("Access-Control-Allow-Methods"); allowMethods == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

// Test HandleHTTP with invalid JSON
func TestHandleHTTP_InvalidJSON(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Send invalid JSON
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte("invalid json")))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}

// Test HandleHTTP with wrong HTTP method
func TestHandleHTTP_WrongMethod(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Send GET request instead of POST
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("GET", "/mcp", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rr.Code)
	}
}

// Test HandleHTTP with empty body
func TestHandleHTTP_EmptyBody(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Send empty body
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte{}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}
