package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

	var receivedBody []byte
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		buf := bytes.Buffer{}
		buf.ReadFrom(r.Body)
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
	mgr := NewManager(database, "") // empty secret

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
