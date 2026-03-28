package imap

import (
	"bufio"
	"bytes"
	"crypto/tls"
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
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthenticate([]string{"PLAIN"})
	if err != nil {
		t.Errorf("handleAuthenticate PLAIN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response for unimplemented PLAIN, got: %s", written)
	}
}

func TestHandleAuthenticateLogin(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthenticate([]string{"LOGIN"})
	if err != nil {
		t.Errorf("handleAuthenticate LOGIN failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response for unimplemented LOGIN mechanism, got: %s", written)
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
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
	session := NewSession(mock, server)
	session.tag = "A1"

	err := session.handleAuthPlain()
	if err != nil {
		t.Errorf("handleAuthPlain failed: %v", err)
	}

	written := mock.Written()
	if !strings.Contains(written, "NO") {
		t.Errorf("expected NO response, got: %s", written)
	}
}

func TestHandleAuthLogin(t *testing.T) {
	mock := newMockConn("")
	server := NewServer(&Config{Addr: ":1143"}, &mockMailstore{})
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
