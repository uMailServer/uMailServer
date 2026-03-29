package smtp

import (
	"net"
	"testing"

	"github.com/google/uuid"
)

func TestSessionID(t *testing.T) {
	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Connect to ourselves
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// ID should be a valid UUID
	id := session.ID()
	if id == "" {
		t.Error("Expected non-empty session ID")
	}

	// Should be able to parse as UUID
	_, err = uuid.Parse(id)
	if err != nil {
		t.Errorf("Expected valid UUID, got: %v", err)
	}
}

func TestSessionRemoteAddr(t *testing.T) {
	// Create a mock connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// RemoteAddr should not be nil
	addr := session.RemoteAddr()
	if addr == nil {
		t.Error("Expected non-nil remote address")
	}
}

func TestSessionState(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Initial state should be StateNew
	if session.State() != StateNew {
		t.Errorf("Expected initial state StateNew, got %v", session.State())
	}
}

func TestSessionIsTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Initially not TLS
	if session.IsTLS() {
		t.Error("Expected IsTLS to be false initially")
	}

	// Set TLS manually
	session.mutex.Lock()
	session.isTLS = true
	session.mutex.Unlock()

	if !session.IsTLS() {
		t.Error("Expected IsTLS to be true after setting")
	}
}

func TestSessionIsAuthenticated(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Initially not authenticated
	if session.IsAuthenticated() {
		t.Error("Expected IsAuthenticated to be false initially")
	}

	// Set authenticated manually
	session.mutex.Lock()
	session.isAuth = true
	session.mutex.Unlock()

	if !session.IsAuthenticated() {
		t.Error("Expected IsAuthenticated to be true after setting")
	}
}

func TestSessionUsername(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Initially empty username
	if session.Username() != "" {
		t.Errorf("Expected empty username initially, got %q", session.Username())
	}

	// Set username manually
	session.mutex.Lock()
	session.username = "testuser@example.com"
	session.mutex.Unlock()

	if session.Username() != "testuser@example.com" {
		t.Errorf("Expected username 'testuser@example.com', got %q", session.Username())
	}
}

func TestSessionGetterStateTransitions(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Test all states
	states := []SessionState{
		StateNew,
		StateGreeted,
		StateMailFrom,
		StateRcptTo,
		StateData,
	}

	for _, state := range states {
		session.mutex.Lock()
		session.state = state
		session.mutex.Unlock()

		if session.State() != state {
			t.Errorf("Expected state %v, got %v", state, session.State())
		}
	}
}

func TestNewSession(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	server := &Server{}
	session := NewSession(conn, server)

	// Check that session is properly initialized
	if session == nil {
		t.Fatal("Expected non-nil session")
	}

	if session.id == "" {
		t.Error("Expected session ID to be set")
	}

	if session.conn != conn {
		t.Error("Expected connection to be set")
	}

	if session.server != server {
		t.Error("Expected server to be set")
	}

	if session.State() != StateNew {
		t.Error("Expected initial state to be StateNew")
	}

	if session.rcptTo == nil {
		t.Error("Expected rcptTo to be initialized")
	}
}
