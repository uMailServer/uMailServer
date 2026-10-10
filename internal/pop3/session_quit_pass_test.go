package pop3

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Regressions for F5230 (QUIT must end the session even when UPDATE cannot
// list the maildrop), F5232 (PASS keeps spaces) and F5233 (PASS is never
// logged).

type quitPassStore struct {
	failList atomic.Bool
	mu       sync.Mutex
	deleted  []int
}

func (m *quitPassStore) Authenticate(_, password string) (bool, error) {
	return password == "secret-pw", nil
}
func (m *quitPassStore) ListMessages(string) ([]*Message, error) {
	if m.failList.Load() {
		return nil, errors.New("injected listing failure")
	}
	return []*Message{{Index: 1, UID: "a", Size: 3}, {Index: 2, UID: "b", Size: 3}}, nil
}
func (m *quitPassStore) GetMessage(string, int) (*Message, error)   { return nil, errors.New("unused") }
func (m *quitPassStore) GetMessageData(string, int) ([]byte, error) { return []byte("x\r\n"), nil }
func (m *quitPassStore) DeleteMessage(_ string, i int) error {
	m.mu.Lock()
	m.deleted = append(m.deleted, i)
	m.mu.Unlock()
	return nil
}
func (m *quitPassStore) GetMessageCount(string) (int, error)       { return 2, nil }
func (m *quitPassStore) GetMessageSize(string, int) (int64, error) { return 3, nil }

type quitPassClient struct {
	conn net.Conn
	r    *bufio.Reader
	done chan struct{}
}

func startQuitPassSession(t *testing.T, srv *Server) *quitPassClient {
	t.Helper()
	c1, c2 := net.Pipe()
	sess := NewSession(c1, srv)
	done := make(chan struct{})
	go func() { sess.Handle(); c1.Close(); close(done) }()
	t.Cleanup(func() { c2.Close(); <-done })
	return &quitPassClient{conn: c2, r: bufio.NewReader(c2), done: done}
}

// cmd returns the first reply line or "<closed>" once the session has ended.
func (c *quitPassClient) cmd(line string) string {
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		return "<closed>"
	}
	l, err := c.r.ReadString('\n')
	if err != nil {
		return "<closed>"
	}
	return strings.TrimRight(l, "\r\n")
}

func TestQuit_UpdateListFailureEndsSession(t *testing.T) {
	store := &quitPassStore{}
	srv := NewServer("127.0.0.1:0", store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := startQuitPassSession(t, srv)
	for _, l := range []string{"USER u", "PASS secret-pw", "DELE 1"} {
		if r := c.cmd(l); !strings.HasPrefix(r, "+OK") {
			t.Fatalf("%q -> %q", l, r)
		}
	}
	store.failList.Store(true)
	if r := c.cmd("QUIT"); !strings.HasPrefix(r, "-ERR") {
		t.Fatalf("QUIT with failing listing: got %q, want -ERR", r)
	}
	store.failList.Store(false)
	if r := c.cmd("NOOP"); r != "<closed>" {
		t.Fatalf("session still alive after QUIT: NOOP -> %q", r)
	}
	<-c.done
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 0 {
		t.Fatalf("deletions committed after the client was told QUIT failed: %v", store.deleted)
	}
}

func TestPass_PasswordWithSpaces(t *testing.T) {
	for _, pw := range []string{"correct horse battery", "two  spaces", "tab\there", "trailing "} {
		srv := NewServer("127.0.0.1:0", &quitPassStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv.SetAuthFunc(func(_, p string) (bool, error) { return p == pw, nil })
		c := startQuitPassSession(t, srv)
		c.cmd("USER u")
		if r := c.cmd("PASS " + pw); r != "+OK" {
			t.Fatalf("password %q: got %q, want +OK", pw, r)
		}
	}
	srv := NewServer("127.0.0.1:0", &quitPassStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := startQuitPassSession(t, srv)
	c.cmd("USER u")
	for _, l := range []string{"PASS", "PASS ", "PASS   "} {
		if r := c.cmd(l); !strings.HasPrefix(r, "-ERR Usage") {
			t.Fatalf("%q -> %q, want usage error", l, r)
		}
	}
}

type quitPassLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *quitPassLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func TestPass_NotWrittenToDebugLog(t *testing.T) {
	logs := &quitPassLog{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := startQuitPassSession(t, NewServer("127.0.0.1:0", &quitPassStore{}, logger))
	for _, l := range []string{"user alice", "  pass wrong-one", "USER alice", "PASS secret-pw", "QUIT"} {
		c.cmd(l)
	}
	<-c.done
	logs.mu.Lock()
	out := logs.buf.String()
	logs.mu.Unlock()
	for _, s := range []string{"wrong-one", "secret-pw"} {
		if strings.Contains(out, s) {
			t.Fatalf("password %q written to log", s)
		}
	}
	if !strings.Contains(out, "USER alice") {
		t.Fatalf("non-secret commands should still be logged")
	}
}
