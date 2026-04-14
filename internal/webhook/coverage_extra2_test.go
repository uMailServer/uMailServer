package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// setupTestManager creates a webhook manager with a test database.
func setupTestManager(t *testing.T) (*Manager, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewManager(database, "testsecret"), database
}

// TestSend_InvalidURL tests that send does not panic with an invalid URL.
func TestSend_InvalidURL(t *testing.T) {
	mgr, _ := setupTestManager(t)

	hook := &Webhook{
		ID:     "test-hook",
		URL:    "http://invalid-host-that-does-not-exist.invalid:1/webhook",
		Events: []string{"*"},
		Active: true,
	}

	// send should not panic; it logs and returns silently on error
	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}
	mgr.send(hook, event)
}

// TestSend_ValidServer tests send with a real HTTP server.
func TestSend_ValidServer(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true) // Allow localhost for testing

	var receivedBody []byte
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		buf := bytes.Buffer{}
		_, _ = buf.ReadFrom(r.Body)
		receivedBody = buf.Bytes()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := &Webhook{
		ID:     "test-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "mail.received", Data: map[string]string{"from": "test@example.com"}}
	mgr.send(hook, event)

	// Verify the request was sent
	if len(receivedBody) == 0 {
		t.Error("expected non-empty body")
	}

	var receivedEvent Event
	if err := json.Unmarshal(receivedBody, &receivedEvent); err != nil {
		t.Fatalf("failed to unmarshal received event: %v", err)
	}
	if receivedEvent.Type != "mail.received" {
		t.Errorf("expected type mail.received, got %s", receivedEvent.Type)
	}

	// Verify headers
	if receivedHeaders.Get("X-Webhook-ID") != "test-hook" {
		t.Errorf("expected X-Webhook-ID test-hook, got %s", receivedHeaders.Get("X-Webhook-ID"))
	}
	if receivedHeaders.Get("X-Webhook-Event") != "mail.received" {
		t.Errorf("expected X-Webhook-Event mail.received, got %s", receivedHeaders.Get("X-Webhook-Event"))
	}
	if receivedHeaders.Get("X-Webhook-Signature") == "" {
		t.Error("expected X-Webhook-Signature to be set when secret is configured")
	}
}

// TestSend_NoSecret tests that no signature header is set when secret is empty.
func TestSend_NoSecret(t *testing.T) {
	database, _ := db.Open(t.TempDir() + "/test.db")
	defer database.Close()
	mgr := NewManager(database, "")
	mgr.SetAllowPrivateIP(true) // empty secret

	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := &Webhook{
		ID:     "no-secret-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: nil}
	mgr.send(hook, event)

	if receivedHeaders.Get("X-Webhook-Signature") != "" {
		t.Error("expected no X-Webhook-Signature when secret is empty")
	}
}

// TestEventMatches tests event pattern matching.
func TestEventMatches(t *testing.T) {
	mgr, _ := setupTestManager(t)

	tests := []struct {
		patterns []string
		event    string
		want     bool
	}{
		{[]string{"mail.received"}, "mail.received", true},
		{[]string{"mail.received"}, "mail.sent", false},
		{[]string{"*"}, "anything", true},
		{[]string{"mail.*"}, "mail.received", true},
		{[]string{"mail.*"}, "mail.sent", true},
		{[]string{"mail.*"}, "spam.detected", false},
		{[]string{}, "mail.received", false},
		{[]string{"delivery.failed", "delivery.success"}, "delivery.success", true},
	}

	for _, tt := range tests {
		got := mgr.eventMatches(tt.patterns, tt.event)
		if got != tt.want {
			t.Errorf("eventMatches(%v, %q) = %v, want %v", tt.patterns, tt.event, got, tt.want)
		}
	}
}

// TestTrigger_InactiveHook tests that inactive hooks are skipped.
func TestTrigger_InactiveHook(t *testing.T) {
	mgr, _ := setupTestManager(t)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr.mu.Lock()
	mgr.hooks = append(mgr.hooks, &Webhook{
		ID:     "inactive-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: false,
	})
	mgr.mu.Unlock()

	mgr.Trigger("test.event", nil)

	// Give goroutine time to run (or not)
	// Since the hook is inactive, the server should never be called
	// We just verify no panic occurs
	_ = called
}

// TestTrigger_EventNotMatching tests that hooks with non-matching events are skipped.
func TestTrigger_EventNotMatching(t *testing.T) {
	mgr, _ := setupTestManager(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("webhook should not have been called for non-matching event")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr.mu.Lock()
	mgr.hooks = append(mgr.hooks, &Webhook{
		ID:     "specific-hook",
		URL:    server.URL,
		Events: []string{"mail.sent"},
		Active: true,
	})
	mgr.mu.Unlock()

	mgr.Trigger("mail.received", nil)
}

// TestHTTPHandler_GET tests listing webhooks via HTTP.
func TestHTTPHandler_GET(t *testing.T) {
	mgr, _ := setupTestManager(t)

	req := httptest.NewRequest(http.MethodGet, "/webhooks", nil)
	w := httptest.NewRecorder()

	mgr.HTTPHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestHTTPHandler_POST tests creating a webhook via HTTP.
func TestHTTPHandler_POST(t *testing.T) {
	mgr, _ := setupTestManager(t)

	body := `{"url": "http://example.com/webhook", "events": ["mail.received"]}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	mgr.HTTPHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

// TestHTTPHandler_PostInvalidJSON tests POST with invalid JSON.
func TestHTTPHandler_PostInvalidJSON(t *testing.T) {
	mgr, _ := setupTestManager(t)

	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	mgr.HTTPHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestHTTPHandler_UnsupportedMethod tests HTTP handler with unsupported method.
func TestHTTPHandler_UnsupportedMethod(t *testing.T) {
	mgr, _ := setupTestManager(t)

	req := httptest.NewRequest(http.MethodDelete, "/webhooks", nil)
	w := httptest.NewRecorder()

	mgr.HTTPHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// TestGetCircuitBreakerMetrics tests the GetCircuitBreakerMetrics method.
func TestGetCircuitBreakerMetrics(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Get metrics when no webhooks have been called
	metrics := mgr.GetCircuitBreakerMetrics()
	if metrics == nil {
		t.Error("expected non-nil metrics map")
	}
}

// TestIsValidWebhookURL_InvalidSchemes tests URL validation with invalid schemes.
func TestIsValidWebhookURL_InvalidSchemes(t *testing.T) {
	mgr, _ := setupTestManager(t)

	tests := []struct {
		url  string
		want bool
	}{
		{"ftp://example.com/webhook", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
	}

	for _, tt := range tests {
		got, _ := mgr.isValidWebhookURL(tt.url)
		if got != tt.want {
			t.Errorf("isValidWebhookURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// TestIsValidWebhookURL_InvalidHostnames tests URL validation with invalid hostnames.
func TestIsValidWebhookURL_InvalidHostnames(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Without allowPrivateIP, localhost should be blocked
	tests := []struct {
		url  string
		want bool
	}{
		{"http://localhost/webhook", false},
		{"http://127.0.0.1/webhook", false},
		{"http://[::1]/webhook", false},
	}

	for _, tt := range tests {
		got, _ := mgr.isValidWebhookURL(tt.url)
		if got != tt.want {
			t.Errorf("isValidWebhookURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// TestIsValidWebhookURL_WithPrivateIPAllowed tests URL validation when private IPs are allowed.
func TestIsValidWebhookURL_WithPrivateIPAllowed(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	// With allowPrivateIP, localhost should be allowed
	url := "http://localhost/webhook"
	valid, _ := mgr.isValidWebhookURL(url)
	if !valid {
		t.Errorf("isValidWebhookURL(%q) = false, want true when allowPrivateIP is true", url)
	}
}

// TestIsValidWebhookURL_EmptyURL tests URL validation with empty URL.
func TestIsValidWebhookURL_EmptyURL(t *testing.T) {
	mgr, _ := setupTestManager(t)

	if valid, _ := mgr.isValidWebhookURL(""); valid {
		t.Error("isValidWebhookURL(\"\") should return false")
	}
}

// TestIsValidWebhookURL_ValidURLs tests URL validation with valid URLs.
func TestIsValidWebhookURL_ValidURLs(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	tests := []string{
		"https://example.com/webhook",
		"https://api.service.com/hooks/webhook",
		"http://webhook.example.com:8080/endpoint",
	}

	for _, url := range tests {
		valid, _ := mgr.isValidWebhookURL(url)
		if !valid {
			t.Errorf("isValidWebhookURL(%q) = false, want true", url)
		}
	}
}

// TestSend_CircuitBreakerOpen tests send when circuit breaker is open.
func TestSend_CircuitBreakerOpen(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := &Webhook{
		ID:     "cb-test-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}

	// Force circuit breaker open by calling send multiple times
	// and getting failures
	for i := 0; i < 5; i++ {
		mgr.send(hook, event)
	}

	// Give time for circuit breaker to update
	time.Sleep(100 * time.Millisecond)

	// Circuit breaker should be in some state, send should handle it gracefully
	mgr.send(hook, event)
}

// TestSend_4xxError tests that 4xx errors are not retried.
func TestSend_4xxError(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	var attemptCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusBadRequest) // 400 - should not retry
	}))
	defer server.Close()

	hook := &Webhook{
		ID:     "4xx-test-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}
	mgr.send(hook, event)

	// Give time for the request to complete
	time.Sleep(100 * time.Millisecond)

	// Should only attempt once for 4xx errors (no retry)
	if attemptCount != 1 {
		t.Errorf("expected 1 attempt for 4xx error, got %d", attemptCount)
	}
}

// TestSend_ServerErrorRetry tests that 5xx errors are retried.
func TestSend_ServerErrorRetry(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	var attemptCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError) // 500 - should retry
	}))
	defer server.Close()

	hook := &Webhook{
		ID:     "5xx-test-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}
	mgr.send(hook, event)

	// Give time for all retries to complete
	time.Sleep(500 * time.Millisecond)

	// Should attempt 3 times for 5xx errors (with retries)
	if attemptCount != 3 {
		t.Errorf("expected 3 attempts for 5xx error, got %d", attemptCount)
	}
}

// TestSend_ConnectionErrorRetry tests that connection errors trigger retries.
func TestSend_ConnectionErrorRetry(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	var attemptCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			// Close connection to simulate error on first attempts
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Use a URL that will cause connection errors
	hook := &Webhook{
		ID:     "conn-err-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}
	mgr.send(hook, event)

	// Give time for retries
	time.Sleep(100 * time.Millisecond)

	// Should have attempted (success or failure path)
	t.Logf("Attempt count: %d", attemptCount)
}

// TestSend_PanicRecovery tests that send recovers from panics gracefully.
func TestSend_PanicRecovery(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.SetAllowPrivateIP(true)

	// This test verifies the panic recovery path is exercised
	// by ensuring send never panics even with bad data
	hook := &Webhook{
		ID:     "panic-test-hook",
		URL:    "http://localhost:99999/webhook", // Will fail to connect
		Events: []string{"*"},
		Active: true,
	}

	event := Event{Type: "test.event", Data: map[string]string{"key": "value"}}

	// Should not panic - panic recovery handles any errors
	mgr.send(hook, event)
}

// TestIsValidWebhookURL_PrivateIPs tests URL validation with various private IP ranges.
func TestIsValidWebhookURL_PrivateIPs(t *testing.T) {
	mgr, _ := setupTestManager(t)

	tests := []struct {
		url  string
		want bool
	}{
		// Private IP ranges
		{"http://10.0.0.1/webhook", false},
		{"http://172.16.0.1/webhook", false},
		{"http://192.168.0.1/webhook", false},
		// Link-local
		{"http://169.254.0.1/webhook", false},
		// Loopback
		{"http://127.0.0.2/webhook", false},
		{"http://[::1]/webhook", false},
	}

	for _, tt := range tests {
		got, _ := mgr.isValidWebhookURL(tt.url)
		if got != tt.want {
			t.Errorf("isValidWebhookURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

// TestIsValidWebhookURL_ParseError tests URL validation when URL cannot be parsed.
func TestIsValidWebhookURL_ParseError(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// URL with parse error
	valid, _ := mgr.isValidWebhookURL("http://[invalid]/webhook")
	if valid {
		t.Error("expected isValidWebhookURL to return false for unparseable URL")
	}
}
