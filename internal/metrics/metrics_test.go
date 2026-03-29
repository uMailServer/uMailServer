package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet(t *testing.T) {
	m := Get()
	if m == nil {
		t.Fatal("Get() returned nil")
	}

	// Should return same instance (singleton)
	m2 := Get()
	if m != m2 {
		t.Error("Get() should return singleton instance")
	}
}

func TestSMTPMetrics(t *testing.T) {
	m := &SimpleMetrics{}

	// Test SMTPConnection
	m.SMTPConnection()
	m.SMTPConnection()
	if m.smtpConnections != 2 {
		t.Errorf("Expected 2 connections, got %d", m.smtpConnections)
	}

	// Test SMTPMessageReceived
	m.SMTPMessageReceived()
	if m.smtpMessages != 1 {
		t.Errorf("Expected 1 message, got %d", m.smtpMessages)
	}

	// Test SMTPAuthFailure
	m.SMTPAuthFailure()
	m.SMTPAuthFailure()
	m.SMTPAuthFailure()
	if m.smtpAuthFailures != 3 {
		t.Errorf("Expected 3 auth failures, got %d", m.smtpAuthFailures)
	}
}

func TestIMAPMetrics(t *testing.T) {
	m := &SimpleMetrics{}

	// Test IMAPConnection
	m.IMAPConnection()
	m.IMAPConnection()
	m.IMAPConnection()
	if m.imapConnections != 3 {
		t.Errorf("Expected 3 IMAP connections, got %d", m.imapConnections)
	}
}

func TestDeliveryMetrics(t *testing.T) {
	m := &SimpleMetrics{}

	// Test DeliverySuccess
	m.DeliverySuccess()
	m.DeliverySuccess()
	if m.deliveriesTotal != 2 {
		t.Errorf("Expected 2 deliveries, got %d", m.deliveriesTotal)
	}

	// Test DeliveryFailed
	m.DeliveryFailed()
	if m.deliveriesFailed != 1 {
		t.Errorf("Expected 1 failed delivery, got %d", m.deliveriesFailed)
	}
}

func TestSpamMetrics(t *testing.T) {
	m := &SimpleMetrics{}

	// Test SpamDetected
	m.SpamDetected()
	m.SpamDetected()
	m.SpamDetected()
	if m.spamDetected != 3 {
		t.Errorf("Expected 3 spam detected, got %d", m.spamDetected)
	}

	// Test HamDetected
	m.HamDetected()
	m.HamDetected()
	if m.hamDetected != 2 {
		t.Errorf("Expected 2 ham detected, got %d", m.hamDetected)
	}
}

func TestAPIRequest(t *testing.T) {
	m := &SimpleMetrics{}

	m.APIRequest()
	m.APIRequest()
	m.APIRequest()
	m.APIRequest()
	if m.apiRequests != 4 {
		t.Errorf("Expected 4 API requests, got %d", m.apiRequests)
	}
}

func TestGetStats(t *testing.T) {
	m := &SimpleMetrics{}

	// Record some metrics
	m.SMTPConnection()
	m.SMTPMessageReceived()
	m.SMTPAuthFailure()
	m.IMAPConnection()
	m.DeliverySuccess()
	m.DeliveryFailed()
	m.SpamDetected()
	m.HamDetected()
	m.APIRequest()

	stats := m.GetStats()

	// Check top-level keys
	expectedSections := []string{"smtp", "imap", "delivery", "spam", "api"}
	for _, key := range expectedSections {
		if _, ok := stats[key]; !ok {
			t.Errorf("GetStats() missing section: %s", key)
		}
	}

	// Check nested SMTP keys
	if smtp, ok := stats["smtp"].(map[string]uint64); ok {
		if _, ok := smtp["connections"]; !ok {
			t.Error("Missing smtp.connections")
		}
		if _, ok := smtp["messages"]; !ok {
			t.Error("Missing smtp.messages")
		}
		if _, ok := smtp["auth_failures"]; !ok {
			t.Error("Missing smtp.auth_failures")
		}
	} else {
		t.Error("smtp section has wrong type")
	}

	// Check nested IMAP keys
	if imap, ok := stats["imap"].(map[string]uint64); ok {
		if _, ok := imap["connections"]; !ok {
			t.Error("Missing imap.connections")
		}
	} else {
		t.Error("imap section has wrong type")
	}

	// Check nested delivery keys
	if delivery, ok := stats["delivery"].(map[string]uint64); ok {
		if _, ok := delivery["success"]; !ok {
			t.Error("Missing delivery.success")
		}
		if _, ok := delivery["failed"]; !ok {
			t.Error("Missing delivery.failed")
		}
	} else {
		t.Error("delivery section has wrong type")
	}

	// Check nested spam keys
	if spam, ok := stats["spam"].(map[string]uint64); ok {
		if _, ok := spam["detected"]; !ok {
			t.Error("Missing spam.detected")
		}
		if _, ok := spam["ham"]; !ok {
			t.Error("Missing spam.ham")
		}
	} else {
		t.Error("spam section has wrong type")
	}

	// Check nested API keys
	if api, ok := stats["api"].(map[string]uint64); ok {
		if _, ok := api["requests"]; !ok {
			t.Error("Missing api.requests")
		}
	} else {
		t.Error("api section has wrong type")
	}
}

func TestHTTPHandler(t *testing.T) {
	m := &SimpleMetrics{}

	// Record some metrics
	m.SMTPConnection()
	m.SMTPMessageReceived()

	// Create request
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	// Call handler
	m.HTTPHandler(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected content-type application/json, got %s", contentType)
	}

	// Check body contains metrics
	body := w.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}
