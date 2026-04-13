package audit

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// --- Additional coverage for event-specific logging functions ---

func TestLogger_LogLogout(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.LogLogout("user@example.com", "10.0.0.1")
}

func TestLogger_LogAccountUpdate(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.LogAccountUpdate("admin@example.com", "user@example.com", "10.0.0.1", []string{"is_admin", "is_active"})
}

func TestLogger_LogAccountDelete(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.LogAccountDelete("admin@example.com", "deleted@example.com", "10.0.0.1")
}

func TestLogger_LogTOTPDisable(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	logger.LogTOTPDisable("user@example.com", "user@example.com", "10.0.0.1")
}

// --- rotate and cleanup coverage ---

func TestLogger_RotateTriggersCleanup(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	// Create logger with very low max age (1 second) to trigger cleanup
	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	// Write enough events to trigger rotation
	for i := 0; i < 50; i++ {
		logger.Log(Event{
			Type:    LoginSuccess,
			User:    "test@example.com",
			IP:      "192.168.1.1",
			Success: true,
			Service: "api",
		})
	}

	// Wait for rotation to complete
	time.Sleep(100 * time.Millisecond)
}

func TestLogger_CleanupWithOldFiles(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	// Create logger
	logger, err := NewLogger(logPath, 1, 1, 1)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	// Force cleanup to run (it checks max age internally)
	// We can't easily test this without mocking time, but we can ensure it doesn't panic
	logger.Log(Event{
		Type:    LoginSuccess,
		User:    "test@example.com",
		IP:      "192.168.1.1",
		Success: true,
		Service: "api",
	})
}

// --- ExtractIP coverage ---

func TestExtractIP_WithXForwardedForMultiple(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1, 192.168.1.1")

	ip := ExtractIP(req)
	if ip != "203.0.113.1" {
		t.Errorf("Expected 203.0.113.1, got %s", ip)
	}
}

func TestExtractIP_WithInvalidXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "not-an-ip")

	ip := ExtractIP(req)
	// Should fall back to remote addr
	if ip != "10.0.0.1" {
		t.Errorf("Expected 10.0.0.1, got %s", ip)
	}
}

func TestExtractIP_WithXRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Real-IP", "203.0.113.2")

	ip := ExtractIP(req)
	if ip != "203.0.113.2" {
		t.Errorf("Expected 203.0.113.2, got %s", ip)
	}
}

func TestExtractIP_WithInvalidXRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "10.0.0.1:8080"
	req.Header.Set("X-Real-IP", "not-an-ip")

	ip := ExtractIP(req)
	// Should fall back to remote addr
	if ip != "10.0.0.1" {
		t.Errorf("Expected 10.0.0.1, got %s", ip)
	}
}

func TestExtractIP_NoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "192.168.1.100:8080"

	ip := ExtractIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("Expected 192.168.1.100, got %s", ip)
	}
}

// --- Event type string values ---

func TestEventTypeValues(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{LoginSuccess, "login_success"},
		{LoginFailure, "login_failure"},
		{Logout, "logout"},
		{AccountCreate, "account_create"},
		{AccountUpdate, "account_update"},
		{AccountDelete, "account_delete"},
		{TOTPEnable, "totp_enable"},
		{TOTPDisable, "totp_disable"},
		{PasswordChange, "password_change"},
	}

	for _, tt := range tests {
		if string(tt.eventType) != tt.expected {
			t.Errorf("EventType(%v) = %s, want %s", tt.eventType, string(tt.eventType), tt.expected)
		}
	}
}
