package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Global test metrics instance (expvar vars are global)
var testMetrics *Metrics

func init() {
	testMetrics = New()
}

func TestMetrics(t *testing.T) {
	t.Run("SMTPMetrics", func(t *testing.T) {
		testMetrics.SMTPConnected()
		if testMetrics.SMTPActiveConnections.Value() != 1 {
			t.Errorf("expected 1 active connection, got %d", testMetrics.SMTPActiveConnections.Value())
		}

		testMetrics.SMTPReceived()
		if testMetrics.SMTPEmailsReceived.Value() < 1 {
			t.Errorf("expected at least 1 received email, got %d", testMetrics.SMTPEmailsReceived.Value())
		}

		testMetrics.SMTPSent()
		if testMetrics.SMTPEmailsSent.Value() < 1 {
			t.Errorf("expected at least 1 sent email, got %d", testMetrics.SMTPEmailsSent.Value())
		}

		testMetrics.SMTPRejected()
		if testMetrics.SMTPEmailsRejected.Value() < 1 {
			t.Errorf("expected at least 1 rejected email, got %d", testMetrics.SMTPEmailsRejected.Value())
		}

		testMetrics.SMTPDisconnected()
		if testMetrics.SMTPActiveConnections.Value() != 0 {
			t.Errorf("expected 0 active connections, got %d", testMetrics.SMTPActiveConnections.Value())
		}
	})

	t.Run("IMAPMetrics", func(t *testing.T) {
		testMetrics.IMAPConnected()
		if testMetrics.IMAPActiveConnections.Value() != 1 {
			t.Errorf("expected 1 active connection, got %d", testMetrics.IMAPActiveConnections.Value())
		}

		testMetrics.IMAPMessageFetched()
		if testMetrics.IMAPMessagesFetched.Value() < 1 {
			t.Errorf("expected at least 1 fetched message, got %d", testMetrics.IMAPMessagesFetched.Value())
		}

		testMetrics.IMAPMessageStored()
		if testMetrics.IMAPMessagesStored.Value() < 1 {
			t.Errorf("expected at least 1 stored message, got %d", testMetrics.IMAPMessagesStored.Value())
		}

		testMetrics.IMAPDisconnected()
		if testMetrics.IMAPActiveConnections.Value() != 0 {
			t.Errorf("expected 0 active connections, got %d", testMetrics.IMAPActiveConnections.Value())
		}
	})

	t.Run("HTTPMetrics", func(t *testing.T) {
		testMetrics.HTTPRequestStarted()
		if testMetrics.HTTPRequestsActive.Value() != 1 {
			t.Errorf("expected 1 active request, got %d", testMetrics.HTTPRequestsActive.Value())
		}

		testMetrics.HTTPRequestFinished(100 * time.Millisecond)
		if testMetrics.HTTPRequestsActive.Value() != 0 {
			t.Errorf("expected 0 active requests, got %d", testMetrics.HTTPRequestsActive.Value())
		}
	})

	t.Run("QueueMetrics", func(t *testing.T) {
		// Record initial values
		initialDelivered := testMetrics.QueueMessagesDelivered.Value()
		initialFailed := testMetrics.QueueMessagesFailed.Value()
		initialBounced := testMetrics.QueueMessagesBounced.Value()

		testMetrics.QueueEnqueued()
		if testMetrics.QueueSize.Value() < 1 {
			t.Errorf("expected queue size >= 1, got %d", testMetrics.QueueSize.Value())
		}

		testMetrics.QueueDelivered()
		if testMetrics.QueueMessagesDelivered.Value() != initialDelivered+1 {
			t.Errorf("expected %d delivered messages, got %d", initialDelivered+1, testMetrics.QueueMessagesDelivered.Value())
		}

		testMetrics.QueueEnqueued()
		testMetrics.QueueFailed()
		if testMetrics.QueueMessagesFailed.Value() != initialFailed+1 {
			t.Errorf("expected %d failed messages, got %d", initialFailed+1, testMetrics.QueueMessagesFailed.Value())
		}

		testMetrics.QueueEnqueued()
		testMetrics.QueueBounced()
		if testMetrics.QueueMessagesBounced.Value() != initialBounced+1 {
			t.Errorf("expected %d bounced messages, got %d", initialBounced+1, testMetrics.QueueMessagesBounced.Value())
		}
	})

	t.Run("SpamMetrics", func(t *testing.T) {
		initialScanned := testMetrics.SpamMessagesScanned.Value()
		initialBlocked := testMetrics.SpamMessagesBlocked.Value()
		initialJunk := testMetrics.SpamMessagesJunk.Value()

		testMetrics.SpamScanned()
		if testMetrics.SpamMessagesScanned.Value() != initialScanned+1 {
			t.Errorf("expected %d scanned messages, got %d", initialScanned+1, testMetrics.SpamMessagesScanned.Value())
		}

		testMetrics.SpamBlocked()
		if testMetrics.SpamMessagesBlocked.Value() != initialBlocked+1 {
			t.Errorf("expected %d blocked messages, got %d", initialBlocked+1, testMetrics.SpamMessagesBlocked.Value())
		}

		testMetrics.SpamJunk()
		if testMetrics.SpamMessagesJunk.Value() != initialJunk+1 {
			t.Errorf("expected %d junk messages, got %d", initialJunk+1, testMetrics.SpamMessagesJunk.Value())
		}
	})

	t.Run("StorageMetrics", func(t *testing.T) {
		testMetrics.UpdateStorage(1024*1024, 10, 2)
		if testMetrics.StorageBytesUsed.Value() != 1024*1024 {
			t.Errorf("expected %d bytes used, got %d", 1024*1024, testMetrics.StorageBytesUsed.Value())
		}
		if testMetrics.StorageAccounts.Value() != 10 {
			t.Errorf("expected 10 accounts, got %d", testMetrics.StorageAccounts.Value())
		}
		if testMetrics.StorageDomains.Value() != 2 {
			t.Errorf("expected 2 domains, got %d", testMetrics.StorageDomains.Value())
		}
	})

	t.Run("Uptime", func(t *testing.T) {
		uptime := testMetrics.Uptime()
		if uptime < 0 {
			t.Error("expected non-negative uptime")
		}
	})
}

func TestPrometheusHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	handler := testMetrics.PrometheusHandler()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("expected non-empty response body")
	}

	// Check for expected metrics in output
	if !contains(body, "umailserver_smtp_emails_received") {
		t.Error("expected smtp_emails_received metric in output")
	}
	if !contains(body, "umailserver_smtp_emails_sent") {
		t.Error("expected smtp_emails_sent metric in output")
	}
}

func TestHealthHandler(t *testing.T) {
	handler := HealthHandler()
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("expected non-empty response body")
	}

	if !contains(body, "healthy") {
		t.Error("expected 'healthy' status in response")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
