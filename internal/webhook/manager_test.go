package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func TestWebhookManager(t *testing.T) {
	database := &db.DB{}
	manager := NewManager(database, "test-secret")

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
		json.Unmarshal(rr.Body.Bytes(), &webhook)

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
		json.Unmarshal(rr.Body.Bytes(), &result)

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
