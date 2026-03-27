package websocket

import (
	"testing"
)

func TestNewSSEServer(t *testing.T) {
	server := NewSSEServer(nil)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.clients == nil {
		t.Error("expected clients map to be initialized")
	}
}

func TestSSEServerGetConnectedCount(t *testing.T) {
	server := NewSSEServer(nil)

	// Initially should be 0
	if server.GetConnectedCount() != 0 {
		t.Errorf("expected 0 connected clients, got %d", server.GetConnectedCount())
	}
}

func TestSSEServerGetConnectedUsers(t *testing.T) {
	server := NewSSEServer(nil)

	users := server.GetConnectedUsers()
	if users == nil {
		t.Error("expected non-nil users slice")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 connected users, got %d", len(users))
	}
}

func TestSSEServerSetAuthFunc(t *testing.T) {
	server := NewSSEServer(nil)

	authFunc := func(token string) (string, bool, error) {
		return "test-user", false, nil
	}

	server.SetAuthFunc(authFunc)
	if server.authFunc == nil {
		t.Error("expected authFunc to be set")
	}
}

func TestSSEServerBroadcast(t *testing.T) {
	server := NewSSEServer(nil)

	// Should not panic when no clients connected
	server.Broadcast("test", map[string]string{"key": "value"})
}

func TestSSEServerSendToAdmins(t *testing.T) {
	server := NewSSEServer(nil)

	// Should not panic when no admin clients connected
	server.SendToAdmins("admin_event", map[string]string{"key": "value"})
}
