package pop3

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)

	if server == nil {
		t.Fatal("Server should not be nil")
	}

	if server.addr != ":0" {
		t.Errorf("Expected addr :0, got %s", server.addr)
	}

	if server.sessions == nil {
		t.Error("sessions should be initialized")
	}
}

func TestPOP3Session(t *testing.T) {
	// Create a pipe for testing
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return u == "test" && p == "pass", nil
	})

	// Add test message
	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "test123",
		Size:  100,
		Data:  []byte("Subject: Test\r\n\r\nTest body"),
	})

	// Create session and handle it in background
	session := NewSession(serverConn, server)
	go session.Handle()

	// Test greeting
	reader := bufio.NewReader(clientConn)
	greeting, _ := reader.ReadString('\n')
	if !strings.HasPrefix(greeting, "+OK") {
		t.Errorf("Expected +OK greeting, got %s", greeting)
	}

	// Send USER command
	fmt.Fprintf(clientConn, "USER test\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after USER, got %s", resp)
	}

	// Send PASS command
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after PASS, got %s", resp)
	}

	// Test STAT
	fmt.Fprintf(clientConn, "STAT\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK 1 100") {
		t.Errorf("Expected +OK 1 100, got %s", resp)
	}

	// Test LIST
	fmt.Fprintf(clientConn, "LIST\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after LIST, got %s", resp)
	}

	// Read message list
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "1 100") {
		t.Errorf("Expected message info, got %s", line)
	}

	// Read end marker
	line, _ = reader.ReadString('\n')
	if line != ".\r\n" {
		t.Errorf("Expected .\\r\\n, got %s", line)
	}

	// Test QUIT
	fmt.Fprintf(clientConn, "QUIT\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after QUIT, got %s", resp)
	}
}

func TestNewSession(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)

	session := NewSession(serverConn, server)

	if session == nil {
		t.Fatal("Session should not be nil")
	}

	if session.state != StateAuthorization {
		t.Error("Expected state to be StateAuthorization")
	}

	if session.server != server {
		t.Error("Server should be set")
	}
}

func TestWriteResponse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	session.WriteResponse("+OK Test")

	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if resp != "+OK Test\r\n" {
		t.Errorf("Expected '+OK Test\\r\\n', got %q", resp)
	}
}

