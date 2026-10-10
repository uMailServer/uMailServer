package imap

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// r150Client drives Server.handleConnection over net.Pipe.
type r150Client struct {
	t    *testing.T
	conn net.Conn
	rd   *bufio.Reader
	done chan struct{}
}

func newR150Client(t *testing.T, store Mailstore) *r150Client {
	t.Helper()
	srv := NewServer(&Config{}, store)
	srv.SetAllowPlainAuth(true)
	srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc, err := ln.Accept()
		if err != nil {
			return
		}
		srv.handleConnection(sc)
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cl := &r150Client{t: t, conn: c, rd: bufio.NewReader(c), done: done}
	t.Cleanup(func() { _ = c.Close(); <-done })
	cl.until("* ")
	cl.send("a0 LOGIN u p\r\n")
	cl.until("a0 ")
	return cl
}

func (c *r150Client) send(s string) {
	c.t.Helper()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write([]byte(s)); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

// until returns every line up to and including the first starting with prefix.
func (c *r150Client) until(prefix string) []string {
	c.t.Helper()
	var out []string
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		l, err := c.rd.ReadString('\n')
		if err != nil {
			c.t.Fatalf("read (waiting %q, got %q): %v", prefix, out, err)
		}
		l = strings.TrimRight(l, "\r\n")
		out = append(out, l)
		if strings.HasPrefix(l, prefix) {
			return out
		}
	}
}

type r150Store struct {
	mockMailstore
	mu       sync.Mutex
	names    []string
	selected []string
}

func (m *r150Store) ListMailboxes(user, pattern string) ([]string, error) {
	var out []string
	for _, n := range m.names {
		if imapWildcardMatch(n, pattern) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *r150Store) SelectMailbox(user, mailbox string) (*Mailbox, error) {
	m.mu.Lock()
	m.selected = append(m.selected, mailbox)
	m.mu.Unlock()
	return &Mailbox{Name: mailbox}, nil
}

// F6320: NFC and NFD spellings of one name are the same mailbox.
func TestR150_MailboxNameNFC(t *testing.T) {
	const nfc, nfd = "Ş", "Ş"
	st := &r150Store{names: []string{"INBOX", nfc}}
	cl := newR150Client(t, st)
	// Wire form is modified UTF-7; "S" + U+0327 = S&Ayc-
	cl.send("a1 SELECT S&Ayc-\r\n")
	cl.until("a1 ")
	st.mu.Lock()
	got := append([]string(nil), st.selected...)
	st.mu.Unlock()
	if len(got) != 1 || got[0] != nfc {
		t.Fatalf("selected %q, want NFC %q", got, nfc)
	}
	// CREATE of the NFD spelling must see the existing mailbox.
	cl.send("a2 CREATE S&Ayc-\r\n")
	if l := cl.until("a2 "); !strings.Contains(l[len(l)-1], "NO") {
		t.Fatalf("create of equivalent name: %q", l)
	}
	_ = nfd
}

// F6320: legacy data stored in NFD stays reachable by either spelling.
func TestR150_MailboxNameLegacyNFD(t *testing.T) {
	const nfd = "Ş"
	st := &r150Store{names: []string{"INBOX", nfd}}
	cl := newR150Client(t, st)
	cl.send("a1 SELECT &AV4-\r\n") // NFC U+015E
	cl.until("a1 ")
	st.mu.Lock()
	got := append([]string(nil), st.selected...)
	st.mu.Unlock()
	if len(got) != 1 || got[0] != nfd {
		t.Fatalf("selected %q, want stored form %q", got, nfd)
	}
	cl.send("a2 LIST \"\" &AV4-\r\n")
	l := cl.until("a2 ")
	found := false
	for _, x := range l {
		if strings.HasPrefix(x, "* LIST") {
			found = true
		}
	}
	if !found {
		t.Fatalf("LIST by NFC pattern missed legacy NFD mailbox: %q", l)
	}
}

// F6321: a rejected APPEND with a non-synchronising literal must swallow it.
func TestR150_AppendRejectedLiteralConsumed(t *testing.T) {
	cl := newR150Client(t, &mockMailstore{})
	payload := "a9 LOGOUT\r\n"[:9] + "\r\n"
	cl.send("a1 APPEND INBOX (\\Bogus) {9+}\r\n" + payload + "a3 NOOP\r\n")
	lines := cl.until("a3 ")
	for _, l := range lines {
		if strings.HasPrefix(l, "a9 ") || strings.HasPrefix(l, "* BYE") {
			t.Fatalf("literal bytes were executed as a command: %q", lines)
		}
	}
	if !strings.HasPrefix(lines[0], "a1 BAD") {
		t.Fatalf("want a1 BAD first, got %q", lines)
	}
}

// F6321: a rejected synchronising literal gets no continuation.
func TestR150_AppendRejectedSyncLiteralNoContinuation(t *testing.T) {
	cl := newR150Client(t, &mockMailstore{})
	cl.send("a1 APPEND INBOX (\\Bogus) {5}\r\n")
	l := cl.until("a1 ")
	for _, x := range l {
		if strings.HasPrefix(x, "+") {
			t.Fatalf("continuation sent for rejected literal: %q", l)
		}
	}
	cl.send("a2 NOOP\r\n")
	cl.until("a2 OK")
}

// F6321: an oversized {n+} cannot be drained: BYE.
func TestR150_AppendOversizedNonSyncBye(t *testing.T) {
	cl := newR150Client(t, &mockMailstore{})
	cl.send("a1 APPEND INBOX {999999999+}\r\n")
	l := cl.until("* BYE")
	if !strings.HasPrefix(l[len(l)-1], "* BYE") {
		t.Fatalf("no BYE: %q", l)
	}
}

// F6325: SEARCH RETURN (ESEARCH, RFC 4731).
func TestR150_EsearchResponse(t *testing.T) {
	got := esearchResponse("a1", true, []string{"MIN", "MAX", "ALL", "COUNT"}, []uint32{2, 3, 4, 9})
	want := `ESEARCH (TAG "a1") UID MIN 2 MAX 9 ALL 2:4,9 COUNT 4`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := esearchResponse("a2", false, []string{"MIN", "COUNT"}, nil); got != `ESEARCH (TAG "a2") COUNT 0` {
		t.Fatalf("empty: %q", got)
	}
	opts, rest, err := parseSearchReturn(tokenizeIMAPArgs("RETURN (MIN COUNT) ALL"))
	if err != nil || len(opts) != 2 || len(rest) != 1 {
		t.Fatalf("parse: %v %v %v", opts, rest, err)
	}
	if _, _, err := parseSearchReturn(tokenizeIMAPArgs("RETURN (SAVE) ALL")); err == nil {
		t.Fatal("SAVE must be refused")
	}
}

// F6329: the line/literal reader must never execute literal bytes, whatever
// the chunking and literal size.
func FuzzR150LiteralChunking(f *testing.F) {
	f.Add(uint8(5), uint8(1), true)
	f.Add(uint8(0), uint8(3), false)
	f.Add(uint8(200), uint8(7), true)
	f.Fuzz(func(t *testing.T, n, chunk uint8, nonSync bool) {
		if chunk == 0 {
			chunk = 1
		}
		cl := newR150Client(t, &mockMailstore{})
		body := strings.Repeat("a9 LOGOUT\r\n", 30)[:n]
		marker := "{" + itoa(int(n))
		if nonSync {
			marker += "+"
		}
		marker += "}"
		msg := "a1 APPEND INBOX (\\Bogus) " + marker + "\r\n" + body + "\r\na3 NOOP\r\n"
		if !nonSync {
			cl.send("a1 APPEND INBOX (\\Bogus) " + marker + "\r\n")
			msg = "a3 NOOP\r\n"
		}
		for i := 0; i < len(msg); i += int(chunk) {
			end := i + int(chunk)
			if end > len(msg) {
				end = len(msg)
			}
			cl.send(msg[i:end])
		}
		for _, l := range cl.until("a3 ") {
			if strings.HasPrefix(l, "a9 ") || strings.HasPrefix(l, "* BYE") || strings.HasPrefix(l, "+") {
				t.Fatalf("unexpected %q", l)
			}
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// F6328: session A mutating a mailbox (append / flag / expunge) while sessions
// B and C sit in IDLE on it must not race and must reach the idlers.
func TestR150_IdleConcurrentSessionsRace(t *testing.T) {
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user := "race@example.com"
	if err := ms.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(&Config{}, ms)
	srv.SetAllowPlainAuth(true)
	srv.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var served sync.WaitGroup
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			served.Add(1)
			go func() { defer served.Done(); srv.handleConnection(c) }()
		}
	}()
	dial := func() *r150Client {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		cl := &r150Client{t: t, conn: c, rd: bufio.NewReader(c), done: make(chan struct{})}
		t.Cleanup(func() { _ = c.Close() })
		cl.until("* ")
		cl.send("a0 LOGIN " + user + " p\r\n")
		cl.until("a0 ")
		cl.send("a1 SELECT INBOX\r\n")
		cl.until("a1 ")
		return cl
	}
	a, b, c := dial(), dial(), dial()
	for _, idler := range []*r150Client{b, c} {
		idler.send("i1 IDLE\r\n")
		idler.until("+")
	}
	const n = 20
	for i := 0; i < n; i++ {
		msg := "Subject: x\r\n\r\nbody\r\n"
		a.send("p" + itoa(i) + " APPEND INBOX {" + itoa(len(msg)) + "+}\r\n" + msg + "\r\n")
		a.until("p" + itoa(i) + " ")
	}
	a.send("s1 STORE 1:* +FLAGS (\\Deleted)\r\n")
	a.until("s1 ")
	a.send("e1 EXPUNGE\r\n")
	a.until("e1 ")
	for name, idler := range map[string]*r150Client{"B": b, "C": c} {
		exists := 0
		for exists < n {
			_ = idler.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			l, err := idler.rd.ReadString('\n')
			if err != nil {
				t.Fatalf("idler %s saw only %d EXISTS: %v", name, exists, err)
			}
			if strings.Contains(l, "EXISTS") {
				exists++
			}
		}
		idler.send("DONE\r\n")
		idler.until("i1 ")
	}
}
