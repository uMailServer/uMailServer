package pop3

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
)

// F4986: RETR/TOP must return the message the session's snapshot numbers,
// even when the maildrop changes underneath (another POP3 session's UPDATE,
// an IMAP expunge/\Deleted flag). The store resolves 1-based indexes against
// the CURRENT maildrop, exactly like BboltStore and the server adapter.

type snap4986Store struct {
	mu      sync.Mutex
	uids    []string
	removed map[string]bool
}

func (a *snap4986Store) visible() []*Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*Message
	for i, u := range a.uids {
		if !a.removed[u] {
			out = append(out, &Message{Index: i + 1, UID: u, Size: int64(len(a.body(u)))})
		}
	}
	return out
}
func (a *snap4986Store) body(u string) string {
	return "Subject: S-" + u + "\r\n\r\nBODY-" + u + "\r\n"
}
func (a *snap4986Store) remove(u string) {
	a.mu.Lock()
	a.removed[u] = true
	a.mu.Unlock()
}
func (a *snap4986Store) Authenticate(string, string) (bool, error) { return true, nil }
func (a *snap4986Store) ListMessages(string) ([]*Message, error)   { return a.visible(), nil }
func (a *snap4986Store) GetMessage(_ string, i int) (*Message, error) {
	v := a.visible()
	if i < 1 || i > len(v) {
		return nil, fmt.Errorf("out of range")
	}
	return v[i-1], nil
}
func (a *snap4986Store) GetMessageData(_ string, i int) ([]byte, error) {
	m, err := a.GetMessage("", i)
	if err != nil {
		return nil, err
	}
	return []byte(a.body(m.UID)), nil
}
func (a *snap4986Store) DeleteMessage(_ string, i int) error {
	m, err := a.GetMessage("", i)
	if err != nil {
		return err
	}
	a.remove(m.UID)
	return nil
}
func (a *snap4986Store) GetMessageCount(string) (int, error) { return len(a.visible()), nil }
func (a *snap4986Store) GetMessageSize(_ string, i int) (int64, error) {
	m, err := a.GetMessage("", i)
	if err != nil {
		return 0, err
	}
	return m.Size, nil
}

type snap4986Client struct {
	t  *testing.T
	c  net.Conn
	rd *bufio.Reader
}

func (c *snap4986Client) line() string {
	l, err := c.rd.ReadString('\n')
	if err != nil {
		c.t.Fatalf("INVALID harness read: %v", err)
	}
	return strings.TrimRight(l, "\r\n")
}

// cmd sends a command and returns the status line plus data lines when the
// status is +OK and multi is set.
func (c *snap4986Client) cmd(s string, multi bool) (string, []string) {
	if _, err := c.c.Write([]byte(s + "\r\n")); err != nil {
		c.t.Fatalf("INVALID harness write: %v", err)
	}
	st := c.line()
	var data []string
	if multi && strings.HasPrefix(st, "+OK") {
		for {
			l := c.line()
			if l == "." {
				break
			}
			data = append(data, l)
		}
	}
	return st, data
}

func snap4986Login(t *testing.T) (*snap4986Store, *snap4986Client, func()) {
	t.Helper()
	store := &snap4986Store{uids: []string{"11", "22", "33"}, removed: map[string]bool{}}
	srv := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), mailstore: store}
	c1, c2 := net.Pipe()
	sess := NewSession(c1, srv)
	go func() { sess.Handle(); c1.Close() }()
	cl := &snap4986Client{t: t, c: c2, rd: bufio.NewReader(c2)}
	if st, _ := cl.cmd("USER a", false); !strings.HasPrefix(st, "+OK") {
		t.Fatalf("INVALID harness USER %q", st)
	}
	if st, _ := cl.cmd("PASS b", false); !strings.HasPrefix(st, "+OK") {
		t.Fatalf("INVALID harness PASS %q", st)
	}
	return store, cl, func() { c2.Close() }
}

func TestRETRSnapshotControl(t *testing.T) {
	_, cl, done := snap4986Login(t)
	defer done()
	st, data := cl.cmd("RETR 2", true)
	got := strings.Join(data, "|")
	t.Logf("CONTROL EXPECTED: BODY-22 | ACTUAL: %s %s", st, got)
	if !strings.Contains(got, "BODY-22") {
		t.Fatalf("INVALID CONTROL")
	}
}

func TestRETRResolvesSnapshotUIDAfterExternalRemoval(t *testing.T) {
	store, cl, done := snap4986Login(t)
	defer done()
	// Another session's UPDATE removes UID 11 after this session's snapshot.
	store.remove("11")
	st, _ := cl.cmd("UIDL 2", false)
	_, data := cl.cmd("RETR 2", true)
	got := strings.Join(data, "|")
	t.Logf("UIDL 2 -> %s", st)
	t.Logf("EXPECTED: RETR 2 returns BODY-22 (snapshot UID 22) | ACTUAL: %s", got)
	if !strings.Contains(got, "BODY-22") {
		t.Fatalf("DEFECT F4986: RETR returned a different message than the session numbered")
	}
}

// Edge cases: TOP, removed message, repeated RETR, UPDATE.
func TestRETRTOPSnapshotEdges(t *testing.T) {
	store, cl, done := snap4986Login(t)
	defer done()
	store.remove("11")
	// TOP resolves the same way.
	_, data := cl.cmd("TOP 2 0", true)
	if got := strings.Join(data, "|"); !strings.Contains(got, "S-22") {
		t.Fatalf("EDGE TOP: want S-22, got %q", got)
	}
	// RETR of the message removed externally must fail, never return another.
	st, data := cl.cmd("RETR 1", true)
	if !strings.HasPrefix(st, "-ERR") {
		t.Fatalf("EDGE removed: want -ERR, got %q %q", st, data)
	}
	// Last message still resolves; repeated RETR stable.
	for i := 0; i < 2; i++ {
		_, data = cl.cmd("RETR 3", true)
		if got := strings.Join(data, "|"); !strings.Contains(got, "BODY-33") {
			t.Fatalf("EDGE last: want BODY-33, got %q", got)
		}
	}
	// DELE + QUIT still removes the snapshot UID (22), not a shifted one.
	if st, _ := cl.cmd("DELE 2", false); !strings.HasPrefix(st, "+OK") {
		t.Fatalf("EDGE DELE: %q", st)
	}
	cl.cmd("QUIT", false)
	v := store.visible()
	if len(v) != 1 || v[0].UID != "33" {
		t.Fatalf("EDGE UPDATE: want only 33 left, got %d msgs", len(v))
	}
}
