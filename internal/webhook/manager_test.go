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

func TestWebhookManager(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	t.Run("CreateWebhook", func(t *testing.T) {
		req := struct {
			URL    string   `json:"url"`
			Events []string `json:"events"`
		}{
			URL:    "https://example.com/webhook",
			Events: []string{"mail.received", "mail.sent"},
		}
		body, _ := json.Marshal(req)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(manager.HTTPHandler)
		handler.ServeHTTP(rr, httptest.NewRequest("POST", "/webhooks", strings.NewReader(string(body))))

		if rr.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d", rr.Code)
		}

		var webhook Webhook
		_ = json.Unmarshal(rr.Body.Bytes(), &webhook)

		if webhook.URL != req.URL {
			t.Errorf("Expected URL %s, got %s", req.URL, webhook.URL)
		}

		if len(webhook.Events) != 2 {
			t.Errorf("Expected 2 events, got %d", len(webhook.Events))
		}
	})

	t.Run("ListWebhooks", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(manager.HTTPHandler)
		handler.ServeHTTP(rr, httptest.NewRequest("GET", "/webhooks", nil))

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		var result map[string]interface{}
		_ = json.Unmarshal(rr.Body.Bytes(), &result)

		if _, ok := result["webhooks"]; !ok {
			t.Error("Expected webhooks in response")
		}
	})

	t.Run("EventMatches", func(t *testing.T) {
		tests := []struct {
			patterns []string
			event    string
			want     bool
		}{
			{[]string{"mail.received"}, "mail.received", true},
			{[]string{"mail.sent"}, "mail.received", false},
			{[]string{"*"}, "anything", true},
			{[]string{"mail.*"}, "mail.received", true},
			{[]string{"mail.*"}, "auth.login", false},
		}

		for _, tt := range tests {
			got := manager.eventMatches(tt.patterns, tt.event)
			if got != tt.want {
				t.Errorf("eventMatches(%v, %s) = %v, want %v", tt.patterns, tt.event, got, tt.want)
			}
		}
	})

	t.Run("SignPayload", func(t *testing.T) {
		payload := []byte("test payload")
		sig1 := manager.sign(payload)
		sig2 := manager.sign(payload)

		if sig1 != sig2 {
			t.Error("Signature should be deterministic")
		}

		if len(sig1) == 0 {
			t.Error("Signature should not be empty")
		}
	})
}

func TestNewManager(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "secret")

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
	if manager.db != database {
		t.Error("expected database to be set")
	}
	if manager.secret != "secret" {
		t.Error("expected secret to be set")
	}
	if manager.client == nil {
		t.Error("expected http client to be initialized")
	}
	if manager.hooks == nil {
		t.Error("expected hooks slice to be initialized")
	}
}

func TestEventTypes(t *testing.T) {
	events := []string{
		EventMailReceived,
		EventMailSent,
		EventDeliveryFailed,
		EventDeliverySuccess,
		EventSpamDetected,
		EventLoginSuccess,
		EventLoginFailed,
	}

	for _, event := range events {
		if event == "" {
			t.Error("event type should not be empty")
		}
	}
}

func TestEventStruct(t *testing.T) {
	now := time.Now()
	event := Event{
		Type:      EventMailReceived,
		Timestamp: now,
		Data:      map[string]string{"from": "test@example.com"},
	}

	if event.Type != EventMailReceived {
		t.Errorf("expected type %s, got %s", EventMailReceived, event.Type)
	}
	if event.Timestamp != now {
		t.Error("expected timestamp to match")
	}
}

func TestWebhookStruct(t *testing.T) {
	now := time.Now()
	webhook := Webhook{
		ID:        "webhook-1",
		URL:       "https://example.com/webhook",
		Events:    []string{"mail.received"},
		Active:    true,
		CreatedAt: now,
	}

	if webhook.ID != "webhook-1" {
		t.Errorf("expected id webhook-1, got %s", webhook.ID)
	}
	if webhook.URL != "https://example.com/webhook" {
		t.Errorf("expected url, got %s", webhook.URL)
	}
	if !webhook.Active {
		t.Error("expected active to be true")
	}
}

func TestEventMatchesEmpty(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "")
	manager.SetAllowPrivateIP(true)

	// Empty patterns should not match
	result := manager.eventMatches([]string{}, "event")
	if result {
		t.Error("empty patterns should not match")
	}
}

func TestEventMatchesWildcard(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "")
	manager.SetAllowPrivateIP(true)

	// Test wildcard matching
	if !manager.eventMatches([]string{"*"}, "any.event") {
		t.Error("* should match any event")
	}
	if !manager.eventMatches([]string{"mail.*"}, "mail.received") {
		t.Error("mail.* should match mail.received")
	}
	if manager.eventMatches([]string{"mail.*"}, "auth.login") {
		t.Error("mail.* should not match auth.login")
	}
}

func TestTrigger(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	// Create a test webhook
	testHook := &Webhook{
		ID:        "test-hook-1",
		URL:       "http://localhost:9999/webhook",
		Events:    []string{"mail.received"},
		Active:    true,
		CreatedAt: time.Now(),
	}

	manager.hooks = append(manager.hooks, testHook)

	// Test triggering an event
	manager.Trigger("mail.received", map[string]string{"from": "test@example.com"})

	// Give it a moment to process
	time.Sleep(10 * time.Millisecond)

	// Test triggering an event that doesn't match
	manager.Trigger("mail.sent", map[string]string{"to": "other@example.com"})

	// Test triggering when webhook is inactive
	testHook.Active = false
	manager.Trigger("mail.received", map[string]string{"from": "test@example.com"})
}

func TestSend(t *testing.T) {
	// Create a test server
	var receivedRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequest = true

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != "uMailServer-Webhook/1.0" {
			t.Errorf("Expected User-Agent uMailServer-Webhook/1.0, got %s", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("X-Webhook-ID") != "test-hook" {
			t.Errorf("Expected X-Webhook-ID test-hook, got %s", r.Header.Get("X-Webhook-ID"))
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	hook := &Webhook{
		ID:     "test-hook",
		URL:    server.URL,
		Events: []string{"mail.received"},
		Active: true,
	}

	event := Event{
		Type:      "mail.received",
		Timestamp: time.Now(),
		Data:      map[string]string{"from": "test@example.com"},
	}

	// Call send directly
	manager.send(hook, event)

	// Wait for request
	time.Sleep(100 * time.Millisecond)

	if !receivedRequest {
		t.Error("Expected webhook request to be received")
	}
}

func TestSendWithSignature(t *testing.T) {
	// Create a test server that verifies signature
	var receivedSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-Webhook-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database := &db.DB{}
	manager := NewManager(database, "test-secret")
	manager.SetAllowPrivateIP(true)

	hook := &Webhook{
		ID:     "test-hook",
		URL:    server.URL,
		Events: []string{"mail.received"},
		Active: true,
	}

	event := Event{
		Type:      "mail.received",
		Timestamp: time.Now(),
		Data:      map[string]string{"from": "test@example.com"},
	}

	manager.send(hook, event)
	time.Sleep(100 * time.Millisecond)

	if receivedSignature == "" {
		t.Error("Expected signature header to be set")
	}
}

func TestSendInvalidURL(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "")
	manager.SetAllowPrivateIP(true)

	hook := &Webhook{
		ID:     "test-hook",
		URL:    "http://invalid-url-that-does-not-exist.example:99999",
		Events: []string{"mail.received"},
		Active: true,
	}

	event := Event{
		Type:      "mail.received",
		Timestamp: time.Now(),
		Data:      map[string]string{"from": "test@example.com"},
	}

	// Should not panic even with invalid URL
	manager.send(hook, event)
}
