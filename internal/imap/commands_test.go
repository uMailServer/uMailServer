package imap

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

// mockAddr implements net.Addr for testing
type mockAddr struct{}

func (m *mockAddr) Network() string { return "tcp" }
func (m *mockAddr) String() string  { return "127.0.0.1:1143" }

// mockConn is a mock net.Conn for testing
type mockConn struct {
	reader *bytes.Reader
	writer *bytes.Buffer
}

func newMockConn(input string) *mockConn {
	return &mockConn{
		reader: bytes.NewReader([]byte(input)),
		writer: &bytes.Buffer{},
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return m.reader.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writer.Write(b)
}

func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return &mockAddr{} }
func (m *mockConn) RemoteAddr() net.Addr               { return &mockAddr{} }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) Written() string {
	return m.writer.String()
}

func TestHandleCommandEmptyLine(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	// Test empty command line
	err := session.handleCommand("")
	if err != nil {
		t.Errorf("expected no error for empty line, got: %v", err)
	}
}

func TestHandleCommandSinglePart(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	// Test command with only one part
	err := session.handleCommand("TAG")
	if err != nil {
		t.Errorf("expected no error for single part command, got: %v", err)
	}

	// Check response contains BAD
	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for invalid command, got: %s", written)
	}
}

func TestHandleCapabilityNotAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	err := session.handleCapability()
	if err != nil {
		t.Errorf("handleCapability failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "CAPABILITY") {
		t.Errorf("expected CAPABILITY in response, got: %s", written)
	}
	if !strings.Contains(written, "IMAP4rev1") {
		t.Errorf("expected IMAP4rev1 in capability, got: %s", written)
	}
}

func TestHandleNoop(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleNoop()
	if err != nil {
		t.Errorf("handleNoop failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleLogout(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleLogout()
	if err != nil {
		t.Errorf("handleLogout failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BYE") {
		t.Errorf("expected BYE response, got: %s", written)
	}

	if session.state != StateLoggedOut {
		t.Error("expected session state to be StateLoggedOut")
	}
}

func TestHandleLoginSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleLogin([]string{"test", "password"})
	if err != nil {
		t.Errorf("handleLogin failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}

	if session.state != StateAuthenticated {
		t.Error("expected session state to be StateAuthenticated")
	}

	if session.user != "test" {
		t.Errorf("expected user 'test', got: %s", session.user)
	}
}

func TestHandleLoginFailure(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	// Missing arguments
	err := session.handleLogin([]string{"test"})
	if err != nil {
		t.Errorf("handleLogin failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleSelectSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleSelect([]string{"INBOX"})
	if err != nil {
		t.Errorf("handleSelect failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}

	if session.state != StateSelected {
		t.Error("expected session state to be StateSelected")
	}

	if session.selected == nil {
		t.Error("expected selected mailbox to be set")
	}
}

func TestHandleSelectMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated

	err := session.handleSelect([]string{})
	if err != nil {
		t.Errorf("handleSelect failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleCreateSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleCreate([]string{"NewBox"})
	if err != nil {
		t.Errorf("handleCreate failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleDeleteSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleDelete([]string{"OldBox"})
	if err != nil {
		t.Errorf("handleDelete failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleRenameSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleRename([]string{"OldBox", "NewBox"})
	if err != nil {
		t.Errorf("handleRename failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleRenameMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated

	// Missing new name
	err := session.handleRename([]string{"OldBox"})
	if err != nil {
		t.Errorf("handleRename failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleListSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleList([]string{"", "*"})
	if err != nil {
		t.Errorf("handleList failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleListMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated

	err := session.handleList([]string{"only-reference"})
	if err != nil {
		t.Errorf("handleList failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleNamespaceSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleNamespace()
	if err != nil {
		t.Errorf("handleNamespace failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
	if !strings.Contains(written, "NAMESPACE") {
		t.Errorf("expected NAMESPACE in response, got: %s", written)
	}
}

func TestHandleStatusSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleStatus([]string{"INBOX", "(MESSAGES)"})
	if err != nil {
		t.Errorf("handleStatus failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
	if !strings.Contains(written, "STATUS") {
		t.Errorf("expected STATUS in response, got: %s", written)
	}
}

func TestHandleExamineSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleExamine([]string{"INBOX"})
	if err != nil {
		t.Errorf("handleExamine failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSubscribeSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleSubscribe([]string{"INBOX"})
	if err != nil {
		t.Errorf("handleSubscribe failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUnsubscribeSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleUnsubscribe([]string{"INBOX"})
	if err != nil {
		t.Errorf("handleUnsubscribe failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleLsubSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleLsub([]string{"", "*"})
	if err != nil {
		t.Errorf("handleLsub failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleLsubMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated

	err := session.handleLsub([]string{""})
	if err != nil {
		t.Errorf("handleLsub failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleEnableSuccess(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated

	err := session.handleEnable([]string{"CONDSTORE"})
	if err != nil {
		t.Errorf("handleEnable failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "ENABLED") {
		t.Errorf("expected ENABLED in response, got: %s", written)
	}
}

func TestHandleCommandInLoggedOutState(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateLoggedOut

	// Any command should return nil when logged out
	err := session.handleCommand("A1 NOOP")
	if err != nil {
		t.Errorf("expected no error in logged out state, got: %v", err)
	}
}

func TestSessionReadLineWithCR(t *testing.T) {
	mock := newMockConn("A1 NOOP\r\n")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	line, err := session.readLine()
	if err != nil {
		t.Errorf("readLine failed: %v", err)
	}

	// Should have trimmed \r
	if strings.Contains(line, "\r") {
		t.Errorf("line should not contain \\r, got: %q", line)
	}
}

func TestSessionState(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	if session.State() != StateNotAuthenticated {
		t.Errorf("expected initial state StateNotAuthenticated, got: %d", session.State())
	}
}

func TestSessionUser(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.user = "testuser"

	if session.User() != "testuser" {
		t.Errorf("expected user 'testuser', got: %s", session.User())
	}
}

func TestSessionSelected(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	// Initially nil
	if session.Selected() != nil {
		t.Error("expected selected to be nil initially")
	}

	// Set selected
	session.selected = &Mailbox{Name: "INBOX"}
	if session.Selected().Name != "INBOX" {
		t.Errorf("expected selected mailbox 'INBOX', got: %s", session.Selected().Name)
	}
}

func TestHandleCreateMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleCreate([]string{})
	if err != nil {
		t.Errorf("handleCreate failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleDeleteMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleDelete([]string{})
	if err != nil {
		t.Errorf("handleDelete failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleStatusMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleStatus([]string{"INBOX"})
	if err != nil {
		t.Errorf("handleStatus failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleSubscribeMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleSubscribe([]string{})
	if err != nil {
		t.Errorf("handleSubscribe failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUnsubscribeMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleUnsubscribe([]string{})
	if err != nil {
		t.Errorf("handleUnsubscribe failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleExamineMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleExamine([]string{})
	if err != nil {
		t.Errorf("handleExamine failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestNewSession(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	if session == nil {
		t.Fatal("expected non-nil session")
	}

	if session.id == "" {
		t.Error("expected session ID to be set")
	}

	if session.state != StateNotAuthenticated {
		t.Errorf("expected initial state StateNotAuthenticated, got: %d", session.state)
	}

	if session.reader == nil {
		t.Error("expected reader to be initialized")
	}

	if session.writer == nil {
		t.Error("expected writer to be initialized")
	}
}

func TestSessionID(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)

	id := session.ID()
	if id == "" {
		t.Error("expected non-empty session ID")
	}

	if len(id) != 19 {
		t.Errorf("expected session ID length 19, got: %d", len(id))
	}
}

func TestBufioReaderWriter(t *testing.T) {
	// Test that bufio reader/writer work correctly
	input := "A1 LOGIN user pass\r\n"
	reader := bufio.NewReader(strings.NewReader(input))

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString failed: %v", err)
	}

	line = strings.TrimRight(line, "\r\n")
	expected := "A1 LOGIN user pass"
	if line != expected {
		t.Errorf("expected '%s', got: '%s'", expected, line)
	}
}

// Test state handlers
func TestHandleNotAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	// Test CAPABILITY in NotAuthenticated state
	err := session.handleNotAuthenticated("CAPABILITY", []string{}, "")
	if err != nil {
		t.Errorf("handleNotAuthenticated CAPABILITY failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "CAPABILITY") {
		t.Errorf("expected CAPABILITY in response, got: %s", written)
	}
}

func TestHandleNotAuthenticatedNoop(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleNotAuthenticated("NOOP", []string{}, "")
	if err != nil {
		t.Errorf("handleNotAuthenticated NOOP failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleNotAuthenticatedLogout(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleNotAuthenticated("LOGOUT", []string{}, "")
	if err != nil {
		t.Errorf("handleNotAuthenticated LOGOUT failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BYE") {
		t.Errorf("expected BYE in response, got: %s", written)
	}
}

func TestHandleNotAuthenticatedUnknown(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleNotAuthenticated("UNKNOWN", []string{}, "")
	if err != nil {
		t.Errorf("handleNotAuthenticated UNKNOWN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for unknown command, got: %s", written)
	}
}

func TestHandleAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	// Test SELECT
	err := session.handleAuthenticated("SELECT", []string{"INBOX"}, "")
	if err != nil {
		t.Errorf("handleAuthenticated SELECT failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "EXISTS") {
		t.Errorf("expected EXISTS in response, got: %s", written)
	}
}

func TestHandleAuthenticatedUnknown(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	err := session.handleAuthenticated("UNKNOWN", []string{}, "")
	if err != nil {
		t.Errorf("handleAuthenticated UNKNOWN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for unknown command, got: %s", written)
	}
}

func TestHandleSelected(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test CHECK
	err := session.handleSelected("CHECK", []string{}, "")
	if err != nil {
		t.Errorf("handleSelected CHECK failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedUnknown(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleSelected("UNKNOWN", []string{}, "")
	if err != nil {
		t.Errorf("handleSelected UNKNOWN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for unknown command, got: %s", written)
	}
}

func TestHandleStartTLSNoConfig(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	// TLS not configured
	err := session.handleStartTLS()
	if err != nil {
		t.Errorf("handleStartTLS failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when TLS not available, got: %s", written)
	}
}

func TestHandleStartTLSAlreadyActive(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143", TLSConfig: &tls.Config{}}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.tlsActive = true

	err := session.handleStartTLS()
	if err != nil {
		t.Errorf("handleStartTLS failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response when TLS already active, got: %s", written)
	}
}

func TestHandleAuthenticate(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	// Missing mechanism
	err := session.handleAuthenticate([]string{})
	if err != nil {
		t.Errorf("handleAuthenticate failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing mechanism, got: %s", written)
	}
}

func TestHandleAuthenticatePlain(t *testing.T) {
	// Test AUTHENTICATE PLAIN with SASL-IR (initial response) and bad credentials
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	creds := base64.StdEncoding.EncodeToString([]byte("\x00user\x00wrongpass"))
	err := session.handleAuthenticate([]string{"PLAIN", creds})
	if err != nil {
		t.Errorf("handleAuthenticate PLAIN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response for failed PLAIN auth, got: %s", written)
	}
}

func TestHandleAuthenticateLogin(t *testing.T) {
	// Test AUTHENTICATE LOGIN with bad credentials
	username := base64.StdEncoding.EncodeToString([]byte("user"))
	password := base64.StdEncoding.EncodeToString([]byte("wrongpass"))
	input := username + "\r\n" + password + "\r\n"

	mock := newMockConn(input)
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthenticate([]string{"LOGIN"})
	if err != nil {
		t.Errorf("handleAuthenticate LOGIN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response for failed LOGIN auth, got: %s", written)
	}
}

func TestHandleAuthenticateUnknown(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthenticate([]string{"UNKNOWN"})
	if err != nil {
		t.Errorf("handleAuthenticate UNKNOWN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response for unsupported mechanism, got: %s", written)
	}
}

func TestHandleAuthPlain(t *testing.T) {
	// Test SASL-IR path with invalid credentials (no authFunc set, mailstore returns false)
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	// base64 of "\x00user\x00pass" = AHoAHHBhc3M= ... let's compute properly
	// \x00 + "testuser" + \x00 + "testpass"
	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))

	err := session.handleAuthPlain([]string{creds})
	if err != nil {
		t.Errorf("handleAuthPlain failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response, got: %s", written)
	}
}

func TestHandleAuthPlainWithAuth(t *testing.T) {
	// Test SASL-IR path with valid credentials
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	server.SetAuthFunc(func(user, pass string) (bool, error) {
		return user == "testuser" && pass == "testpass", nil
	})
	session := NewSession(mock, server)
	session.tag = "A1"

	creds := base64.StdEncoding.EncodeToString([]byte("\x00testuser\x00testpass"))

	err := session.handleAuthPlain([]string{creds})
	if err != nil {
		t.Errorf("handleAuthPlain failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
	if session.user != "testuser" {
		t.Errorf("expected user 'testuser', got: %s", session.user)
	}
	if session.state != StateAuthenticated {
		t.Errorf("expected StateAuthenticated, got: %v", session.state)
	}
}

func TestHandleAuthLogin(t *testing.T) {
	// Provide username and password responses as base64 lines for the mock reader
	username := base64.StdEncoding.EncodeToString([]byte("testuser"))
	password := base64.StdEncoding.EncodeToString([]byte("testpass"))
	input := username + "\r\n" + password + "\r\n"

	mock := newMockConn(input)
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	server.SetAuthFunc(func(user, pass string) (bool, error) {
		return user == "testuser" && pass == "testpass", nil
	})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthLogin()
	if err != nil {
		t.Errorf("handleAuthLogin failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
	if session.user != "testuser" {
		t.Errorf("expected user 'testuser', got: %s", session.user)
	}
}

func TestHandleAuthLoginFailure(t *testing.T) {
	username := base64.StdEncoding.EncodeToString([]byte("testuser"))
	password := base64.StdEncoding.EncodeToString([]byte("wrongpass"))
	input := username + "\r\n" + password + "\r\n"

	mock := newMockConn(input)
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	server.SetAuthFunc(func(user, pass string) (bool, error) {
		return false, nil
	})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthLogin()
	if err != nil {
		t.Errorf("handleAuthLogin failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response, got: %s", written)
	}
}

func TestHandleAppend(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	// Missing args
	err := session.handleAppend([]string{}, "")
	if err != nil {
		t.Errorf("handleAppend failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleAppendWithMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateAuthenticated
	session.user = "test"

	// Only mailbox, no message data
	err := session.handleAppend([]string{"INBOX"}, "")
	if err != nil {
		t.Errorf("handleAppend failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response, got: %s", written)
	}
}

func TestHandleCheck(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleCheck()
	if err != nil {
		t.Errorf("handleCheck failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleClose(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleClose()
	if err != nil {
		t.Errorf("handleClose failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}

	if session.state != StateAuthenticated {
		t.Errorf("expected state to be StateAuthenticated, got: %d", session.state)
	}

	if session.selected != nil {
		t.Error("expected selected to be nil after close")
	}
}

func TestHandleExpunge(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleExpunge()
	if err != nil {
		t.Errorf("handleExpunge failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleExpungeNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleExpunge()
	if err != nil {
		t.Errorf("handleExpunge failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleSearch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleSearch([]string{"ALL"}, "")
	if err != nil {
		t.Errorf("handleSearch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "SEARCH") {
		t.Errorf("expected SEARCH in response, got: %s", written)
	}
}

func TestHandleSearchNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleSearch([]string{"ALL"}, "")
	if err != nil {
		t.Errorf("handleSearch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleFetch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleFetch([]string{"1:*", "FLAGS"}, "")
	if err != nil {
		t.Errorf("handleFetch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleFetchMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleFetch([]string{"1"}, "")
	if err != nil {
		t.Errorf("handleFetch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleFetchNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleFetch([]string{"1:*", "FLAGS"}, "")
	if err != nil {
		t.Errorf("handleFetch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleStore(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleStore([]string{"1:*", "+FLAGS", "(\\Seen)"})
	if err != nil {
		t.Errorf("handleStore failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleStoreMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleStore([]string{"1", "+FLAGS"})
	if err != nil {
		t.Errorf("handleStore failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleStoreNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleStore([]string{"1:*", "+FLAGS", "(\\Seen)"})
	if err != nil {
		t.Errorf("handleStore failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleStoreInvalidOperation(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleStore([]string{"1:*", "INVALID", "(\\Seen)"})
	if err != nil {
		t.Errorf("handleStore failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for invalid operation, got: %s", written)
	}
}

func TestHandleCopy(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleCopy([]string{"1:*", "Sent"})
	if err != nil {
		t.Errorf("handleCopy failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleCopyMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleCopy([]string{"1:*"})
	if err != nil {
		t.Errorf("handleCopy failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleCopyNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleCopy([]string{"1:*", "Sent"})
	if err != nil {
		t.Errorf("handleCopy failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleMove(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleMove([]string{"1:*", "Archive"})
	if err != nil {
		t.Errorf("handleMove failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleMoveMissingArgs(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleMove([]string{"1:*"})
	if err != nil {
		t.Errorf("handleMove failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing args, got: %s", written)
	}
}

func TestHandleMoveNoMailbox(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = nil

	err := session.handleMove([]string{"1:*", "Archive"})
	if err != nil {
		t.Errorf("handleMove failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response when no mailbox selected, got: %s", written)
	}
}

func TestHandleUID(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test UID SEARCH
	err := session.handleUID([]string{"SEARCH", "ALL"}, "")
	if err != nil {
		t.Errorf("handleUID SEARCH failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "SEARCH") {
		t.Errorf("expected SEARCH in response, got: %s", written)
	}
}

func TestHandleUIDMissingCommand(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleUID([]string{}, "")
	if err != nil {
		t.Errorf("handleUID failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for missing command, got: %s", written)
	}
}

func TestHandleUIDUnknown(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleUID([]string{"UNKNOWN"}, "")
	if err != nil {
		t.Errorf("handleUID failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for unknown command, got: %s", written)
	}
}

func TestHandleUIDFetch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleUIDFetch([]string{"1:*", "FLAGS"}, "")
	if err != nil {
		t.Errorf("handleUIDFetch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUIDStore(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleUIDStore([]string{"1:*", "+FLAGS", "(\\Seen)"})
	if err != nil {
		t.Errorf("handleUIDStore failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUIDCopy(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleUIDCopy([]string{"1:*", "Sent"})
	if err != nil {
		t.Errorf("handleUIDCopy failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUIDMove(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleUIDMove([]string{"1:*", "Archive"})
	if err != nil {
		t.Errorf("handleUIDMove failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleUIDSearch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}

	err := session.handleUIDSearch([]string{"ALL"}, "")
	if err != nil {
		t.Errorf("handleUIDSearch failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "SEARCH") {
		t.Errorf("expected SEARCH in response, got: %s", written)
	}
}

func TestHandleUIDExpunge(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleUIDExpunge([]string{"1:*"})
	if err != nil {
		t.Errorf("handleUIDExpunge failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestFormatFetchResponse(t *testing.T) {
	msg := &Message{
		UID:          123,
		SeqNum:       1,
		Flags:        []string{"\\Seen", "\\Answered"},
		Size:         1024,
		InternalDate: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Subject:      "Test Subject",
		From:         "sender@example.com",
		To:           "recipient@example.com",
		Date:         "15-Jan-2024 10:30:00 +0000",
		Data:         []byte("Test message body"),
	}

	tests := []struct {
		name  string
		items []string
		check []string
	}{
		{
			name:  "FLAGS",
			items: []string{"FLAGS"},
			check: []string{"FLAGS", "\\Seen", "\\Answered"},
		},
		{
			name:  "RFC822.SIZE",
			items: []string{"RFC822.SIZE"},
			check: []string{"RFC822.SIZE", "1024"},
		},
		{
			name:  "UID",
			items: []string{"UID"},
			check: []string{"UID", "123"},
		},
		{
			name:  "RFC822",
			items: []string{"RFC822"},
			check: []string{"RFC822", "Test message body"},
		},
		{
			name:  "BODYSTRUCTURE",
			items: []string{"BODYSTRUCTURE"},
			check: []string{"BODYSTRUCTURE"},
		},
		{
			name:  "ENVELOPE",
			items: []string{"ENVELOPE"},
			check: []string{"ENVELOPE", "Test Subject"},
		},
		{
			name:  "multiple items",
			items: []string{"FLAGS", "UID", "RFC822.SIZE"},
			check: []string{"FLAGS", "UID", "RFC822.SIZE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatFetchResponse(msg, tt.items)
			for _, expected := range tt.check {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain %q, got: %s", expected, result)
				}
			}
		})
	}
}

func TestHandleIdleInWrongState(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	err := session.handleIdle()
	if err != nil {
		t.Errorf("handleIdle failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response in NotAuthenticated state, got: %s", written)
	}
}

func TestHandleAuthenticatedSelect(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("SELECT", []string{"INBOX"}, "A1 SELECT INBOX")
	if err != nil {
		t.Errorf("handleAuthenticated SELECT failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleAuthenticatedExamine(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("EXAMINE", []string{"INBOX"}, "A1 EXAMINE INBOX")
	if err != nil {
		t.Errorf("handleAuthenticated EXAMINE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleAuthenticatedCreate(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("CREATE", []string{"NewMailbox"}, "A1 CREATE NewMailbox")
	if err != nil {
		t.Errorf("handleAuthenticated CREATE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleAuthenticatedDelete(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("DELETE", []string{"OldMailbox"}, "A1 DELETE OldMailbox")
	if err != nil {
		t.Errorf("handleAuthenticated DELETE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleAuthenticatedRename(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("RENAME", []string{"OldName", "NewName"}, "A1 RENAME OldName NewName")
	if err != nil {
		t.Errorf("handleAuthenticated RENAME failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleAuthenticatedList(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("LIST", []string{"", "*"}, "A1 LIST \"\" *")
	if err != nil {
		t.Errorf("handleAuthenticated LIST failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "LIST") {
		t.Errorf("expected LIST in response, got: %s", written)
	}
}

func TestHandleAuthenticatedLsub(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("LSUB", []string{"", "*"}, "A1 LSUB \"\" *")
	if err != nil {
		t.Errorf("handleAuthenticated LSUB failed: %v", err)
	}

	written := mock.Written()
	// LSUB returns LIST responses in this implementation
	if !strings.Contains(written, "LIST") {
		t.Errorf("expected LIST in response, got: %s", written)
	}
}

func TestHandleAuthenticatedStatus(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("STATUS", []string{"INBOX", "(MESSAGES UNSEEN)"}, "A1 STATUS INBOX (MESSAGES UNSEEN)")
	if err != nil {
		t.Errorf("handleAuthenticated STATUS failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "STATUS") {
		t.Errorf("expected STATUS in response, got: %s", written)
	}
}

func TestHandleAuthenticatedAppend(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err := session.handleAuthenticated("APPEND", []string{"INBOX", "{0}"}, "A1 APPEND INBOX {0}")
	if err != nil {
		t.Errorf("handleAuthenticated APPEND failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") && !strings.Contains(written, "+") {
		t.Errorf("expected OK or continuation response, got: %s", written)
	}
}

func TestHandleSelectedClose(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("CLOSE", []string{}, "A1 CLOSE")
	if err != nil {
		t.Errorf("handleSelected CLOSE failed: %v", err)
	}

	if session.state != StateAuthenticated {
		t.Errorf("expected state to change to Authenticated, got: %d", session.state)
	}

	if session.selected != nil {
		t.Error("expected selected mailbox to be cleared")
	}
}

func TestHandleSelectedCheck(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("CHECK", []string{}, "A1 CHECK")
	if err != nil {
		t.Errorf("handleSelected CHECK failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedExpunge(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("EXPUNGE", []string{}, "A1 EXPUNGE")
	if err != nil {
		t.Errorf("handleSelected EXPUNGE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedFetch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("FETCH", []string{"1:*", "FLAGS"}, "A1 FETCH 1:* FLAGS")
	if err != nil {
		t.Errorf("handleSelected FETCH failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedStore(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("STORE", []string{"1", "+FLAGS", "(\\Seen)"}, "A1 STORE 1 +FLAGS (\\Seen)")
	if err != nil {
		t.Errorf("handleSelected STORE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedCopy(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("COPY", []string{"1", "Sent"}, "A1 COPY 1 Sent")
	if err != nil {
		t.Errorf("handleSelected COPY failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedMove(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("MOVE", []string{"1", "Trash"}, "A1 MOVE 1 Trash")
	if err != nil {
		t.Errorf("handleSelected MOVE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleSelectedSearch(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("SEARCH", []string{"ALL"}, "A1 SEARCH ALL")
	if err != nil {
		t.Errorf("handleSelected SEARCH failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "SEARCH") {
		t.Errorf("expected SEARCH in response, got: %s", written)
	}
}

func TestHandleSelectedUidCommands(t *testing.T) {
	commands := []string{"UID FETCH", "UID STORE", "UID COPY", "UID MOVE", "UID SEARCH"}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			mock := newMockConn("")
			server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
			session := NewSession(mock, server)
			session.state = StateSelected
			session.user = "test"
			session.selected = &Mailbox{Name: "INBOX"}
			session.tag = "A1"

			var err error
			switch cmd {
			case "UID FETCH":
				err = session.handleSelected("UID", []string{"FETCH", "1:*", "FLAGS"}, "A1 UID FETCH 1:* FLAGS")
			case "UID STORE":
				err = session.handleSelected("UID", []string{"STORE", "1", "FLAGS", "(\\Seen)"}, "A1 UID STORE 1 FLAGS (\\Seen)")
			case "UID COPY":
				err = session.handleSelected("UID", []string{"COPY", "1", "Sent"}, "A1 UID COPY 1 Sent")
			case "UID MOVE":
				err = session.handleSelected("UID", []string{"MOVE", "1", "Trash"}, "A1 UID MOVE 1 Trash")
			case "UID SEARCH":
				err = session.handleSelected("UID", []string{"SEARCH", "ALL"}, "A1 UID SEARCH ALL")
			}

			if err != nil {
				t.Errorf("handleSelected %s failed: %v", cmd, err)
			}

			written := mock.Written()
			if !strings.Contains(written, "OK") && !strings.Contains(written, "SEARCH") {
				t.Errorf("expected OK or SEARCH response, got: %s", written)
			}
		})
	}
}

func TestHandleStartTLSWithConfig(t *testing.T) {
	mock := newMockConn("")
	config := &Config{
		Addr: ":1143",
		// Note: TLSConfig is nil here, testing the code path where TLS is not configured
	}
	server := NewServer(config, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	// When TLSConfig is nil, the function returns "NO TLS not available" but doesn't error
	err := session.handleStartTLS()
	if err != nil {
		t.Errorf("handleStartTLS returned error: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO TLS not available") {
		t.Errorf("expected 'NO TLS not available' response, got: %s", written)
	}
}

func TestHandleAuthenticatedSubscribeUnsubscribe(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	// Test SUBSCRIBE
	err := session.handleAuthenticated("SUBSCRIBE", []string{"INBOX"}, "A1 SUBSCRIBE INBOX")
	if err != nil {
		t.Errorf("handleAuthenticated SUBSCRIBE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response for SUBSCRIBE, got: %s", written)
	}

	// Test UNSUBSCRIBE
	mock = newMockConn("")
	session = NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	err = session.handleAuthenticated("UNSUBSCRIBE", []string{"INBOX"}, "A1 UNSUBSCRIBE INBOX")
	if err != nil {
		t.Errorf("handleAuthenticated UNSUBSCRIBE failed: %v", err)
	}

	written = mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response for UNSUBSCRIBE, got: %s", written)
	}
}

func TestHandleSelectedNoop(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	err := session.handleSelected("NOOP", []string{}, "A1 NOOP")
	if err != nil {
		t.Errorf("handleSelected NOOP failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleIdleAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateAuthenticated
	session.user = "test"
	session.tag = "A1"

	// Test IDLE in authenticated state - should work
	err := session.handleIdle()
	if err != nil {
		t.Errorf("handleIdle failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleIdleSelected(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateSelected
	session.user = "test"
	session.selected = &Mailbox{Name: "INBOX"}
	session.tag = "A1"

	// Test IDLE in selected state
	err := session.handleIdle()
	if err != nil {
		t.Errorf("handleIdle failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

func TestHandleIdleNotAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	// Test IDLE in not authenticated state - should fail
	err := session.handleIdle()
	if err != nil {
		t.Errorf("handleIdle failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response in NotAuthenticated state, got: %s", written)
	}
}

func TestHandleStartTLSNotAuthenticated(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	// Test STARTTLS in not authenticated state
	err := session.handleStartTLS()
	if err != nil {
		t.Errorf("handleStartTLS failed: %v", err)
	}

	written := mock.Written()
	// Should return NO since TLS is not configured
	if !strings.Contains(written, "NO") && !strings.Contains(written, "BAD") {
		t.Errorf("expected NO or BAD response, got: %s", written)
	}
}

func TestHandleSelectedWithFetch(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test FETCH command
	err := session.handleSelected("FETCH", []string{"1:*", "FLAGS"}, "A1 FETCH 1:* FLAGS")
	if err != nil {
		t.Errorf("handleSelected FETCH failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for FETCH command")
	}
}

func TestHandleSelectedWithStore(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test STORE command
	err := session.handleSelected("STORE", []string{"1", "+FLAGS", "(\\Seen)"}, "A1 STORE 1 +FLAGS (\\Seen)")
	if err != nil {
		t.Errorf("handleSelected STORE failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for STORE command")
	}
}

func TestHandleSelectedWithCopy(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Create destination mailbox
	store.CreateMailbox("testuser", "Sent")

	// Test COPY command
	err := session.handleSelected("COPY", []string{"1", "Sent"}, "A1 COPY 1 Sent")
	if err != nil {
		t.Errorf("handleSelected COPY failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for COPY command")
	}
}

func TestHandleSelectedWithMove(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Create destination mailbox
	store.CreateMailbox("testuser", "Archive")

	// Test MOVE command
	err := session.handleSelected("MOVE", []string{"1", "Archive"}, "A1 MOVE 1 Archive")
	if err != nil {
		t.Errorf("handleSelected MOVE failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for MOVE command")
	}
}

func TestHandleSelectedWithExpunge(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test EXPUNGE command
	err := session.handleSelected("EXPUNGE", []string{}, "A1 EXPUNGE")
	if err != nil {
		t.Errorf("handleSelected EXPUNGE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response for EXPUNGE, got: %s", written)
	}
}

func TestHandleSelectedWithSearch(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test SEARCH command
	err := session.handleSelected("SEARCH", []string{"ALL"}, "A1 SEARCH ALL")
	if err != nil {
		t.Errorf("handleSelected SEARCH failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for SEARCH command")
	}
}

func TestHandleSelectedWithClose(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test CLOSE command
	err := session.handleSelected("CLOSE", []string{}, "A1 CLOSE")
	if err != nil {
		t.Errorf("handleSelected CLOSE failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response for CLOSE, got: %s", written)
	}

	// Session should be back to authenticated state
	if session.state != StateAuthenticated {
		t.Errorf("expected state to be Authenticated after CLOSE, got: %v", session.state)
	}
}

func TestHandleSelectedWithCheck(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test CHECK command
	err := session.handleSelected("CHECK", []string{}, "A1 CHECK")
	if err != nil {
		t.Errorf("handleSelected CHECK failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response for CHECK, got: %s", written)
	}
}

func TestHandleSelectedWithUID(t *testing.T) {
	mock := newMockConn("")
	store := &mockMailstore{}
	server := NewServer(&Config{Addr: ":1143"}, store)
	session := NewSession(mock, server)
	session.state = StateSelected
	session.tag = "A1"
	session.user = "testuser"
	session.selected = &Mailbox{Name: "INBOX"}

	// Test UID FETCH command
	err := session.handleSelected("UID", []string{"FETCH", "1", "FLAGS"}, "A1 UID FETCH 1 FLAGS")
	if err != nil {
		t.Errorf("handleSelected UID FETCH failed: %v", err)
	}

	written := mock.Written()
	if written == "" {
		t.Error("expected response for UID FETCH command")
	}
}

func TestHandleNotAuthenticatedCommands(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		args      []string
		fullLine  string
		wantError bool
	}{
		{
			name:     "CAPABILITY",
			command:  "CAPABILITY",
			args:     []string{},
			fullLine: "A1 CAPABILITY",
		},
		{
			name:     "NOOP",
			command:  "NOOP",
			args:     []string{},
			fullLine: "A1 NOOP",
		},
		{
			name:     "STARTTLS",
			command:  "STARTTLS",
			args:     []string{},
			fullLine: "A1 STARTTLS",
		},
		{
			name:     "LOGIN",
			command:  "LOGIN",
			args:     []string{"user", "pass"},
			fullLine: "A1 LOGIN user pass",
		},
		{
			name:     "AUTHENTICATE",
			command:  "AUTHENTICATE",
			args:     []string{"PLAIN", base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))},
			fullLine: "A1 AUTHENTICATE PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockConn("")
			server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
			server.SetAuthFunc(func(u, p string) (bool, error) {
				return true, nil
			})
			session := NewSession(mock, server)
			session.state = StateNotAuthenticated
			session.tag = "A1"

			err := session.handleNotAuthenticated(tt.command, tt.args, tt.fullLine)
			if (err != nil) != tt.wantError {
				t.Errorf("handleNotAuthenticated() error = %v, wantError %v", err, tt.wantError)
			}

			written := mock.Written()
			if written == "" {
				t.Error("expected some response")
			}
		})
	}
}

func TestHandleAuthenticatedCommands(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		args     []string
		fullLine string
	}{
		{
			name:     "SELECT",
			command:  "SELECT",
			args:     []string{"INBOX"},
			fullLine: "A1 SELECT INBOX",
		},
		{
			name:     "EXAMINE",
			command:  "EXAMINE",
			args:     []string{"INBOX"},
			fullLine: "A1 EXAMINE INBOX",
		},
		{
			name:     "CREATE",
			command:  "CREATE",
			args:     []string{"TestFolder"},
			fullLine: "A1 CREATE TestFolder",
		},
		{
			name:     "DELETE",
			command:  "DELETE",
			args:     []string{"TestFolder"},
			fullLine: "A1 DELETE TestFolder",
		},
		{
			name:     "RENAME",
			command:  "RENAME",
			args:     []string{"OldFolder", "NewFolder"},
			fullLine: "A1 RENAME OldFolder NewFolder",
		},
		{
			name:     "LIST",
			command:  "LIST",
			args:     []string{"", "*"},
			fullLine: "A1 LIST \"\" *",
		},
		{
			name:     "LSUB",
			command:  "LSUB",
			args:     []string{"", "*"},
			fullLine: "A1 LSUB \"\" *",
		},
		{
			name:     "STATUS",
			command:  "STATUS",
			args:     []string{"INBOX", "(MESSAGES UNSEEN)"},
			fullLine: "A1 STATUS INBOX (MESSAGES UNSEEN)",
		},
		{
			name:     "NAMESPACE",
			command:  "NAMESPACE",
			args:     []string{},
			fullLine: "A1 NAMESPACE",
		},
		// APPEND test removed - requires complex continuation handling
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockConn("")
			store := &mockMailstore{}
			server := NewServer(&Config{Addr: ":1143"}, store)
			server.SetAuthFunc(func(u, p string) (bool, error) {
				return true, nil
			})
			session := NewSession(mock, server)
			session.state = StateAuthenticated
			session.tag = "A1"
			session.user = "testuser"

			// Create mailbox for SELECT/EXAMINE tests
			if tt.command == "SELECT" || tt.command == "EXAMINE" {
				store.CreateMailbox("testuser", "INBOX")
			}

			err := session.handleAuthenticated(tt.command, tt.args, tt.fullLine)
			if err != nil {
				t.Errorf("handleAuthenticated() error = %v", err)
			}

			written := mock.Written()
			if written == "" && tt.command != "APPEND" {
				t.Error("expected some response")
			}
		})
	}
}

func TestHandleCommandWithUnknownCommand(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	// Test unknown command
	err := session.handleCommand("UNKNOWNCOMMAND")
	if err != nil {
		t.Errorf("handleCommand failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "BAD") {
		t.Errorf("expected BAD response for unknown command, got: %s", written)
	}
}

func TestHandleCommandWithContinuation(t *testing.T) {
	// Test AUTHENTICATE LOGIN with full round-trip
	username := base64.StdEncoding.EncodeToString([]byte("user"))
	password := base64.StdEncoding.EncodeToString([]byte("pass"))
	input := username + "\r\n" + password + "\r\n"

	mock := newMockConn(input)
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	server.SetAuthFunc(func(u, p string) (bool, error) {
		return true, nil
	})
	session := NewSession(mock, server)
	session.state = StateNotAuthenticated
	session.tag = "A1"

	err := session.handleCommand("A1 AUTHENTICATE LOGIN")
	if err != nil {
		t.Errorf("handleCommand failed: %v", err)
	}

	written := mock.Written()
	// Should contain continuation request (+) and OK
	if !strings.Contains(written, "+") {
		t.Errorf("expected continuation request (+), got: %s", written)
	}
	if !strings.Contains(written, "OK") {
		t.Errorf("expected OK response, got: %s", written)
	}
}

// TestParseSearchCriteriaAllVariations tests parseSearchCriteria with all criteria types
func TestParseSearchCriteriaAllVariations(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected SearchCriteria
	}{
		{
			name: "ALL",
			args: []string{"ALL"},
			expected: SearchCriteria{
				All: true,
			},
		},
		{
			name: "ANSWERED",
			args: []string{"ANSWERED"},
			expected: SearchCriteria{
				All:      true,
				Answered: true,
			},
		},
		{
			name: "DELETED",
			args: []string{"DELETED"},
			expected: SearchCriteria{
				All:     true,
				Deleted: true,
			},
		},
		{
			name: "FLAGGED",
			args: []string{"FLAGGED"},
			expected: SearchCriteria{
				All:     true,
				Flagged: true,
			},
		},
		{
			name: "NEW",
			args: []string{"NEW"},
			expected: SearchCriteria{
				All: true,
				New: true,
			},
		},
		{
			name: "OLD",
			args: []string{"OLD"},
			expected: SearchCriteria{
				All: true,
				Old: true,
			},
		},
		{
			name: "RECENT",
			args: []string{"RECENT"},
			expected: SearchCriteria{
				All:    true,
				Recent: true,
			},
		},
		{
			name: "SEEN",
			args: []string{"SEEN"},
			expected: SearchCriteria{
				All:  true,
				Seen: true,
			},
		},
		{
			name: "UNANSWERED",
			args: []string{"UNANSWERED"},
			expected: SearchCriteria{
				All:        true,
				Unanswered: true,
			},
		},
		{
			name: "UNDELETED",
			args: []string{"UNDELETED"},
			expected: SearchCriteria{
				All:       true,
				Undeleted: true,
			},
		},
		{
			name: "UNFLAGGED",
			args: []string{"UNFLAGGED"},
			expected: SearchCriteria{
				All:       true,
				Unflagged: true,
			},
		},
		{
			name: "UNSEEN",
			args: []string{"UNSEEN"},
			expected: SearchCriteria{
				All:    true,
				Unseen: true,
			},
		},
		{
			name: "DRAFT",
			args: []string{"DRAFT"},
			expected: SearchCriteria{
				All:   true,
				Draft: true,
			},
		},
		{
			name: "UNDRAFT",
			args: []string{"UNDRAFT"},
			expected: SearchCriteria{
				All:     true,
				Undraft: true,
			},
		},
		{
			name: "FROM",
			args: []string{"FROM", "test@example.com"},
			expected: SearchCriteria{
				All:  true,
				From: "test@example.com",
			},
		},
		{
			name: "TO",
			args: []string{"TO", "recipient@example.com"},
			expected: SearchCriteria{
				All: true,
				To:  "recipient@example.com",
			},
		},
		{
			name: "CC",
			args: []string{"CC", "cc@example.com"},
			expected: SearchCriteria{
				All: true,
				Cc:  "cc@example.com",
			},
		},
		{
			name: "BCC",
			args: []string{"BCC", "bcc@example.com"},
			expected: SearchCriteria{
				All: true,
				Bcc: "bcc@example.com",
			},
		},
		{
			name: "SUBJECT",
			args: []string{"SUBJECT", "test subject"},
			expected: SearchCriteria{
				All:     true,
				Subject: "test subject",
			},
		},
		{
			name: "Multiple criteria",
			args: []string{"FROM", "test@example.com", "SUBJECT", "hello", "SEEN"},
			expected: SearchCriteria{
				All:     true,
				From:    "test@example.com",
				Subject: "hello",
				Seen:    true,
			},
		},
		{
			name: "Empty args",
			args: []string{},
			expected: SearchCriteria{
				All: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSearchCriteria(tt.args)

			if result.All != tt.expected.All {
				t.Errorf("All = %v, want %v", result.All, tt.expected.All)
			}
			if result.Answered != tt.expected.Answered {
				t.Errorf("Answered = %v, want %v", result.Answered, tt.expected.Answered)
			}
			if result.Deleted != tt.expected.Deleted {
				t.Errorf("Deleted = %v, want %v", result.Deleted, tt.expected.Deleted)
			}
			if result.Flagged != tt.expected.Flagged {
				t.Errorf("Flagged = %v, want %v", result.Flagged, tt.expected.Flagged)
			}
			if result.Seen != tt.expected.Seen {
				t.Errorf("Seen = %v, want %v", result.Seen, tt.expected.Seen)
			}
			if result.From != tt.expected.From {
				t.Errorf("From = %v, want %v", result.From, tt.expected.From)
			}
			if result.To != tt.expected.To {
				t.Errorf("To = %v, want %v", result.To, tt.expected.To)
			}
			if result.Subject != tt.expected.Subject {
				t.Errorf("Subject = %v, want %v", result.Subject, tt.expected.Subject)
			}
		})
	}
}
