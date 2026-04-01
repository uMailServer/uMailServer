package pop3

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// mockMailstore implements Mailstore for testing
type mockMailstore struct {
	messages  []*Message
	authOk    bool
	authErr   error
	listErr   error
	dataMap   map[int][]byte
	deleteLog []int
}

func newMockMailstore() *mockMailstore {
	return &mockMailstore{
		messages: []*Message{
			{Index: 1, UID: "uid-001", Size: 100},
			{Index: 2, UID: "uid-002", Size: 200},
			{Index: 3, UID: "uid-003", Size: 300},
		},
		authOk:  true,
		dataMap: make(map[int][]byte),
	}
}

func (m *mockMailstore) Authenticate(username, password string) (bool, error) {
	return m.authOk, m.authErr
}

func (m *mockMailstore) ListMessages(user string) ([]*Message, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Return a fresh copy so DELE in one session doesn't affect RSET reload
	result := make([]*Message, len(m.messages))
	copy(result, m.messages)
	return result, nil
}

func (m *mockMailstore) GetMessage(user string, index int) (*Message, error) {
	if index < 0 || index >= len(m.messages) {
		return nil, fmt.Errorf("out of range")
	}
	return m.messages[index], nil
}

func (m *mockMailstore) GetMessageData(user string, index int) ([]byte, error) {
	if d, ok := m.dataMap[index]; ok {
		return d, nil
	}
	return []byte(fmt.Sprintf("From: test@example.com\r\nSubject: Msg %d\r\n\r\nBody of message %d\r\n", index+1, index+1)), nil
}

func (m *mockMailstore) DeleteMessage(user string, index int) error {
	m.deleteLog = append(m.deleteLog, index)
	return nil
}

func (m *mockMailstore) GetMessageCount(user string) (int, error) {
	return len(m.messages), nil
}

func (m *mockMailstore) GetMessageSize(user string, index int) (int64, error) {
	if index < 0 || index >= len(m.messages) {
		return 0, fmt.Errorf("out of range")
	}
	return m.messages[index].Size, nil
}

// --- Helpers ---

func startTestServer(t *testing.T, store Mailstore, authFunc func(string, string) (bool, error)) (*Server, string) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := NewServer("127.0.0.1:0", store, logger)
	if authFunc != nil {
		srv.SetAuthFunc(authFunc)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	addr := srv.listener.Addr().String()
	return srv, addr
}

func dialAndRead(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	reader := bufio.NewReader(conn)
	greeting, _ := reader.ReadString('\n')
	if !strings.HasPrefix(greeting, "+OK") {
		t.Fatalf("expected greeting, got: %s", greeting)
	}
	return conn, reader
}

func sendCmd(t *testing.T, conn net.Conn, reader *bufio.Reader, cmd string) string {
	t.Helper()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "%s\r\n", cmd)
	resp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read response failed for %q: %v", cmd, err)
	}
	return strings.TrimRight(resp, "\r\n")
}

// sendCmdMulti sends a command and reads the multi-line response (+OK line + data lines until ".")
// Returns the +OK status line and the data lines.
func sendCmdMulti(t *testing.T, conn net.Conn, reader *bufio.Reader, cmd string) (status string, lines []string) {
	t.Helper()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "%s\r\n", cmd)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status for %q: %v", cmd, err)
	}
	status = strings.TrimRight(status, "\r\n")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read multi-line for %q: %v", cmd, err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			break
		}
		lines = append(lines, line)
	}
	return status, lines
}

// --- Tests ---

func TestServerStartStop(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Close()
}

func TestFullSessionFlow(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return user == "testuser" && pass == "testpass", nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// USER
	resp := sendCmd(t, conn, reader, "USER testuser")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("USER: expected +OK, got %s", resp)
	}

	// PASS
	resp = sendCmd(t, conn, reader, "PASS testpass")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("PASS: expected +OK, got %s", resp)
	}

	// STAT
	resp = sendCmd(t, conn, reader, "STAT")
	if !strings.HasPrefix(resp, "+OK 3") {
		t.Errorf("STAT: expected +OK 3, got %s", resp)
	}

	// LIST (all) - multi-line
	status, lines := sendCmdMulti(t, conn, reader, "LIST")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("LIST: expected +OK, got %s", status)
	}
	if len(lines) != 3 {
		t.Errorf("LIST all: expected 3 entries, got %d: %v", len(lines), lines)
	}

	// LIST (single)
	resp = sendCmd(t, conn, reader, "LIST 1")
	if !strings.HasPrefix(resp, "+OK 1") {
		t.Errorf("LIST 1: expected +OK 1, got %s", resp)
	}

	// UIDL (all) - multi-line
	status, lines = sendCmdMulti(t, conn, reader, "UIDL")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("UIDL: expected +OK, got %s", status)
	}
	if len(lines) != 3 {
		t.Errorf("UIDL all: expected 3 entries, got %d: %v", len(lines), lines)
	}

	// UIDL (single)
	resp = sendCmd(t, conn, reader, "UIDL 2")
	if !strings.HasPrefix(resp, "+OK 2 uid-002") {
		t.Errorf("UIDL 2: expected +OK 2 uid-002, got %s", resp)
	}

	// NOOP
	resp = sendCmd(t, conn, reader, "NOOP")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("NOOP: expected +OK, got %s", resp)
	}

	// DELE 2
	resp = sendCmd(t, conn, reader, "DELE 2")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("DELE 2: expected +OK, got %s", resp)
	}

	// LIST deleted message
	resp = sendCmd(t, conn, reader, "LIST 2")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("LIST 2 after DELE: expected -ERR, got %s", resp)
	}

	// QUIT triggers UPDATE state which calls DeleteMessage
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "QUIT\r\n")
	quitResp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(quitResp, "+OK") {
		t.Errorf("QUIT: expected +OK, got %s", quitResp)
	}

	// Verify delete was called for index 1 (0-based)
	if len(store.deleteLog) != 1 || store.deleteLog[0] != 1 {
		t.Errorf("expected delete for index 1, got %v", store.deleteLog)
	}
}

func TestRSETCommand(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// Delete a message
	resp := sendCmd(t, conn, reader, "DELE 1")
	if !strings.HasPrefix(resp, "+OK") {
		t.Fatalf("DELE 1: expected +OK, got %s", resp)
	}

	// RSET should reload messages
	resp = sendCmd(t, conn, reader, "RSET")
	if !strings.HasPrefix(resp, "+OK") {
		t.Fatalf("RSET: expected +OK, got %s", resp)
	}

	// Verify message 1 is back
	resp = sendCmd(t, conn, reader, "LIST 1")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("LIST 1 after RSET: expected +OK, got %s", resp)
	}
}

func TestRETRCommand(t *testing.T) {
	store := newMockMailstore()
	store.dataMap[0] = []byte("From: test@example.com\r\nSubject: Test\r\n\r\nHello World\r\n")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// RETR 1 - multi-line response
	status, lines := sendCmdMulti(t, conn, reader, "RETR 1")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("RETR: expected +OK, got %s", status)
	}
	if len(lines) == 0 {
		t.Error("expected message body lines in RETR")
	}
}

func TestTOPCommand(t *testing.T) {
	store := newMockMailstore()
	store.dataMap[0] = []byte("From: test@example.com\r\nSubject: Test\r\n\r\nLine1\r\nLine2\r\nLine3\r\n")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// TOP 1 2 (headers + 2 body lines)
	status, lines := sendCmdMulti(t, conn, reader, "TOP 1 2")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("TOP: expected +OK, got %s", status)
	}
	// Should have headers + blank + 2 body lines
	if len(lines) < 3 {
		t.Errorf("TOP 1 2: expected at least 3 lines, got %d: %v", len(lines), lines)
	}
}

func TestAuthorizationErrors(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return false, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// PASS without USER
	resp := sendCmd(t, conn, reader, "PASS secret")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS without USER: expected -ERR, got %s", resp)
	}

	// USER without args
	resp = sendCmd(t, conn, reader, "USER")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("USER without args: expected -ERR, got %s", resp)
	}

	// PASS without args
	resp = sendCmd(t, conn, reader, "USER testuser")
	resp = sendCmd(t, conn, reader, "PASS")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS without args: expected -ERR, got %s", resp)
	}

	// Wrong password
	resp = sendCmd(t, conn, reader, "USER testuser")
	resp = sendCmd(t, conn, reader, "PASS wrong")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS wrong: expected -ERR, got %s", resp)
	}

	// Command not valid in authorization state
	resp = sendCmd(t, conn, reader, "STAT")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("STAT in AUTHORIZATION: expected -ERR, got %s", resp)
	}
}

func TestAuthFuncError(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return false, fmt.Errorf("auth backend error")
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	resp := sendCmd(t, conn, reader, "PASS pass")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS with auth error: expected -ERR, got %s", resp)
	}
}

func TestListMessagesError(t *testing.T) {
	store := newMockMailstore()
	store.listErr = fmt.Errorf("db error")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	resp := sendCmd(t, conn, reader, "PASS pass")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS with list error: expected -ERR, got %s", resp)
	}
}

func TestTransactionErrors(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// LIST invalid index
	resp := sendCmd(t, conn, reader, "LIST 99")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("LIST 99: expected -ERR, got %s", resp)
	}

	// LIST non-numeric
	resp = sendCmd(t, conn, reader, "LIST abc")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("LIST abc: expected -ERR, got %s", resp)
	}

	// RETR without args
	resp = sendCmd(t, conn, reader, "RETR")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("RETR no args: expected -ERR, got %s", resp)
	}

	// RETR invalid index
	resp = sendCmd(t, conn, reader, "RETR 0")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("RETR 0: expected -ERR, got %s", resp)
	}

	// DELE without args
	resp = sendCmd(t, conn, reader, "DELE")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("DELE no args: expected -ERR, got %s", resp)
	}

	// DELE invalid index
	resp = sendCmd(t, conn, reader, "DELE 99")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("DELE 99: expected -ERR, got %s", resp)
	}

	// DELE same message twice
	sendCmd(t, conn, reader, "DELE 1")
	resp = sendCmd(t, conn, reader, "DELE 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("DELE twice: expected -ERR, got %s", resp)
	}

	// UIDL invalid index
	resp = sendCmd(t, conn, reader, "UIDL 99")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("UIDL 99: expected -ERR, got %s", resp)
	}

	// TOP without enough args
	resp = sendCmd(t, conn, reader, "TOP 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("TOP 1 arg: expected -ERR, got %s", resp)
	}

	// TOP invalid lines
	resp = sendCmd(t, conn, reader, "TOP 1 -1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("TOP -1 lines: expected -ERR, got %s", resp)
	}

	// Unknown command
	resp = sendCmd(t, conn, reader, "BOGUS")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("BOGUS: expected -ERR, got %s", resp)
	}
}

func TestCAPACommand(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// CAPA in AUTHORIZATION state
	status, lines := sendCmdMulti(t, conn, reader, "CAPA")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("CAPA: expected +OK, got %s", status)
	}
	found := false
	for _, l := range lines {
		if l == "USER" {
			found = true
		}
	}
	if !found {
		t.Error("CAPA in AUTHORIZATION: expected USER capability")
	}

	// Authenticate
	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// CAPA in TRANSACTION state
	status, lines = sendCmdMulti(t, conn, reader, "CAPA")
	if !strings.HasPrefix(status, "+OK") {
		t.Fatalf("CAPA in TRANSACTION: expected +OK, got %s", status)
	}
	found = false
	for _, l := range lines {
		if l == "STAT" {
			found = true
		}
	}
	if !found {
		t.Error("CAPA in TRANSACTION: expected STAT capability")
	}
}

func TestQUITFromAuthorization(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)

	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "QUIT\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "+OK") {
		t.Errorf("QUIT from AUTHORIZATION: expected +OK, got %s", resp)
	}
	conn.Close()
}

func TestQUITDeletesMarkedMessages(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// Delete messages 1 and 3
	sendCmd(t, conn, reader, "DELE 1")
	sendCmd(t, conn, reader, "DELE 3")

	// QUIT triggers UPDATE state
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(conn, "QUIT\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "+OK") {
		t.Errorf("QUIT: expected +OK, got %s", resp)
	}
	conn.Close()

	if len(store.deleteLog) != 2 {
		t.Errorf("expected 2 deletes, got %d: %v", len(store.deleteLog), store.deleteLog)
	}
}

func TestEmptyMailbox(t *testing.T) {
	store := newMockMailstore()
	store.messages = []*Message{}
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// STAT on empty
	resp := sendCmd(t, conn, reader, "STAT")
	if !strings.HasPrefix(resp, "+OK 0") {
		t.Errorf("STAT empty: expected +OK 0, got %s", resp)
	}

	// LIST on empty - multi-line with just "."
	status, lines := sendCmdMulti(t, conn, reader, "LIST")
	if !strings.HasPrefix(status, "+OK") {
		t.Errorf("LIST empty: expected +OK, got %s", status)
	}
	if len(lines) != 0 {
		t.Errorf("LIST empty: expected 0 entries, got %d", len(lines))
	}

	// LIST 1 on empty
	resp = sendCmd(t, conn, reader, "LIST 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("LIST 1 empty: expected -ERR, got %s", resp)
	}
}

func TestNoAuthFunc(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, nil)
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	resp := sendCmd(t, conn, reader, "PASS pass")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("PASS without authFunc: expected +OK, got %s", resp)
	}

	resp = sendCmd(t, conn, reader, "STAT")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("STAT: expected +OK, got %s", resp)
	}
}

func TestTOPNoBodySeparator(t *testing.T) {
	store := newMockMailstore()
	store.dataMap[0] = []byte("Just a single block of text without headers")
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// TOP should handle no-separator case - sends all data without "." terminator
	// The server sends the data directly and then WriteDataEnd
	// We need to read the +OK then the data then "."
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "TOP 1 10\r\n")
	resp, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.TrimRight(resp, "\r\n"), "+OK") {
		t.Fatalf("TOP no-separator: expected +OK, got %s", resp)
	}
	// Read until dot
	for {
		line, _ := reader.ReadString('\n')
		if strings.TrimRight(line, "\r\n") == "." {
			break
		}
	}
}

func TestRSETCommandError(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// Authenticate first (listErr is nil)
	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")

	// Now make ListMessages fail for RSET
	store.listErr = fmt.Errorf("db error")
	resp := sendCmd(t, conn, reader, "RSET")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("RSET with error: expected -ERR, got %s", resp)
	}
}

func TestPassBeforeUser(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	// PASS without USER first
	resp := sendCmd(t, conn, reader, "PASS secret")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("PASS without USER: expected -ERR, got %s", resp)
	}

	// USER then PASS should work
	sendCmd(t, conn, reader, "USER test")
	resp = sendCmd(t, conn, reader, "PASS secret")
	if !strings.HasPrefix(resp, "+OK") {
		t.Errorf("PASS after USER: expected +OK, got %s", resp)
	}
}

func TestSessionID(t *testing.T) {
	srv := &Server{logger: slog.Default()}
	session := NewSession(nil, srv)
	if session.ID() == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestNewServerNilLogger(t *testing.T) {
	srv := NewServer("127.0.0.1:0", nil, nil)
	if srv.logger == nil {
		t.Error("expected default logger when nil passed")
	}
}

func TestSetTLSConfig(t *testing.T) {
	srv := NewServer("127.0.0.1:0", nil, nil)
	cfg := &TLSConfig{CertFile: "cert.pem", KeyFile: "key.pem"}
	srv.SetTLSConfig(cfg)
	if srv.tlsConfig != cfg {
		t.Error("TLS config not set")
	}
}

func TestRETRDeletedMessage(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")
	sendCmd(t, conn, reader, "DELE 1")

	resp := sendCmd(t, conn, reader, "RETR 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("RETR deleted: expected -ERR, got %s", resp)
	}
}

func TestTOPDeletedMessage(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")
	sendCmd(t, conn, reader, "DELE 2")

	resp := sendCmd(t, conn, reader, "TOP 2 0")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("TOP deleted: expected -ERR, got %s", resp)
	}
}

func TestUIDLDeletedMessage(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return true, nil
	})
	defer srv.Stop()

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()

	sendCmd(t, conn, reader, "USER test")
	sendCmd(t, conn, reader, "PASS pass")
	sendCmd(t, conn, reader, "DELE 1")

	resp := sendCmd(t, conn, reader, "UIDL 1")
	if !strings.HasPrefix(resp, "-ERR") {
		t.Errorf("UIDL deleted: expected -ERR, got %s", resp)
	}
}
