package imap

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type literalTestStore struct {
	mockMailstore
	mu       sync.Mutex
	created  []string
	appended []string
}

func (m *literalTestStore) CreateMailbox(user, mailbox string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created = append(m.created, mailbox)
	return nil
}

func (m *literalTestStore) AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appended = append(m.appended, string(data))
	return nil
}

func (m *literalTestStore) snapshot() (created, appended []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.created...), append([]string(nil), m.appended...)
}

// runLiteralSession drives Server.handleConnection over net.Pipe. steps
// alternate between bytes to send and the reply prefix to wait for. It returns
// the replies that matched each wait ("" if the connection ended first).
func runLiteralSession(t *testing.T, steps ...string) (*Server, *literalTestStore, []string) {
	t.Helper()
	store := &literalTestStore{}
	srv := NewServer(&Config{}, store)
	srv.SetAllowPlainAuth(true)
	srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); srv.handleConnection(serverConn) }()
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		r := bufio.NewReader(clientConn)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			lines <- strings.TrimRight(l, "\r\n")
		}
	}()
	wait := func(p string) string {
		for l := range lines {
			if strings.HasPrefix(l, p) {
				return l
			}
		}
		return ""
	}
	_, _ = clientConn.Write([]byte("a1 LOGIN u p\r\n"))
	if r := wait("a1 "); !strings.Contains(r, "OK") {
		t.Fatalf("login reply %q", r)
	}
	var replies []string
	for i := 0; i+1 < len(steps); i += 2 {
		_, _ = clientConn.Write([]byte(steps[i]))
		r := wait(steps[i+1])
		replies = append(replies, r)
		if r == "" {
			break
		}
	}
	_ = clientConn.Close()
	<-done
	return srv, store, replies
}

// A negative literal size used to reach make([]byte, -1); the recovered panic
// skipped the session-table cleanup, leaking a MaxConnections slot per try.
func TestAppend_NegativeLiteralRejectedWithoutLeak(t *testing.T) {
	for _, lit := range []string{"{-1}", "{-1+}", "{+5}", "{4294967296}"} {
		srv, _, replies := runLiteralSession(t,
			"a2 APPEND INBOX "+lit+"\r\n", "a2 ",
			"a3 LOGOUT\r\n", "a3 ")
		if len(replies) != 2 || !strings.HasPrefix(replies[0], "a2 BAD") || replies[1] == "" {
			t.Errorf("%s: replies %q, want a2 BAD then a3 LOGOUT reply", lit, replies)
		}
		srv.sessionsMu.RLock()
		n := len(srv.sessions)
		srv.sessionsMu.RUnlock()
		if n != 0 {
			t.Errorf("%s: %d sessions leaked", lit, n)
		}
	}
}

// LITERAL+ is advertised: APPEND {n+} must store the literal, never execute its
// octets as commands.
func TestAppend_LiteralPlusIsNotExecutedAsCommands(t *testing.T) {
	body := "x1 CREATE Smuggled\r\n"
	_, store, replies := runLiteralSession(t,
		fmt.Sprintf("a2 APPEND INBOX {%d+}\r\n%s\r\n", len(body), body), "a2 ")
	created, appended := store.snapshot()
	if len(replies) != 1 || !strings.Contains(replies[0], "OK") {
		t.Errorf("reply %q, want a2 OK", replies)
	}
	if !reflect.DeepEqual(appended, []string{body}) {
		t.Errorf("appended %q, want [%q]", appended, body)
	}
	for _, c := range created {
		if c == "Smuggled" {
			t.Error("literal octets were executed as a CREATE command")
		}
	}
}

// RFC 3502 MULTIAPPEND: the CRLF after each follow-on "{n}" marker is not part
// of the message.
func TestAppend_MultiappendSkipsMarkerCRLF(t *testing.T) {
	_, store, _ := runLiteralSession(t,
		"a2 APPEND INBOX {3}\r\n", "+",
		"one {3}\r\n", "+",
		"two {3+}\r\nsix\r\n", "a2 ")
	if _, appended := store.snapshot(); !reflect.DeepEqual(appended, []string{"one", "two", "six"}) {
		t.Errorf("appended %q, want [one two six]", appended)
	}
}
