package imap

import (
	"bufio"
	"bytes"
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
