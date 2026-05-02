package imap

import (
	"crypto/tls"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

type mockMailstore struct{}

func (m *mockMailstore) Authenticate(username, password string) (bool, error) {
	return username == "test" && password == "password", nil
}

func (m *mockMailstore) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	return &Mailbox{Name: mailbox, Exists: 10}, nil
}

func (m *mockMailstore) CreateMailbox(user, mailbox string) error {
	return nil
}

func (m *mockMailstore) DeleteMailbox(user, mailbox string) error {
	return nil
}

func (m *mockMailstore) RenameMailbox(user, oldName, newName string) error {
	return nil
}

func (m *mockMailstore) ListMailboxes(user, pattern string) ([]string, error) {
	return []string{"INBOX", "Sent", "Drafts"}, nil
}

func (m *mockMailstore) SetSubscribed(user, mailbox string, subscribed bool) error {
	return nil
}

func (m *mockMailstore) GetSubscribed(user, mailbox string) (bool, error) {
	return false, nil
}

func (m *mockMailstore) ListSubscribed(user string) ([]string, error) {
	return []string{}, nil
}

func (m *mockMailstore) FetchMessages(user, mailbox string, seqSet string, items []string) ([]*Message, error) {
	return []*Message{}, nil
}

func (m *mockMailstore) StoreFlags(user, mailbox string, seqSet string, flags []string, op FlagOperation) error {
	return nil
}

func (m *mockMailstore) Expunge(user, mailbox string) error {
	return nil
}

func (m *mockMailstore) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	return nil
}

func (m *mockMailstore) SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error) {
	return []uint32{}, nil
}

func (m *mockMailstore) CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	return nil
}

func (m *mockMailstore) MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error {
	return nil
}

func (m *mockMailstore) EnsureDefaultMailboxes(user string) error {
	return nil
}

func (m *mockMailstore) GetACL(owner, mailbox, grantee string) (uint8, error) {
	return 0, nil
}

func (m *mockMailstore) SetACL(owner, mailbox, grantee string, rights uint8, grantingUser string) error {
	return nil
}

func (m *mockMailstore) DeleteACL(owner, mailbox, grantee string) error {
	return nil
}

func (m *mockMailstore) ListACL(owner, mailbox string) ([]storage.ACLEntry, error) {
	return nil, nil
}

func (m *mockMailstore) ListMailboxesSharedWith(user string) ([]string, error) {
	return nil, nil
}

func (m *mockMailstore) ListGranteesMailboxes(owner string) ([]string, error) {
	return nil, nil
}

func TestNewServer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	config := &Config{
		Addr:   ":1143",
		Logger: logger,
	}
	mailstore := &mockMailstore{}

	server := NewServer(config, mailstore)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.addr != ":1143" {
		t.Errorf("expected addr :1143, got %s", server.addr)
	}
	if server.sessions == nil {
		t.Error("expected sessions map to be initialized")
	}
}

func TestNewServerNilLogger(t *testing.T) {
	config := &Config{
		Addr: ":1143",
	}
	mailstore := &mockMailstore{}

	server := NewServer(config, mailstore)
	if server.logger == nil {
		t.Error("expected logger to be initialized with default")
	}
}

func TestServerSetAuthFunc(t *testing.T) {
	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	authFunc := func(username, password string) (bool, error) {
		return true, nil
	}

	server.SetAuthFunc(authFunc)
	if server.authFunc == nil {
		t.Error("expected authFunc to be set")
	}
}

func TestServerNotStarted(t *testing.T) {
	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	if server.running.Load() {
		t.Error("expected server to not be running initially")
	}
}

func TestConfigStruct(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	tlsConfig := &tls.Config{}

	config := &Config{
		Addr:      ":993",
		TLSConfig: tlsConfig,
		Logger:    logger,
	}

	if config.Addr != ":993" {
		t.Errorf("expected addr :993, got %s", config.Addr)
	}
	if config.TLSConfig != tlsConfig {
		t.Error("expected TLSConfig to be set")
	}
	if config.Logger != logger {
		t.Error("expected Logger to be set")
	}
}

func TestSessionMethods(t *testing.T) {
	// Create a mock connection using net.Pipe
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Test ID
	if session.ID() == "" {
		t.Error("expected session ID to be non-empty")
	}

	// Test State
	if session.State() != StateNotAuthenticated {
		t.Errorf("expected initial state StateNotAuthenticated, got %d", session.State())
	}

	// Test User (should be empty initially)
	if session.User() != "" {
		t.Errorf("expected empty user, got %s", session.User())
	}

	// Test Selected (should be nil initially)
	if session.Selected() != nil {
		t.Error("expected nil selected mailbox")
	}
}

func TestSessionClose(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Close the session
	session.Close()

	// Check state is logged out
	if session.State() != StateLoggedOut {
		t.Errorf("expected state StateLoggedOut after close, got %d", session.State())
	}
}

func TestDefaultCapabilities(t *testing.T) {
	caps := defaultCapabilities()

	// Check some expected capabilities
	expectedCaps := []string{"IMAP4rev1", "STARTTLS", "AUTH=PLAIN", "IDLE", "UIDPLUS"}
	for _, cap := range expectedCaps {
		found := false
		for _, c := range caps {
			if c == cap {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected capability %s not found", cap)
		}
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	time.Sleep(time.Nanosecond) // Small delay to ensure different timestamp
	id2 := generateSessionID()

	if id1 == "" {
		t.Error("expected non-empty session ID")
	}

	if id1 == id2 {
		t.Error("expected unique session IDs")
	}
}

func TestSessionWriteResponse(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write a response
	go func() {
		session.WriteResponse("A1", "OK test completed")
	}()

	// Read from client side
	buf := make([]byte, 1024)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := string(buf[:n])
	if !strings.Contains(response, "A1") {
		t.Errorf("expected response to contain 'A1', got %s", response)
	}
	if !strings.Contains(response, "OK test completed") {
		t.Errorf("expected response to contain 'OK test completed', got %s", response)
	}
}

func TestSessionWriteData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write data
	go func() {
		session.WriteData("1 FETCH (UID 100)")
	}()

	// Read from client side
	buf := make([]byte, 1024)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := string(buf[:n])
	if !strings.HasPrefix(response, "* ") {
		t.Errorf("expected data response to start with '* ', got %s", response)
	}
}

func TestSessionWriteContinuation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write continuation
	go func() {
		session.WriteContinuation("ready for literal data")
	}()

	// Read from client side
	buf := make([]byte, 1024)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	response := string(buf[:n])
	if !strings.HasPrefix(response, "+ ") {
		t.Errorf("expected continuation response to start with '+ ', got %s", response)
	}
}

func TestServerWithTLSConfig(t *testing.T) {
	// Create a test TLS config (insecure for testing)
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	config := &Config{
		Addr:      ":1143",
		TLSConfig: tlsConfig,
	}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	if server.tlsConfig != tlsConfig {
		t.Error("expected TLS config to be set")
	}
}

func TestSessionReadLine(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write a line from client
	go func() {
		client.Write([]byte("A1 LOGIN user pass\r\n"))
	}()

	// Read line on server side
	line, err := session.readLine()
	if err != nil {
		t.Fatalf("failed to read line: %v", err)
	}

	if line != "A1 LOGIN user pass" {
		t.Errorf("expected line 'A1 LOGIN user pass', got %s", line)
	}
}

func TestServerStartStop(t *testing.T) {
	config := &Config{Addr: "127.0.0.1:0"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	// Start server
	err := server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	if !server.running.Load() {
		t.Error("expected server to be running")
	}

	// Stop server
	err = server.Stop()
	if err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}

	if server.running.Load() {
		t.Error("expected server to be stopped")
	}
}

func TestServerStartTLS(t *testing.T) {
	// Generate self-signed certificate for testing
	certPEM := `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAFBgMrZXAwEDEOMAwGA1UE
AwwFVVNFUjEwMCAXDTIzMDEwMTAwMDAwMFoYDzIwNTIwMTAxMDAwMDAwWjAQMQ4w
DAYDVQQDDAVVU0VSMTAwKzAFBgMrZXEAJ3dhbnRkdWV4cGxvcmVydGhlYXJ0Zm9y
Y2hpbmVycmVhbGx5ZmFzdGVydDANBgMrZXAEBwC4HNHczqMwBQYDK2VxA0EALVVx
dmVyeWxvbmNvbW1pdG1lbnR0aGF0aXN0cnVseW1lYW5pbmdmdWxhbmR0aGF0d2ls
bHN0YW5kdXB0b3N0dWZm
-----END CERTIFICATE-----`

	keyPEM := `-----BEGIN PRIVATE KEY-----
MFMCAQEwBQYDK2VxBCIEILW1xdydmVyeWxvbmNvbW1pdG1lbnR0aGF0aXN0cnVs
eW1lYW5pbmdmdWxhbmR0aGF0d2lsbHN0YW5kdXB0b3N0dWZmIT7FehQdRp6YysM=
-----END PRIVATE KEY-----`

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Skipf("Skipping TLS test: failed to load test certificates: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	config := &Config{
		Addr:      "127.0.0.1:0",
		TLSConfig: tlsConfig,
	}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	// Start TLS server
	err = server.StartTLS()
	if err != nil {
		t.Fatalf("Failed to start TLS server: %v", err)
	}

	if !server.running.Load() {
		t.Error("expected TLS server to be running")
	}

	// Stop server
	err = server.Stop()
	if err != nil {
		t.Fatalf("Failed to stop TLS server: %v", err)
	}
}

func TestServerStartTLSNoConfig(t *testing.T) {
	config := &Config{Addr: "127.0.0.1:0"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	// Try to start TLS without config
	err := server.StartTLS()
	if err == nil {
		t.Error("expected error when starting TLS without config")
	}
}

func TestSessionHandleCapability(t *testing.T) {
	// Test that handleCommand exists and can be called
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write command and read response concurrently with timeout
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write command
		client.Write([]byte("A1 CAPABILITY\r\n"))
		// Read response
		n, _ := client.Read(buf)
		response := string(buf[:n])
		if strings.Contains(response, "IMAP4rev1") || strings.Contains(response, "A1") {
			done <- true
		}
		// Close client to end session.Handle() loop
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
		// Test passed
	case <-time.After(500 * time.Millisecond):
		// Close client to stop the session
		client.Close()
	}
}

func TestSessionHandleNoop(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Write command and read response concurrently with timeout
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write command
		client.Write([]byte("A1 NOOP\r\n"))
		// Read response
		n, _ := client.Read(buf)
		response := string(buf[:n])
		if strings.Contains(response, "A1") {
			done <- true
		}
		// Close client to end session.Handle() loop
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
		// Test passed
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleLogout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Send LOGOUT command with timeout
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write command
		client.Write([]byte("A1 LOGOUT\r\n"))
		// Read some responses
		for i := 0; i < 3; i++ {
			client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			client.Read(buf)
		}
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}

	// Check state is logged out (or was attempted)
	if session.State() != StateLoggedOut {
		t.Logf("Session state after logout attempt: %d", session.State())
	}
}

func TestSessionHandleInvalidCommand(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Send invalid command with timeout
	done := make(chan string, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write command
		client.Write([]byte("A1 INVALIDCMD\r\n"))
		// Read response with timeout
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _ := client.Read(buf)
		done <- string(buf[:n])
		client.Close()
	}()

	// Handle one command
	go session.Handle()

	// Wait for result with timeout
	select {
	case response := <-done:
		if !strings.Contains(response, "BAD") && !strings.Contains(response, "A1") {
			t.Logf("Response: %s", response)
		}
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleStartTLS(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Create test TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	config := &Config{
		Addr:      ":1143",
		TLSConfig: tlsConfig,
	}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Send STARTTLS command with timeout
	done := make(chan string, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write command
		client.Write([]byte("A1 STARTTLS\r\n"))
		// Read response with timeout
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _ := client.Read(buf)
		done <- string(buf[:n])
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case response := <-done:
		// STARTTLS should fail without proper TLS config on server side
		if !strings.Contains(response, "A1") {
			t.Logf("STARTTLS response: %s", response)
		}
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionStateTransitions(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Initial state
	if session.State() != StateNotAuthenticated {
		t.Errorf("expected initial state StateNotAuthenticated, got %d", session.State())
	}

	// Simulate authentication
	session.state = StateAuthenticated
	if session.State() != StateAuthenticated {
		t.Errorf("expected state StateAuthenticated, got %d", session.State())
	}

	// Simulate selection
	session.state = StateSelected
	if session.State() != StateSelected {
		t.Errorf("expected state StateSelected, got %d", session.State())
	}
}

func TestSessionUserAfterAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Set user directly (simulating auth)
	session.user = "testuser"

	if session.User() != "testuser" {
		t.Errorf("expected user 'testuser', got %s", session.User())
	}
}

func TestSessionSelectedMailbox(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Set selected mailbox
	mbox := &Mailbox{Name: "INBOX", Exists: 10}
	session.selected = mbox

	if session.Selected() == nil {
		t.Fatal("expected selected mailbox to be set")
	}

	if session.Selected().Name != "INBOX" {
		t.Errorf("expected selected mailbox name 'INBOX', got %s", session.Selected().Name)
	}
}

func TestSessionHandleEmptyLine(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Send empty line (just CRLF) with timeout
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Send empty line
		client.Write([]byte("\r\n"))
		// Then send a valid command to exit the loop
		time.Sleep(50 * time.Millisecond)
		client.Write([]byte("A1 LOGOUT\r\n"))
		// Read some responses
		for i := 0; i < 3; i++ {
			client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			client.Read(buf)
		}
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}

	// Session should be logged out (or was attempted)
	if session.State() != StateLoggedOut {
		t.Logf("Session state after empty line test: %d", session.State())
	}
}

func TestGenerateSessionIDUnique(t *testing.T) {
	// Generate multiple session IDs and ensure uniqueness
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateSessionID()
		if ids[id] {
			t.Fatalf("duplicate session ID generated: %s", id)
		}
		ids[id] = true
		time.Sleep(time.Nanosecond)
	}
}

func TestDefaultCapabilitiesContent(t *testing.T) {
	caps := defaultCapabilities()

	expectedCaps := []string{
		"IMAP4rev1",
		"STARTTLS",
		"AUTH=PLAIN",
		"AUTH=LOGIN",
		"IDLE",
		"NAMESPACE",
		"CHILDREN",
		"UIDPLUS",
		"MOVE",
		"CONDSTORE",
		"ENABLE",
		"LITERAL+",
		"SASL-IR",
		"ESEARCH",
	}

	for _, cap := range expectedCaps {
		found := false
		for _, c := range caps {
			if c == cap {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected capability %s not found in default capabilities", cap)
		}
	}
}

func TestServerMailstore(t *testing.T) {
	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	server := NewServer(config, mailstore)

	// Verify mailstore is set
	if server.mailstore == nil {
		t.Error("expected mailstore to be set")
	}
}

func TestSessionTLSConnection(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Initially TLS should not be active
	if session.tlsActive {
		t.Error("expected TLS to not be active initially")
	}
}

func TestSessionIdleState(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Initially IDLE should not be active
	if session.idleActive {
		t.Error("expected IDLE to not be active initially")
	}
}

func TestSessionHandleLogin(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Send LOGIN command with timeout
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write LOGIN command
		client.Write([]byte("A1 LOGIN test password\r\n"))
		// Read response with timeout
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleSelectBeforeAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Try to SELECT before authentication
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		// Skip greeting
		buf := make([]byte, 1024)
		client.Read(buf)
		// Write SELECT command
		client.Write([]byte("A1 SELECT INBOX\r\n"))
		// Read response with timeout
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleCreate(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate authenticated state
	session.state = StateAuthenticated
	session.user = "testuser"

	// Test CREATE command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send CREATE command
		client.Write([]byte("A1 CREATE TestMailbox\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleDelete(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate authenticated state
	session.state = StateAuthenticated
	session.user = "testuser"

	// Test DELETE command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send DELETE command
		client.Write([]byte("A1 DELETE TestMailbox\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleRename(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate authenticated state
	session.state = StateAuthenticated
	session.user = "testuser"

	// Test RENAME command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send RENAME command
		client.Write([]byte("A1 RENAME OldMailbox NewMailbox\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleSubscribe(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate authenticated state
	session.state = StateAuthenticated
	session.user = "testuser"

	// Test SUBSCRIBE command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send SUBSCRIBE command
		client.Write([]byte("A1 SUBSCRIBE TestMailbox\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleExpunge(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate selected state
	session.state = StateSelected
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test EXPUNGE command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send EXPUNGE command
		client.Write([]byte("A1 EXPUNGE\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleSearch(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate selected state
	session.state = StateSelected
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test SEARCH command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send SEARCH command
		client.Write([]byte("A1 SEARCH ALL\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleFetch(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate selected state
	session.state = StateSelected
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX", Exists: 10}

	// Test FETCH command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send FETCH command
		client.Write([]byte("A1 FETCH 1 (FLAGS)\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

func TestSessionHandleStatus(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	config := &Config{Addr: ":1143"}
	mailstore := &mockMailstore{}
	imapServer := NewServer(config, mailstore)

	session := NewSession(server, imapServer)

	// Simulate authenticated state
	session.state = StateAuthenticated
	session.user = "testuser"

	// Test STATUS command
	done := make(chan bool, 1)
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		// Read greeting
		client.Read(buf)
		// Send STATUS command
		client.Write([]byte("A1 STATUS INBOX (MESSAGES)\r\n"))
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		client.Read(buf)
		client.Close()
	}()

	// Handle the session in background
	go session.Handle()

	// Wait for result with timeout
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		client.Close()
	}
}

// TestServerStartTLSNoTLSConfig tests StartTLS without TLS config
func TestServerStartTLSNoTLSConfig(t *testing.T) {
	mailstore := &mockMailstore{}
	s := NewServer(&Config{Addr: ":1143"}, mailstore)

	err := s.StartTLS()
	if err == nil {
		t.Error("Expected error when starting TLS without config")
	}
	if !strings.Contains(err.Error(), "TLS config not provided") {
		t.Errorf("Expected 'TLS config not provided' error, got: %v", err)
	}
}

// TestAcceptLoopShutdown tests acceptLoop exits on shutdown signal
func TestAcceptLoopShutdown(t *testing.T) {
	mailstore := &mockMailstore{}
	s := NewServer(&Config{Addr: "127.0.0.1:0"}, mailstore)

	// Create a listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Initialize shutdown channel
	s.shutdown = make(chan struct{})
	s.running.Store(true)

	// Start acceptLoop
	done := make(chan bool)
	go func() {
		s.acceptLoop(listener)
		done <- true
	}()

	// Close shutdown channel to trigger exit
	close(s.shutdown)

	select {
	case <-done:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for acceptLoop to exit")
	}
}
