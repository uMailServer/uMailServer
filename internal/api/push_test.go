package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// Helper function for admin context
func withIsAdmin(ctx context.Context, isAdmin bool) context.Context {
	return context.WithValue(ctx, "isAdmin", isAdmin)
}

// Helper functions for push tests
func NewTestServer(t *testing.T, tmpDir string) *Server {
	database, err := db.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})
}

func withUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, "user", user)
}

// Test handlePushVAPID
func TestHandlePushVAPID(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/push/vapid-public-key", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushVAPID(w, req)

	// Returns 503 when push service is not configured (VAPID key empty)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandlePushVAPID_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/push/vapid-public-key", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushVAPID(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlePushVAPID_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/push/vapid-public-key", nil)
	// No user in context
	w := httptest.NewRecorder()

	server.handlePushVAPID(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handlePushSubscribe
func TestHandlePushSubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]string{
		"endpoint": "https://fcm.googleapis.com/fcm/send/test",
		"p256dh":   "test-p256dh-key",
		"auth":     "test-auth-secret",
		"deviceType": "mobile",
		"os":       "Android",
		"browser":  "Chrome",
		"name":     "Test Device",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/push/subscribe", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	// Returns 201 Created on success
	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandlePushSubscribe_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/push/subscribe", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePushSubscribe_MissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]string{
		"endpoint": "https://fcm.googleapis.com/fcm/send/test",
		// Missing p256dh and auth
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/push/subscribe", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePushSubscribe_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/push/subscribe", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test handlePushUnsubscribe
func TestHandlePushUnsubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Unsubscribe uses DELETE method with query parameter
	req := httptest.NewRequest("DELETE", "/api/v1/push/unsubscribe?id=test-sub-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushUnsubscribe(w, req)

	// Returns 200 even if subscription not found (stub implementation)
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandlePushUnsubscribe_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Unsubscribe uses DELETE method - test with invalid body (no query param, invalid JSON body)
	req := httptest.NewRequest("DELETE", "/api/v1/push/unsubscribe", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushUnsubscribe(w, req)

	// Returns 400 when body is invalid and no query param provided
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlePushUnsubscribe_MissingEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Unsubscribe uses DELETE method - no query param and empty body
	req := httptest.NewRequest("DELETE", "/api/v1/push/unsubscribe", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushUnsubscribe(w, req)

	// Returns 400 when neither query param nor endpoint in body provided
	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// Test handlePushSubscriptions
func TestHandlePushSubscriptions(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/push/subscriptions", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushSubscriptions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandlePushSubscriptions_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/push/subscriptions", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushSubscriptions(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test handlePushTest
func TestHandlePushTest(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/push/test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushTest(w, req)

	// Returns 200 even though it's a stub implementation
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result["status"] != "sent" {
		t.Errorf("Expected status 'sent', got %s", result["status"])
	}
}

func TestHandlePushTest_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/push/test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushTest(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlePushTest_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/push/test", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handlePushTest(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleAdminPushStats
func TestHandleAdminPushStats(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/admin/push/stats", nil)
	req = req.WithContext(withUser(req.Context(), "admin@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), true))
	w := httptest.NewRecorder()

	server.handleAdminPushStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := result["totalSubscriptions"]; !ok {
		t.Error("Expected totalSubscriptions in response")
	}
}

func TestHandleAdminPushStats_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/admin/push/stats", nil)
	req = req.WithContext(withUser(req.Context(), "admin@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), true))
	w := httptest.NewRecorder()

	server.handleAdminPushStats(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleAdminPushStats_NotAdmin(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/admin/push/stats", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), false))
	w := httptest.NewRecorder()

	server.handleAdminPushStats(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

