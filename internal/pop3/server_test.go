package pop3

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
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

	// Create session
	session := NewSession(serverConn, server)

	// Create reader BEFORE starting goroutine to avoid pipe deadlock
	reader := bufio.NewReader(clientConn)

	// Start handling in background - greeting will be sent first
	go func() {
		// Send greeting (normally done by handleConnection before Handle())
		session.WriteResponse("+OK uMailServer POP3 ready")
		// Then handle commands
		session.Handle()
	}()

	// Test greeting
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

	// Close both connections to stop the session goroutine
	clientConn.Close()
	serverConn.Close()

	// Give the goroutine time to exit
	time.Sleep(10 * time.Millisecond)
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

	// Create reader first to avoid deadlock
	reader := bufio.NewReader(clientConn)

	// Write in background
	go session.WriteResponse("+OK Test")

	resp, _ := reader.ReadString('\n')
	if resp != "+OK Test\r\n" {
		t.Errorf("Expected '+OK Test\\r\\n', got %q", resp)
	}
}

func TestServerStartStop(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer(":11110", store, nil)

	// Start server
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if !server.running {
		t.Error("expected server to be running")
	}

	// Stop server
	err = server.Stop()
	if err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}

	if server.running {
		t.Error("expected server to be stopped")
	}
}

func TestServerSetAuthFunc(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)

	authFunc := func(username, password string) (bool, error) {
		return true, nil
	}

	server.SetAuthFunc(authFunc)
	if server.authFunc == nil {
		t.Error("expected authFunc to be set")
	}
}

func TestServerSetTLSConfig(t *testing.T) {
	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)

	tlsConfig := &TLSConfig{
		CertFile: "/path/to/cert.pem",
		KeyFile:  "/path/to/key.pem",
	}

	server.SetTLSConfig(tlsConfig)
	if server.tlsConfig == nil {
		t.Error("expected tlsConfig to be set")
	}
	if server.tlsConfig.CertFile != "/path/to/cert.pem" {
		t.Errorf("expected cert file /path/to/cert.pem, got %s", server.tlsConfig.CertFile)
	}
}

func TestSessionClose(t *testing.T) {
	_, serverConn := net.Pipe()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	session.Close()

	// Connection should be closed
	// Just verify no panic
}

func TestSessionID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)

	id := session.ID()
	if id == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestMessageStruct(t *testing.T) {
	msg := &Message{
		Index: 1,
		UID:   "test-uid",
		Size:  1024,
		Data:  []byte("test data"),
	}

	if msg.Index != 1 {
		t.Errorf("expected index 1, got %d", msg.Index)
	}
	if msg.UID != "test-uid" {
		t.Errorf("expected uid test-uid, got %s", msg.UID)
	}
	if msg.Size != 1024 {
		t.Errorf("expected size 1024, got %d", msg.Size)
	}
}

func TestStateConsts(t *testing.T) {
	if StateAuthorization != 0 {
		t.Errorf("expected StateAuthorization to be 0, got %d", StateAuthorization)
	}
	if StateTransaction != 1 {
		t.Errorf("expected StateTransaction to be 1, got %d", StateTransaction)
	}
	if StateUpdate != 2 {
		t.Errorf("expected StateUpdate to be 2, got %d", StateUpdate)
	}
}

func TestPOP3CommandsAuthFailure(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return false, nil // Always fail
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Send USER
	fmt.Fprintf(clientConn, "USER test\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after USER, got %s", resp)
	}

	// Send PASS with wrong password
	fmt.Fprintf(clientConn, "PASS wrong\r\n")
	resp, _ = reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR after wrong PASS, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandInvalidUserFirst(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Send PASS without USER first
	fmt.Fprintf(clientConn, "PASS password\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR when PASS sent before USER, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandSTATNotAuthenticated(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Send STAT without authentication
	fmt.Fprintf(clientConn, "STAT\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for STAT without auth, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandNOOP(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "test123",
		Size:  100,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Send NOOP
	fmt.Fprintf(clientConn, "NOOP\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after NOOP, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandRSET(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "test123",
		Size:  100,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Send RSET
	fmt.Fprintf(clientConn, "RSET\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after RSET, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandUIDL(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "test123",
		Size:  100,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Send UIDL
	fmt.Fprintf(clientConn, "UIDL\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after UIDL, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandCAPA(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Send CAPA
	fmt.Fprintf(clientConn, "CAPA\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after CAPA, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandEmptyLine(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Send empty line
	fmt.Fprintf(clientConn, "\r\n")
	// Should not crash, just continue

	// Send QUIT to end session
	fmt.Fprintf(clientConn, "QUIT\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("Expected +OK after QUIT, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestPOP3CommandInvalidCommand(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewSimpleMemoryStore()
	server := NewServer(":0", store, nil)
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})

	store.AddMessage("test", &Message{
		Index: 1,
		UID:   "test123",
		Size:  100,
		Data:  []byte("test"),
	})

	session := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)

	go func() {
		session.WriteResponse("+OK POP3 server ready")
		session.Handle()
	}()

	// Read greeting
	reader.ReadString('\n')

	// Authenticate
	fmt.Fprintf(clientConn, "USER test\r\n")
	reader.ReadString('\n')
	fmt.Fprintf(clientConn, "PASS pass\r\n")
	reader.ReadString('\n')

	// Send invalid command
	fmt.Fprintf(clientConn, "INVALIDCMD\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("Expected -ERR for invalid command, got %s", resp)
	}

	clientConn.Close()
	serverConn.Close()
	time.Sleep(10 * time.Millisecond)
}

func TestSimpleMemoryStore(t *testing.T) {
	store := NewSimpleMemoryStore()

	// Test Authenticate
	ok, err := store.Authenticate("user", "pass")
	if err != nil {
		t.Errorf("Authenticate returned error: %v", err)
	}
	if !ok {
		t.Error("expected Authenticate to return true")
	}

	// Test ListMessages (empty)
	msgs, err := store.ListMessages("user")
	if err != nil {
		t.Errorf("ListMessages returned error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}

	// Add a message
	store.AddMessage("user", &Message{
		Index: 1,
		UID:   "msg1",
		Size:  100,
		Data:  []byte("test message"),
	})

	// Test ListMessages (with message)
	msgs, err = store.ListMessages("user")
	if err != nil {
		t.Errorf("ListMessages returned error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}

	// Test GetMessage
	msg, err := store.GetMessage("user", 0)
	if err != nil {
		t.Errorf("GetMessage returned error: %v", err)
	}
	if msg.UID != "msg1" {
		t.Errorf("expected uid msg1, got %s", msg.UID)
	}

	// Test GetMessageData
	data, err := store.GetMessageData("user", 0)
	if err != nil {
		t.Errorf("GetMessageData returned error: %v", err)
	}
	if string(data) != "test message" {
		t.Errorf("expected data 'test message', got %s", string(data))
	}

	// Test GetMessageCount
	count, err := store.GetMessageCount("user")
	if err != nil {
		t.Errorf("GetMessageCount returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Test GetMessageSize
	size, err := store.GetMessageSize("user", 0)
	if err != nil {
		t.Errorf("GetMessageSize returned error: %v", err)
	}
	if size != 100 {
		t.Errorf("expected size 100, got %d", size)
	}

	// Test DeleteMessage
	err = store.DeleteMessage("user", 0)
	if err != nil {
		t.Errorf("DeleteMessage returned error: %v", err)
	}

	// Verify message deleted - the message at index 0 should be nil now
	// Note: DeleteMessage sets the message to nil but doesn't remove it from slice
	msgs, _ = store.ListMessages("user")
	if len(msgs) > 0 && msgs[0] != nil {
		t.Error("expected message to be deleted (nil)")
	}
}

func TestSimpleMemoryStoreGetMessageNotFound(t *testing.T) {
	store := NewSimpleMemoryStore()

	// Try to get non-existent message
	_, err := store.GetMessage("nonexistent", 0)
	if err == nil {
		t.Error("expected error when getting non-existent message")
	}
}

func TestSimpleMemoryStoreGetMessageDataNotFound(t *testing.T) {
	store := NewSimpleMemoryStore()

	// Try to get data for non-existent message
	_, err := store.GetMessageData("nonexistent", 0)
	if err == nil {
		t.Error("expected error when getting data for non-existent message")
	}
}

func TestSimpleMemoryStoreGetMessageSizeNotFound(t *testing.T) {
	store := NewSimpleMemoryStore()

	// Try to get size for non-existent message
	_, err := store.GetMessageSize("nonexistent", 0)
	if err == nil {
		t.Error("expected error when getting size for non-existent message")
	}
}

func TestMaildirStore(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Test userPath
	path := store.userPath("test@example.com")
	if path == "" {
		t.Error("expected non-empty user path")
	}

	// Test newPath and curPath
	newPath := store.newPath("test@example.com")
	curPath := store.curPath("test@example.com")
	if newPath == "" || curPath == "" {
		t.Error("expected non-empty paths")
	}
}

func TestMaildirStoreAuthenticate(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Create user directory
	userPath := store.userPath("test@example.com")
	os.MkdirAll(userPath, 0755)

	// Authenticate should succeed when directory exists
	ok, err := store.Authenticate("test@example.com", "password")
	if err != nil {
		t.Errorf("Authenticate returned error: %v", err)
	}
	if !ok {
		t.Error("expected Authenticate to return true when directory exists")
	}

	// Authenticate should fail when directory doesn't exist
	ok, _ = store.Authenticate("nonexistent@example.com", "password")
	if ok {
		t.Error("expected Authenticate to return false when directory doesn't exist")
	}
}

func TestMaildirStoreListMessagesEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// List messages when no maildir exists
	msgs, err := store.ListMessages("test@example.com")
	if err != nil {
		t.Errorf("ListMessages returned error: %v", err)
	}
	// msgs can be nil or empty slice depending on implementation
	if msgs != nil && len(msgs) > 0 {
		t.Errorf("expected empty messages, got %d", len(msgs))
	}
}

func TestMaildirStoreGetMessageNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Try to get non-existent message
	_, err := store.GetMessage("test@example.com", 0)
	if err == nil {
		t.Error("expected error when getting non-existent message")
	}
}

func TestMaildirStoreGetMessageCount(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Get count when no messages
	count, err := store.GetMessageCount("test@example.com")
	if err != nil {
		t.Errorf("GetMessageCount returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestMaildirStoreGetMessageSizeNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Try to get size for non-existent message
	_, err := store.GetMessageSize("test@example.com", 0)
	if err == nil {
		t.Error("expected error when getting size for non-existent message")
	}
}

func TestMaildirStoreGetMessageDataNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewMaildirStore(tmpDir)

	// Try to get data for non-existent message
	_, err := store.GetMessageData("test@example.com", 0)
	if err == nil {
		t.Error("expected error when getting data for non-existent message")
	}
}
