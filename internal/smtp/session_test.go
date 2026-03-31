package smtp

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

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

// ---------------------------------------------------------------------------
// BDAT command tests (RFC 3030 CHUNKING)
// ---------------------------------------------------------------------------

// newBDATTestSession creates a session wired to a pair of connected TCP sockets
// so we can write data that the server-side io.ReadFull will see.
func newBDATTestSession(t *testing.T) (*Session, net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept happens in goroutine, Dial from client side
	serverConnCh := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept()
		serverConnCh <- c
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	serverConn := <-serverConnCh

	server := &Server{
		config: &Config{
			Hostname:       "testhost",
			MaxMessageSize: 1024 * 1024, // 1 MB
			MaxRecipients:  100,
		},
	}

	session := NewSession(serverConn, server)
	return session, clientConn
}

// setupBDATSessionState puts the session into StateRcptTo so BDAT is accepted.
func setupBDATSessionState(s *Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.state = StateRcptTo
	s.mailFrom = "sender@example.com"
	s.rcptTo = []string{"rcpt@example.com"}
}

func TestHandleBDAT_NonLastChunk(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// Write 5 bytes of chunk data that the server will read via io.ReadFull
	chunkData := "HELLO"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	// Process BDAT with size 5 (non-last)
	err = session.handleBDAT("5")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	// Read response from server
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 response for non-last chunk, got: %q", response)
	}

	// Verify bdatBuffer contains the chunk data
	session.mutex.RLock()
	if session.bdatBuffer == nil {
		session.mutex.RUnlock()
		t.Fatal("Expected bdatBuffer to be initialized")
	}
	if session.bdatBuffer.String() != chunkData {
		t.Errorf("Expected bdatBuffer to contain %q, got %q", chunkData, session.bdatBuffer.String())
	}
	session.mutex.RUnlock()
}

func TestHandleBDAT_LastFlag(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// Write chunk data
	chunkData := "WORLD"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	// Set up a delivery handler to capture the message
	var deliveredFrom string
	var deliveredTo []string
	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredFrom = from
		deliveredTo = to
		deliveredData = data
		return nil
	}

	// Process BDAT with LAST flag
	err = session.handleBDAT("5 LAST")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	// Read response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 response for last chunk, got: %q", response)
	}

	// Verify message was delivered
	if deliveredFrom != "sender@example.com" {
		t.Errorf("Expected delivered from 'sender@example.com', got %q", deliveredFrom)
	}
	if len(deliveredTo) != 1 || deliveredTo[0] != "rcpt@example.com" {
		t.Errorf("Expected delivered to ['rcpt@example.com'], got %v", deliveredTo)
	}
	if string(deliveredData) != chunkData {
		t.Errorf("Expected delivered data %q, got %q", chunkData, string(deliveredData))
	}
}

func TestHandleBDAT_ZeroSizeLastChunk(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// First send a non-last chunk with some data
	chunkData := "HELLO"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write first chunk: %v", err)
	}

	err = session.handleBDAT("5")
	if err != nil {
		t.Fatalf("handleBDAT first chunk returned error: %v", err)
	}

	// Drain the 250 response from the first BDAT
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	clientConn.Read(buf)

	// Set up delivery handler
	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	// Now send zero-size LAST chunk (no data to write)
	err = session.handleBDAT("0 LAST")
	if err != nil {
		t.Fatalf("handleBDAT zero-size last returned error: %v", err)
	}

	// Read response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf2 := make([]byte, 256)
	n, _ := clientConn.Read(buf2)
	response := string(buf2[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 response for zero-size last chunk, got: %q", response)
	}

	// Should have delivered the data from the first chunk
	if string(deliveredData) != chunkData {
		t.Errorf("Expected delivered data %q, got %q", chunkData, string(deliveredData))
	}
}

func TestHandleBDAT_WithoutPriorMailFrom(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Do NOT set state to StateRcptTo; leave at StateNew or StateGreeted
	// so BDAT should be rejected with 503
	err := session.handleBDAT("5")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	// Read the 503 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 response when BDAT called without prior MAIL/RCPT, got: %q", response)
	}
}

func TestHandleBDAT_SizeExceeded(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// Set a very small max message size
	session.server.config.MaxMessageSize = 10 // 10 bytes max

	err := session.handleBDAT("100")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	// Read response — should be 552
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "552") {
		t.Errorf("Expected 552 response for size exceeded, got: %q", response)
	}
}

func TestHandleBDAT_SyntaxErrorNoArgs(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	err := session.handleBDAT("")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "501") {
		t.Errorf("Expected 501 response for missing BDAT args, got: %q", response)
	}
}

func TestHandleBDAT_SyntaxErrorBadSize(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	err := session.handleBDAT("notanumber")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "501") {
		t.Errorf("Expected 501 response for invalid BDAT size, got: %q", response)
	}
}

func TestHandleBDAT_NegativeSize(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	err := session.handleBDAT("-1")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "501") {
		t.Errorf("Expected 501 response for negative BDAT size, got: %q", response)
	}
}

// ---------------------------------------------------------------------------
// handleDATA tests
// ---------------------------------------------------------------------------

// newDataTestSession creates a session pair similar to newBDATTestSession
// but with state set up for DATA testing.
func newDataTestSession(t *testing.T) (*Session, net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	serverConnCh := make(chan net.Conn, 1)
	go func() {
		c, _ := listener.Accept()
		serverConnCh <- c
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	serverConn := <-serverConnCh

	server := &Server{
		config: &Config{
			Hostname:       "testhost",
			MaxMessageSize: 1024 * 1024,
			MaxRecipients:  100,
		},
	}

	session := NewSession(serverConn, server)

	// Put session into StateRcptTo (required before DATA)
	session.mutex.Lock()
	session.state = StateRcptTo
	session.mailFrom = "sender@example.com"
	session.rcptTo = []string{"rcpt@example.com"}
	session.mutex.Unlock()

	return session, clientConn
}

func TestHandleDATA_FullFlow(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Set up delivery handler
	var deliveredFrom string
	var deliveredTo []string
	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredFrom = from
		deliveredTo = to
		deliveredData = data
		return nil
	}

	// Read the 354 response in a goroutine, then write data
	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	// Read the 354 "Start mail input" response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read 354 response: %v", err)
	}
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354 response, got: %s", resp)
	}

	// Send message body followed by the end-of-data marker
	clientConn.Write([]byte("Subject: Test\r\nFrom: sender@example.com\r\nTo: rcpt@example.com\r\n\r\nHello World\r\n.\r\n"))

	// Wait for handleDATA to finish
	if err := <-done; err != nil {
		t.Fatalf("handleDATA returned error: %v", err)
	}

	// Read the 250 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "250") {
		t.Errorf("Expected 250 response after DATA, got: %s", resp2)
	}

	// Verify delivery handler was called
	if deliveredFrom != "sender@example.com" {
		t.Errorf("Expected from 'sender@example.com', got %q", deliveredFrom)
	}
	if len(deliveredTo) != 1 || deliveredTo[0] != "rcpt@example.com" {
		t.Errorf("Expected to ['rcpt@example.com'], got %v", deliveredTo)
	}
	if len(deliveredData) == 0 {
		t.Error("Expected non-empty delivered data")
	}
}

func TestHandleDATA_BadState(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Session is in StateNew, so DATA should fail with 503
	err := session.handleDATA()
	if err != nil {
		t.Fatalf("handleDATA returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 for DATA without RCPT TO, got: %q", response)
	}
}

func TestHandleDATA_SizeExceeded(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Set a very small max message size
	session.server.config.MaxMessageSize = 10

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	// Read 354
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354, got: %s", resp)
	}

	// Send a message larger than 10 bytes
	clientConn.Write([]byte("Subject: Test\r\n\r\nThis message is definitely longer than ten bytes.\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "552") {
		t.Errorf("Expected 552 size exceeded, got: %s", resp2)
	}
}

func TestHandleDATA_DeliveryError(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Delivery handler that returns an error
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		return fmt.Errorf("delivery failed")
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354, got: %s", resp)
	}

	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "451") {
		t.Errorf("Expected 451 for delivery failure, got: %s", resp2)
	}
}

func TestHandleDATA_WithPipelineReject(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Set up pipeline with a rejecting stage
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&rejectStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354, got: %s", resp)
	}

	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	// Pipeline.Process returns error for reject, so handleDATA returns 451
	if !strings.HasPrefix(resp2, "451") {
		t.Errorf("Expected 451 for pipeline error, got: %s", resp2)
	}
}

func TestHandleDATA_WithPipelineQuarantine(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	// Set up pipeline with a quarantine stage
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&quarantineStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354, got: %s", resp)
	}

	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "250") {
		t.Errorf("Expected 250 OK (quarantined messages still deliver), got: %s", resp2)
	}

	// Data should have X-Spam-Status header prepended
	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	if !strings.Contains(string(deliveredData), "X-Spam-Status") {
		t.Errorf("Expected X-Spam-Status header in delivered data, got: %s", string(deliveredData[:200]))
	}
}

func TestHandleDATA_DotStuffing(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "354") {
		t.Fatalf("Expected 354, got: %s", resp)
	}

	// Send message with dot-stuffed line (a line starting with ".." should become ".")
	clientConn.Write([]byte("Subject: Test\r\n\r\n..dot-stuffed line\r\n.\r\n"))

	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "250") {
		t.Errorf("Expected 250 OK, got: %s", resp2)
	}

	// The dot-stuffed ".." should have been unstuffed to "."
	bodyStr := string(deliveredData)
	if !strings.Contains(bodyStr, ".dot-stuffed line") {
		t.Errorf("Expected dot-unstuffed content, got: %q", bodyStr)
	}
}

func TestHandleDATA_MessageIDAdded(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n')

	// Send message without Message-ID header
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))

	<-done

	reader.ReadString('\n')

	// The delivered data should contain a Message-ID header
	if !strings.Contains(strings.ToLower(string(deliveredData)), "message-id:") {
		t.Errorf("Expected Message-ID header to be added, got: %q", string(deliveredData[:200]))
	}
}

// ---------------------------------------------------------------------------
// handleSTARTTLS tests
// ---------------------------------------------------------------------------

func TestHandleSTARTTLS_NoTLSConfig(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// No TLS config set (default is nil)
	err := session.handleSTARTTLS()
	if err != nil {
		t.Fatalf("handleSTARTTLS returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "502") {
		t.Errorf("Expected 502 when TLS config is nil, got: %q", response)
	}
}

func TestHandleSTARTTLS_AlreadyTLS(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Set TLS config and mark session as already TLS
	session.server.config.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	session.mutex.Lock()
	session.isTLS = true
	session.mutex.Unlock()

	err := session.handleSTARTTLS()
	if err != nil {
		t.Fatalf("handleSTARTTLS returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 when already TLS, got: %q", response)
	}
}

func TestHandleSTARTTLS_SuccessfulUpgrade(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()

	// Generate a self-signed cert for testing
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	session.server.config.TLSConfig = tlsConfig

	// handleSTARTTLS sends 220 then tries TLS handshake
	done := make(chan error, 1)
	go func() {
		done <- session.handleSTARTTLS()
	}()

	// Read the 220 response from the raw TCP connection
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, readErr := clientConn.Read(buf)
	if readErr != nil {
		t.Fatalf("Failed to read 220 response: %v", readErr)
	}
	response := string(buf[:n])
	if !strings.Contains(response, "220") {
		t.Fatalf("Expected 220 Ready to start TLS, got: %q", response)
	}

	// Now upgrade the client to TLS to complete the handshake
	tlsClientConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	tlsClientConn := tls.Client(clientConn, tlsClientConfig)
	defer tlsClientConn.Close()

	// Set a deadline so handshake doesn't hang forever
	tlsClientConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Perform the TLS handshake from the client side
	err = tlsClientConn.Handshake()
	if err != nil {
		t.Fatalf("TLS handshake failed: %v", err)
	}

	// Wait for handleSTARTTLS to complete
	if err := <-done; err != nil {
		t.Errorf("handleSTARTTLS returned error: %v", err)
	}

	// Verify the session is now marked as TLS
	if !session.IsTLS() {
		t.Error("Expected session to be marked as TLS after STARTTLS upgrade")
	}

	// Verify state was reset to StateNew
	if session.State() != StateNew {
		t.Errorf("Expected state StateNew after STARTTLS, got %v", session.State())
	}
}

// ---------------------------------------------------------------------------
// handleBDAT pipeline and total-size tests
// ---------------------------------------------------------------------------

func TestHandleBDAT_TotalSizeExceededOnLast(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// Set a max message size that allows individual chunks but not combined
	session.server.config.MaxMessageSize = 10

	// First chunk: 4 bytes (under the 10 byte limit)
	chunk1 := "ABCD"
	_, err := clientConn.Write([]byte(chunk1))
	if err != nil {
		t.Fatalf("Failed to write chunk1 data: %v", err)
	}
	err = session.handleBDAT("4")
	if err != nil {
		t.Fatalf("handleBDAT first chunk returned error: %v", err)
	}

	// Drain the 250 response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	clientConn.Read(buf)

	// Second chunk: 7 bytes, which when combined with the first 4 = 11 > 10
	chunk2 := "EFGHIJK"
	_, err = clientConn.Write([]byte(chunk2))
	if err != nil {
		t.Fatalf("Failed to write chunk2 data: %v", err)
	}

	err = session.handleBDAT("7 LAST")
	if err != nil {
		t.Fatalf("handleBDAT last chunk returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf2 := make([]byte, 256)
	n, _ := clientConn.Read(buf2)
	response := string(buf2[:n])

	if !strings.Contains(response, "552") {
		t.Errorf("Expected 552 for total size exceeded on LAST, got: %q", response)
	}
}

func TestHandleBDAT_LastWithPipelineReject(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	// Set up pipeline with a rejecting stage
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&rejectStage{})
	session.server.pipeline = pipeline

	chunkData := "HELLO"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	err = session.handleBDAT("5 LAST")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	// Pipeline.Process returns error for reject, so BDAT returns 451
	if !strings.Contains(response, "451") {
		t.Errorf("Expected 451 for pipeline reject in BDAT, got: %q", response)
	}
}

func TestHandleBDAT_LastWithPipelineQuarantine(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&quarantineStage{})
	session.server.pipeline = pipeline

	chunkData := "HELLO"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	err = session.handleBDAT("5 LAST")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "250") {
		t.Errorf("Expected 250 for quarantined BDAT message, got: %q", response)
	}

	if !strings.Contains(string(deliveredData), "X-Spam-Status") {
		t.Errorf("Expected X-Spam-Status header in BDAT delivered data, got: %q", string(deliveredData[:200]))
	}
}

func TestHandleBDAT_LastWithDeliveryError(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	setupBDATSessionState(session)

	session.server.onDeliver = func(from string, to []string, data []byte) error {
		return fmt.Errorf("delivery failed")
	}

	chunkData := "HELLO"
	_, err := clientConn.Write([]byte(chunkData))
	if err != nil {
		t.Fatalf("Failed to write chunk data: %v", err)
	}

	err = session.handleBDAT("5 LAST")
	if err != nil {
		t.Fatalf("handleBDAT returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "451") {
		t.Errorf("Expected 451 for delivery error in BDAT, got: %q", response)
	}
}

func TestHandleBDAT_ReadChunkError(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()

	setupBDATSessionState(session)

	// Close client side so io.ReadFull fails immediately
	clientConn.Close()

	// Request 100 bytes but connection is closed
	err := session.handleBDAT("100")
	if err == nil {
		t.Error("Expected error when io.ReadFull fails")
	}
}

// ---------------------------------------------------------------------------
// handleAUTH additional paths
// ---------------------------------------------------------------------------

func TestHandleAUTH_EncryptionRequired(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Not TLS and not AllowInsecure
	session.server.config.AllowInsecure = false
	session.mutex.Lock()
	session.isTLS = false
	session.state = StateGreeted
	session.mutex.Unlock()

	err := session.handleAUTH("PLAIN credentials")
	if err != nil {
		t.Fatalf("handleAUTH returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "538") {
		t.Errorf("Expected 538 encryption required, got: %q", response)
	}
}

func TestHandleAUTH_AlreadyAuthenticated(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	session.server.config.AllowInsecure = true
	session.mutex.Lock()
	session.isAuth = true
	session.state = StateGreeted
	session.mutex.Unlock()

	err := session.handleAUTH("PLAIN credentials")
	if err != nil {
		t.Fatalf("handleAUTH returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 already authenticated, got: %q", response)
	}
}

func TestHandleAUTH_BadSequenceNewState(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	session.server.config.AllowInsecure = true
	// StateNew — should fail

	err := session.handleAUTH("PLAIN credentials")
	if err != nil {
		t.Fatalf("handleAUTH returned error: %v", err)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, _ := clientConn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "503") {
		t.Errorf("Expected 503 bad sequence for AUTH in NEW state, got: %q", response)
	}
}

// ---------------------------------------------------------------------------
// Session.Read tests
// ---------------------------------------------------------------------------

func TestSessionRead(t *testing.T) {
	session, clientConn := newBDATTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Write data from client side
	testData := []byte("hello world")
	_, err := clientConn.Write(testData)
	if err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	// Read from session (which wraps the server-side connection)
	buf := make([]byte, len(testData))
	session.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := session.Read(buf)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Expected %d bytes read, got %d", len(testData), n)
	}
	if string(buf) != string(testData) {
		t.Errorf("Expected %q, got %q", string(testData), string(buf))
	}
}

// ---------------------------------------------------------------------------
// handleDATA with pipeline — custom rejection code/message
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineRejectCustomCode(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	// Pipeline.Process returns (ResultReject, error) for reject stages.
	// handleDATA checks err first and returns 451. This tests that path.
	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&rejectStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.Contains(resp, "451") {
		t.Errorf("Expected 451 for pipeline error, got: %s", resp)
	}
}

// ---------------------------------------------------------------------------
// handleDATA with pipeline — SPF/DKIM/DMARC auth results headers
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineAuthResultsHeaders(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&authResultsStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done

	reader.ReadString('\n') // 250

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "Authentication-Results:") {
		t.Errorf("Expected Authentication-Results header, got: %q", dataStr[:300])
	}
	if !strings.Contains(dataStr, "spf=pass") {
		t.Errorf("Expected spf=pass in Authentication-Results, got: %q", dataStr[:300])
	}
}

// ---------------------------------------------------------------------------
// handleDATA with pipeline — spam score header
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineSpamScoreHeader(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&spamScoreStage{})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done

	reader.ReadString('\n') // 250

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "X-Spam-Score:") {
		t.Errorf("Expected X-Spam-Score header, got: %q", dataStr[:300])
	}
}

// ---------------------------------------------------------------------------
// handleDATA with pipeline — TLS session Received header
// ---------------------------------------------------------------------------

func TestHandleDATA_PipelineTLSReceivedHeader(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()
	defer clientConn.Close()

	var deliveredData []byte
	session.server.onDeliver = func(from string, to []string, data []byte) error {
		deliveredData = data
		return nil
	}

	// Mark session as TLS so proto should be ESMTPS
	session.mutex.Lock()
	session.isTLS = true
	session.mutex.Unlock()

	logger := &testLogger{}
	pipeline := NewPipeline(logger)
	pipeline.AddStage(&testStage{name: "Test"})
	session.server.pipeline = pipeline

	done := make(chan error, 1)
	go func() {
		done <- session.handleDATA()
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(clientConn)
	reader.ReadString('\n') // 354
	clientConn.Write([]byte("Subject: Test\r\n\r\nBody\r\n.\r\n"))
	<-done

	reader.ReadString('\n') // 250

	if deliveredData == nil {
		t.Fatal("Expected delivered data")
	}
	dataStr := string(deliveredData)
	if !strings.Contains(dataStr, "with ESMTPS") {
		t.Errorf("Expected 'with ESMTPS' in Received header for TLS session, got: %q", dataStr[:300])
	}
}

// ---------------------------------------------------------------------------
// handleDATA — readData error
// ---------------------------------------------------------------------------

func TestHandleDATA_ReadDataError(t *testing.T) {
	session, clientConn := newDataTestSession(t)
	defer session.Close()

	// Close client connection so readData gets an error.
	// The 354 write may succeed or fail depending on buffering,
	// and the 451 write after readData failure may also fail.
	// The important thing is that handleDATA doesn't panic.
	clientConn.Close()

	_ = session.handleDATA()
	// We just verify it doesn't panic; the exact error depends on timing.
}

// ---------------------------------------------------------------------------
// Helper types for tests
// ---------------------------------------------------------------------------

// quarantineStage always returns ResultQuarantine
type quarantineStage struct{}

func (s *quarantineStage) Name() string { return "QuarantineTest" }
func (s *quarantineStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SpamScore = 5.5
	return ResultQuarantine
}

// authResultsStage sets SPF/DKIM/DMARC results for header generation
type authResultsStage struct{}

func (s *authResultsStage) Name() string { return "AuthResults" }
func (s *authResultsStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SPFResult = SPFResult{Result: "pass", Domain: "example.com"}
	ctx.DKIMResult = DKIMResult{Valid: true, Domain: "example.com"}
	ctx.DMARCResult = DMARCResult{Result: "pass", Policy: "none"}
	return ResultAccept
}

// spamScoreStage sets a spam score > 0 for X-Spam-Score header generation
type spamScoreStage struct{}

func (s *spamScoreStage) Name() string { return "SpamScore" }
func (s *spamScoreStage) Process(ctx *MessageContext) PipelineResult {
	ctx.SpamResult.Score = 4.2
	ctx.SpamScore = 0 // keep below quarantine threshold
	return ResultAccept
}

// generateTestCert creates a self-signed TLS certificate for testing
func generateTestCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// ---------- handleRCPT coverage ----------

func TestHandleRCPT_BadSequence(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("RCPT TO:<test@example.com>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "503") {
		t.Errorf("Expected 503 for RCPT without MAIL, got: %q", resp)
	}
}

func TestHandleRCPT_SyntaxError(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.state = StateMailFrom

	go func() {
		s.HandleCommand("RCPT TO:invalid-no-angle-brackets")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for invalid RCPT syntax, got: %q", resp)
	}
}

func TestHandleRCPT_Success(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.state = StateMailFrom

	go func() {
		s.HandleCommand("RCPT TO:<rcpt@example.com>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for valid RCPT, got: %q", resp)
	}
}

func TestHandleRCPT_MultipleRecipients(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.state = StateMailFrom

	go func() {
		s.HandleCommand("RCPT TO:<rcpt1@example.com>")
	}()
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader.ReadString('\n')

	go func() {
		s.HandleCommand("RCPT TO:<rcpt2@example.com>")
	}()
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for second RCPT, got: %q", resp)
	}
}

func createSessionWithPipe(t *testing.T) (*Session, net.Conn, *bufio.Reader) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	server := NewServer(&Config{
		Hostname:       "testhost",
		MaxMessageSize: 1024 * 1024,
		MaxRecipients:  100,
	}, nil)
	s := NewSession(serverConn, server)
	reader := bufio.NewReader(clientConn)
	return s, clientConn, reader
}

// ---------------------------------------------------------------------------
// handleAuthLOGIN invalid base64 tests
// ---------------------------------------------------------------------------

func TestHandleAuthLOGIN_InvalidBase64Username(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Read the 334 VXNlcm5hbWU6 (base64 "Username:") prompt
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read username prompt: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 prompt for username, got: %q", resp)
	}

	// Send invalid base64 for username
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("!!!invalid-base64!!!\r\n"))

	// Read the 501 error response (must read before <-done to avoid net.Pipe deadlock)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "501") {
		t.Errorf("Expected 501 for invalid base64 username, got: %q", resp2)
	}

	// Wait for handleAuthLOGIN to finish
	<-done
}

func TestHandleAuthLOGIN_InvalidBase64Password(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Read the 334 username prompt
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read username prompt: %v", err)
	}
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 prompt, got: %q", resp)
	}

	// Send valid base64 username
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("dXNlcm5hbWU=\r\n")) // base64("username")

	// Read the 334 UGFzc3dvcmQ6 (base64 "Password:") prompt
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read password prompt: %v", err)
	}
	if !strings.HasPrefix(resp2, "334") {
		t.Fatalf("Expected 334 prompt for password, got: %q", resp2)
	}

	// Send invalid base64 for password
	clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	clientConn.Write([]byte("!!!not-base64!!!\r\n"))

	// Read the 501 error response (must read before <-done to avoid net.Pipe deadlock)
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "501") {
		t.Errorf("Expected 501 for invalid base64 password, got: %q", resp3)
	}

	// Wait for handleAuthLOGIN to finish
	<-done
}

// ---------------------------------------------------------------------------
// handleEHLO without AllowInsecure / with STARTTLS capability
// ---------------------------------------------------------------------------

func TestHandleEHLO_NoAuthWithoutTLSWhenInsecureDisallowed(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// AllowInsecure=false, isTLS=false => AUTH should NOT be advertised
	s.server.config.AllowInsecure = false
	s.mutex.Lock()
	s.isTLS = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("EHLO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read all lines of the multi-line EHLO response
	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO response: %v", err)
		}
		fullResp += line
		// Last line uses "250 " (space, not dash) after the code
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if strings.Contains(fullResp, "AUTH") {
		t.Errorf("AUTH capability should NOT be advertised when AllowInsecure=false and isTLS=false, got: %q", fullResp)
	}
}

func TestHandleEHLO_AuthAdvertisedWhenAllowInsecure(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// AllowInsecure=true, isTLS=false => AUTH SHOULD be advertised
	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.isTLS = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("EHLO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO response: %v", err)
		}
		fullResp += line
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if !strings.Contains(fullResp, "AUTH PLAIN LOGIN") {
		t.Errorf("AUTH capability should be advertised when AllowInsecure=true, got: %q", fullResp)
	}
}

func TestHandleEHLO_STARTTLSCapability(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// Set TLSConfig but isTLS=false => STARTTLS should be advertised
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}
	s.server.config.TLSConfig = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	s.mutex.Lock()
	s.isTLS = false
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("EHLO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO response: %v", err)
		}
		fullResp += line
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if !strings.Contains(fullResp, "STARTTLS") {
		t.Errorf("STARTTLS capability should be advertised when TLSConfig is set and isTLS=false, got: %q", fullResp)
	}
}

func TestHandleEHLO_NoSTARTTLSWhenAlreadyTLS(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// Set TLSConfig AND isTLS=true => STARTTLS should NOT be advertised
	cert, err := generateTestCert()
	if err != nil {
		t.Fatalf("Failed to generate test cert: %v", err)
	}
	s.server.config.TLSConfig = &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
	s.mutex.Lock()
	s.isTLS = true
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("EHLO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var fullResp string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read EHLO response: %v", err)
		}
		fullResp += line
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if strings.Contains(fullResp, "STARTTLS") {
		t.Errorf("STARTTLS should NOT be advertised when isTLS=true, got: %q", fullResp)
	}
}

// ---------------------------------------------------------------------------
// handleMAIL with invalid email
// ---------------------------------------------------------------------------

func TestHandleMAIL_InvalidEmail(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// Session must be in StateGreeted to accept MAIL FROM
	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL FROM:<@>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for MAIL FROM:<@> (invalid email), got: %q", resp)
	}
}

func TestHandleMAIL_BadSequenceNewState(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	// Session is in StateNew — MAIL should fail with 503
	go func() {
		s.HandleCommand("MAIL FROM:<user@example.com>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "503") {
		t.Errorf("Expected 503 for MAIL in StateNew, got: %q", resp)
	}
}

func TestHandleMAIL_EmptySender(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL FROM:<>")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for empty sender (bounce), got: %q", resp)
	}
}

// ---------------------------------------------------------------------------
// Additional coverage tests for session.go
// ---------------------------------------------------------------------------

func TestHandleEHLO_EmptyArg(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("EHLO")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for EHLO with no argument, got: %q", resp)
	}
}

func TestHandleHELO_Success(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("HELO testclient")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for valid HELO, got: %q", resp)
	}
}

func TestHandleHELO_EmptyArg(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("HELO")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for HELO with no argument, got: %q", resp)
	}
}

func TestHandleCommand_UnknownCommand(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("XYZZY")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "500") {
		t.Errorf("Expected 500 for unknown command, got: %q", resp)
	}
}

func TestHandleAUTH_UnrecognizedMechanism(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("AUTH XYZ")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "504") {
		t.Errorf("Expected 504 for unrecognized auth mechanism, got: %q", resp)
	}
}

func TestHandleAuthPLAIN_InvalidBase64(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN !!!invalid!!!")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for invalid PLAIN base64, got: %q", resp)
	}
	<-done
}

func TestHandleAuthPLAIN_BadCredentialFormat(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	// Valid base64 but wrong format (no \x00 separators)
	encoded := base64.StdEncoding.EncodeToString([]byte("just-a-string"))

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + encoded)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "501") {
		t.Errorf("Expected 501 for bad PLAIN credential format, got: %q", resp)
	}
	<-done
}

func TestHandleAuthPLAIN_AuthFails(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	// Set up auth handler that rejects
	s.server.onAuth = func(username, password string) (bool, error) {
		return false, nil
	}

	// PLAIN format: \0username\0password
	encoded := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00wrongpass"))

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH PLAIN " + encoded)
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "535") {
		t.Errorf("Expected 535 for failed auth, got: %q", resp)
	}
	<-done
}

func TestHandleAuthLOGIN_Success(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.AllowInsecure = true
	s.mutex.Lock()
	s.state = StateGreeted
	s.isTLS = false
	s.mutex.Unlock()

	s.server.onAuth = func(username, password string) (bool, error) {
		if username == "testuser" && password == "testpass" {
			return true, nil
		}
		return false, nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("AUTH LOGIN")
	}()

	// Read username prompt
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "334") {
		t.Fatalf("Expected 334 prompt, got: %q", resp)
	}

	// Send base64-encoded username
	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testuser")) + "\r\n"))

	// Read password prompt
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "334") {
		t.Fatalf("Expected 334 password prompt, got: %q", resp2)
	}

	// Send base64-encoded password
	clientConn.Write([]byte(base64.StdEncoding.EncodeToString([]byte("testpass")) + "\r\n"))

	// Read 235 success response
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp3, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp3, "235") {
		t.Errorf("Expected 235 auth success, got: %q", resp3)
	}
	<-done
}

func TestHandleRSET(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("RSET")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for RSET, got: %q", resp)
	}
}

func TestHandleVRFY(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("VRFY user@example.com")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "252") {
		t.Errorf("Expected 252 for VRFY, got: %q", resp)
	}
}

func TestHandleEXPN(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("EXPN list@example.com")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "550") {
		t.Errorf("Expected 550 for EXPN, got: %q", resp)
	}
}

func TestHandleHELP(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("HELP")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "214") {
		t.Errorf("Expected 214 for HELP, got: %q", resp)
	}
}

func TestHandleNOOP(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	go func() {
		s.HandleCommand("NOOP")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for NOOP, got: %q", resp)
	}
}

func TestHandleQUIT(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	done := make(chan error, 1)
	go func() {
		done <- s.HandleCommand("QUIT")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "221") {
		t.Errorf("Expected 221 for QUIT, got: %q", resp)
	}
	<-done
}

func TestHandleRCPT_MaxRecipients(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.MaxRecipients = 2
	s.mutex.Lock()
	s.state = StateMailFrom
	s.mutex.Unlock()

	// First recipient
	go func() {
		s.HandleCommand("RCPT TO:<a@example.com>")
	}()
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader.ReadString('\n')

	// Second recipient
	go func() {
		s.HandleCommand("RCPT TO:<b@example.com>")
	}()
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader.ReadString('\n')

	// Third recipient - should fail with 452
	go func() {
		s.HandleCommand("RCPT TO:<c@example.com>")
	}()
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "452") {
		t.Errorf("Expected 452 for too many recipients, got: %q", resp)
	}
}

func TestHandleMAIL_WithSIZEParameter(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.mutex.Lock()
	s.state = StateGreeted
	s.mutex.Unlock()

	go func() {
		s.HandleCommand("MAIL FROM:<user@example.com> SIZE=1024")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "250") {
		t.Errorf("Expected 250 for MAIL with SIZE param, got: %q", resp)
	}
}

func TestParseCommand_EmptyLine(t *testing.T) {
	cmd, arg := parseCommand("")
	if cmd != "" || arg != "" {
		t.Errorf("Expected empty cmd/arg for empty line, got cmd=%q arg=%q", cmd, arg)
	}
}

func TestWriteResponse_WithTimeout(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.WriteTimeout = 5 * time.Second

	go func() {
		s.WriteResponse(220, "test")
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp, "220") {
		t.Errorf("Expected 220, got: %q", resp)
	}
}

func TestWriteMultiLineResponse_WithTimeout(t *testing.T) {
	s, clientConn, reader := createSessionWithPipe(t)
	defer clientConn.Close()

	s.server.config.WriteTimeout = 5 * time.Second

	go func() {
		s.WriteMultiLineResponse(250, []string{"line1", "line2"})
	}()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp1, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp1, "250-line1") {
		t.Errorf("Expected '250-line1', got: %q", resp1)
	}

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(resp2, "250 line2") {
		t.Errorf("Expected '250 line2', got: %q", resp2)
	}
}

func TestSessionClose(t *testing.T) {
	s, clientConn, _ := createSessionWithPipe(t)
	// Close the client side first, then session
	clientConn.Close()
	err := s.Close()
	if err != nil {
		t.Errorf("Expected no error on Close, got: %v", err)
	}
}
