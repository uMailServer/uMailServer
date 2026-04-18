package websocket

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

func TestNewSSEServer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.clients == nil {
		t.Error("expected clients map to be initialized")
	}
}

func TestSSEServerGetConnectedCount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Initially should be 0
	if server.GetConnectedCount() != 0 {
		t.Errorf("expected 0 connected clients, got %d", server.GetConnectedCount())
	}
}

func TestSSEServerGetConnectedUsers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	users := server.GetConnectedUsers()
	if users == nil {
		t.Error("expected non-nil users slice")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 connected users, got %d", len(users))
	}
}

func TestSSEServerSetAuthFunc(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	authFunc := func(token string) (string, bool, error) {
		return "test-user", false, nil
	}

	server.SetAuthFunc(authFunc)
	if server.authFunc == nil {
		t.Error("expected authFunc to be set")
	}
}

func TestSSEServerSetCorsOrigin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Initially corsOrigin should be empty
	if server.corsOrigin != "" {
		t.Errorf("expected empty corsOrigin initially, got %q", server.corsOrigin)
	}

	// Set CORS origin
	server.SetCorsOrigin("https://example.com")
	if server.corsOrigin != "https://example.com" {
		t.Errorf("expected corsOrigin to be set to %q, got %q", "https://example.com", server.corsOrigin)
	}

	// Test changing origin
	server.SetCorsOrigin("https://other.com")
	if server.corsOrigin != "https://other.com" {
		t.Errorf("expected corsOrigin to be updated to %q, got %q", "https://other.com", server.corsOrigin)
	}
}

func TestSSEServerBroadcast(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Should not panic when no clients connected
	server.Broadcast("test", map[string]string{"key": "value"})
}

func TestSSEServerSendToAdmins(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Should not panic when no admin clients connected
	server.SendToAdmins("admin_event", map[string]string{"key": "value"})
}

func TestSSEServerSendToUser(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Test sending to non-existent user
	err := server.SendToUser("nonexistent", "test_event", map[string]string{"key": "value"})
	if err == nil {
		t.Error("expected error when sending to non-existent user")
	}
}

func TestSSEServerSendEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Create a mock ResponseWriter and Flusher
	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		writer:  rec,
		flusher: rec,
		stop:    make(chan struct{}),
	}

	// Test sending event
	server.sendEvent(client, "test_event", map[string]string{"message": "hello"})

	// Verify event was written
	body := rec.Body.String()
	if !contains(body, "event: test_event") {
		t.Errorf("expected event header in output, got: %s", body)
	}
	if !contains(body, "data:") {
		t.Errorf("expected data in output, got: %s", body)
	}
}

func TestSSEServerSendEventMarshalError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		writer:  rec,
		flusher: rec,
		stop:    make(chan struct{}),
	}

	// Send data that can't be marshaled (function type)
	server.sendEvent(client, "bad_event", func() {})

	// Should not write anything due to marshal error
	if rec.Body.Len() > 0 {
		t.Error("expected no output for unmarshalable data")
	}
}

func TestSSEServerHandleNotification(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		writer:  rec,
		flusher: rec,
		stop:    make(chan struct{}),
	}

	tests := []struct {
		name         string
		notification imap.MailboxNotification
		wantEvent    string
	}{
		{
			name: "NewMessage",
			notification: imap.MailboxNotification{
				User:       "testuser",
				Type:       imap.NotificationNewMessage,
				Mailbox:    "INBOX",
				MessageUID: 123,
				SeqNum:     1,
			},
			wantEvent: "new_mail",
		},
		{
			name: "Expunge",
			notification: imap.MailboxNotification{
				User:    "testuser",
				Type:    imap.NotificationExpunge,
				Mailbox: "INBOX",
				SeqNum:  1,
			},
			wantEvent: "expunge",
		},
		{
			name: "FlagsChanged",
			notification: imap.MailboxNotification{
				User:       "testuser",
				Type:       imap.NotificationFlagsChanged,
				Mailbox:    "INBOX",
				MessageUID: 123,
				SeqNum:     1,
				Flags:      []string{"\\Seen"},
			},
			wantEvent: "flags_changed",
		},
		{
			name: "MailboxUpdate",
			notification: imap.MailboxNotification{
				User:    "testuser",
				Type:    imap.NotificationMailboxUpdate,
				Mailbox: "INBOX",
			},
			wantEvent: "folder_update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec.Body.Reset()
			server.handleNotification(client, tt.notification)

			body := rec.Body.String()
			if !contains(body, "event: "+tt.wantEvent) {
				t.Errorf("expected event %s, got: %s", tt.wantEvent, body)
			}
		})
	}
}

func TestSSEServerHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	// Development mode removed - authFunc is now required
	// Tests should set authFunc before calling Handler()
	t.Run("WithoutAuthFuncSet", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()

		server.Handler().ServeHTTP(rec, req)

		// Without authFunc configured, should return 500 Internal Server Error
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status %d (Internal Server Error), got %d", http.StatusInternalServerError, rec.Code)
		}
	})
}

func TestSSEServerHandlerWithAuthFunc(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)
	server.SetAuthFunc(func(token string) (string, bool, error) {
		if token == "valid-token" {
			return "authed-user", true, nil
		}
		return "", false, errors.New("invalid token")
	})

	t.Run("WithAuthToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		req.Header.Set("X-Auth-Token", "valid-token")
		rec := &mockResponseRecorder{
			ResponseRecorder: httptest.NewRecorder(),
		}

		go server.Handler().ServeHTTP(rec, req)
		time.Sleep(500 * time.Millisecond)

		if rec.Header().Get("Content-Type") != "text/event-stream" {
			t.Error("expected SSE headers to be set for authenticated request")
		}
	})

	t.Run("WithInvalidAuthToken", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sse?token=invalid-token", nil)
		rec := httptest.NewRecorder()

		server.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("WithHeaderToken", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		server := NewSSEServer(logger)
		server.SetAuthFunc(func(token string) (string, bool, error) {
			if token == "header-token" {
				return "header-user", false, nil
			}
			return "", false, errors.New("invalid token")
		})

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		req.Header.Set("X-Auth-Token", "header-token")
		rec := &mockResponseRecorder{
			ResponseRecorder: httptest.NewRecorder(),
		}

		go server.Handler().ServeHTTP(rec, req)
		time.Sleep(500 * time.Millisecond)

		if rec.Header().Get("Content-Type") != "text/event-stream" {
			t.Error("expected SSE headers to be set")
		}
	})
}

func TestSSEServerSendToUserWithClient(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		writer:  rec,
		flusher: rec,
		stop:    make(chan struct{}),
	}

	// Register client manually
	server.clientsMu.Lock()
	server.clients["testuser"] = []*SSEClient{client}
	server.clientsMu.Unlock()

	// Test sending to existing user
	err := server.SendToUser("testuser", "test_event", map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	body := rec.Body.String()
	if !contains(body, "event: test_event") {
		t.Errorf("expected event in output, got: %s", body)
	}
}

func TestSSEServerBroadcastWithClients(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	rec1 := &mockResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec2 := &mockResponseRecorder{ResponseRecorder: httptest.NewRecorder()}

	client1 := &SSEClient{
		user:    "user1",
		isAdmin: false,
		writer:  rec1,
		flusher: rec1,
		stop:    make(chan struct{}),
	}
	client2 := &SSEClient{
		user:    "user2",
		isAdmin: true,
		writer:  rec2,
		flusher: rec2,
		stop:    make(chan struct{}),
	}

	server.clientsMu.Lock()
	server.clients["user1"] = []*SSEClient{client1}
	server.clients["user2"] = []*SSEClient{client2}
	server.clientsMu.Unlock()

	server.Broadcast("broadcast_event", map[string]string{"message": "hello all"})

	if !contains(rec1.Body.String(), "event: broadcast_event") {
		t.Error("expected client1 to receive broadcast")
	}
	if !contains(rec2.Body.String(), "event: broadcast_event") {
		t.Error("expected client2 to receive broadcast")
	}
}

func TestSSEServerSendToAdminsWithAdmin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	adminRec := &mockResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
	userRec := &mockResponseRecorder{ResponseRecorder: httptest.NewRecorder()}

	adminClient := &SSEClient{
		user:    "admin",
		isAdmin: true,
		writer:  adminRec,
		flusher: adminRec,
		stop:    make(chan struct{}),
	}
	regularClient := &SSEClient{
		user:    "user",
		isAdmin: false,
		writer:  userRec,
		flusher: userRec,
		stop:    make(chan struct{}),
	}

	server.clientsMu.Lock()
	server.clients["admin"] = []*SSEClient{adminClient}
	server.clients["user"] = []*SSEClient{regularClient}
	server.clientsMu.Unlock()

	server.SendToAdmins("admin_event", map[string]string{"secret": "data"})

	if !contains(adminRec.Body.String(), "event: admin_event") {
		t.Error("expected admin to receive event")
	}
	if userRec.Body.Len() > 0 {
		t.Error("expected regular user NOT to receive admin event")
	}
}

func TestSSEServerGetConnectedUsersWithClients(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		stop:    make(chan struct{}),
	}

	server.clientsMu.Lock()
	server.clients["testuser"] = []*SSEClient{client}
	server.clientsMu.Unlock()

	users := server.GetConnectedUsers()
	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}
	if users[0] != "testuser" {
		t.Errorf("expected user testuser, got %s", users[0])
	}
}

func TestSSEServerGetConnectedCountWithClients(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		stop:    make(chan struct{}),
	}

	server.clientsMu.Lock()
	server.clients["testuser"] = []*SSEClient{client}
	server.clientsMu.Unlock()

	count := server.GetConnectedCount()
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

// mockResponseRecorder wraps httptest.ResponseRecorder and implements http.Flusher
type mockResponseRecorder struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	flushed bool
}

func (m *mockResponseRecorder) Header() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Header()
}

func (m *mockResponseRecorder) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed = true
}

// GetBody returns the body buffer with lock protection for thread-safe access
func (m *mockResponseRecorder) GetBody() *bytes.Buffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Body
}

func (m *mockResponseRecorder) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Write(b)
}

func (m *mockResponseRecorder) WriteHeader(statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseRecorder.WriteHeader(statusCode)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Additional handler tests

func TestSSEServerHandlerAllowsMultipleClientsForSameUser(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)
	server.SetAuthFunc(func(token string) (string, bool, error) {
		return "testuser", false, nil
	})

	// First client
	oldStop := make(chan struct{})
	oldClient := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		stop:    oldStop,
	}

	server.clientsMu.Lock()
	server.clients["testuser"] = []*SSEClient{oldClient}
	server.clientsMu.Unlock()

	// Connect a second client for the same user with a longer timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
	req.Header.Set("X-Auth-Token", "second-token")
	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	go server.Handler().ServeHTTP(rec, req)

	// Wait for connection to establish, then check both clients exist
	time.Sleep(100 * time.Millisecond)

	server.clientsMu.RLock()
	clients := server.clients["testuser"]
	server.clientsMu.RUnlock()

	if len(clients) != 2 {
		t.Errorf("expected 2 clients for testuser, got %d", len(clients))
	}

	// Old client's stop channel should NOT be closed
	select {
	case <-oldStop:
		t.Error("expected old client stop channel NOT to be closed")
	case <-time.After(100 * time.Millisecond):
		// Good, old client is still active
	}

	// Let the handler finish cleanly
	time.Sleep(500 * time.Millisecond)
}

func TestSSEServerHandleNotificationUnknownType(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := NewSSEServer(logger)

	rec := &mockResponseRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}

	client := &SSEClient{
		user:    "testuser",
		isAdmin: false,
		writer:  rec,
		flusher: rec,
		stop:    make(chan struct{}),
	}

	// Unknown notification type - should not send anything
	notification := imap.MailboxNotification{
		User:    "testuser",
		Type:    imap.NotificationType(999), // Unknown type
		Mailbox: "INBOX",
	}

	server.handleNotification(client, notification)

	if rec.Body.Len() > 0 {
		t.Error("expected no output for unknown notification type")
	}
}
