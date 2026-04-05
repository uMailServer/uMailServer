package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// TestHTTPHandlerMethodNotAllowed tests the HTTPHandler with unsupported HTTP methods
// (DELETE, PUT, PATCH) which should return 405 Method Not Allowed.
func TestHTTPHandlerMethodNotAllowed(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	methods := []string{http.MethodDelete, http.MethodPut, http.MethodPatch, http.MethodOptions}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(manager.HTTPHandler)
			handler.ServeHTTP(rr, httptest.NewRequest(method, "/webhooks", nil))

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405 for method %s, got %d", method, rr.Code)
			}
		})
	}
}

// TestHandleCreateInvalidJSON tests handleCreate with invalid JSON body.
func TestHandleCreateInvalidJSON(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	// Send invalid JSON
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(manager.HTTPHandler)
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader("not json")))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid JSON, got %d", rr.Code)
	}
}

// TestHandleCreateEmptyBody tests handleCreate with empty body.
func TestHandleCreateEmptyBody(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(manager.HTTPHandler)
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader("")))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty body, got %d", rr.Code)
	}
}

// TestSendWithNoSecret tests the send function when no secret is configured,
// verifying that the X-Webhook-Signature header is NOT set.
func TestSendWithNoSecret(t *testing.T) {
	var receivedSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Webhook-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "") // empty secret

	hook := &Webhook{
		ID:     "test-hook-no-secret",
		URL:    server.URL,
		Events: []string{"mail.received"},
		Active: true,
	}

	event := Event{
		Type: "mail.received",
		Data: map[string]string{"from": "test@example.com"},
	}

	manager.send(hook, event)

	// Wait for request
	time.Sleep(100 * time.Millisecond)

	if receivedSignature != "" {
		t.Error("expected no signature header when secret is empty")
	}
}

// TestSendHTTPError tests the send function when the server returns a non-200 status.
func TestSendHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	hook := &Webhook{
		ID:     "test-hook-error",
		URL:    server.URL,
		Events: []string{"mail.received"},
		Active: true,
	}

	event := Event{
		Type: "mail.received",
		Data: map[string]string{"from": "test@example.com"},
	}

	// Should not panic even with 500 response
	manager.send(hook, event)
	time.Sleep(50 * time.Millisecond)
}

// TestTriggerInactiveWebhook tests that inactive webhooks are skipped during Trigger.
func TestTriggerInactiveWebhook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inactive webhook should not receive request")
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	// Add an inactive webhook
	hook := &Webhook{
		ID:     "inactive-hook",
		URL:    server.URL,
		Events: []string{"*"},
		Active: false,
	}
	manager.hooks = append(manager.hooks, hook)

	manager.Trigger("mail.received", map[string]string{"from": "test@example.com"})
	time.Sleep(50 * time.Millisecond)
}

// TestTriggerEventNotMatching tests that webhooks whose events don't match are skipped.
func TestTriggerEventNotMatching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("non-matching webhook should not receive request")
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	hook := &Webhook{
		ID:     "mismatch-hook",
		URL:    server.URL,
		Events: []string{"mail.sent"},
		Active: true,
	}
	manager.hooks = append(manager.hooks, hook)

	manager.Trigger("mail.received", map[string]string{"from": "test@example.com"})
	time.Sleep(50 * time.Millisecond)
}

// TestHandleCreateValidThenList tests creating a webhook then listing it.
func TestHandleCreateValidThenList(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	// Create a webhook
	req := struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}{
		URL:    "https://example.com/new-hook",
		Events: []string{"delivery.failed"},
	}
	body, _ := json.Marshal(req)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(manager.HTTPHandler)
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(string(body))))

	if rr.Code != http.StatusCreated {
		t.Fatalf("Expected status 201, got %d", rr.Code)
	}

	// List webhooks
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/webhooks", nil))

	if rr2.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr2.Code)
	}

	var result map[string]interface{}
	json.Unmarshal(rr2.Body.Bytes(), &result)
	webhooks, ok := result["webhooks"].([]interface{})
	if !ok || len(webhooks) != 1 {
		t.Fatalf("Expected 1 webhook in list, got %v", result)
	}
}
