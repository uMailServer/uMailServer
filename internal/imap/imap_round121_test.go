package imap

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const r121User = "user@example.com"

// r121Server is round82Server with a hook to configure the Server and
// without the automatic login (the caller logs in), returning the shared
// store so tests can act as a second session / delivery.
func r121Server(t *testing.T, conf func(*Server), auth func(u, p string) (bool, error)) (*round82Client, *BboltMailstore, *Server) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(r121User, mb); err != nil {
			t.Fatal(err)
		}
	}
	srv := NewServer(&Config{}, ms)
	srv.SetAllowPlainAuth(true)
	if auth == nil {
		auth = func(u, p string) (bool, error) { return true, nil }
	}
	srv.SetAuthFunc(auth)
	if conf != nil {
		conf(srv)
	}
	clientConn, serverConn := net.Pipe()
	go srv.handleConnection(serverConn)
	c := newRound82Client(t, clientConn)
	c.until("* OK")
	return c, ms, srv
}

func r121Login(t *testing.T, c *round82Client) {
	t.Helper()
	if out := c.cmd("a0 LOGIN " + r121User + " pw"); !strings.HasPrefix(out[len(out)-1], "a0 OK") {
		t.Fatalf("login: %q", out)
	}
}

func r121Last(out []string) string { return out[len(out)-1] }

func r121Msg(subj string) []byte {
	return []byte("From: a@example.com\r\nSubject: " + subj + "\r\n\r\nbody\r\n")
}

// expectSilence fails if any line arrives within d.
func (c *round82Client) expectSilence(d time.Duration) {
	c.t.Helper()
	select {
	case l, ok := <-c.lines:
		if ok {
			c.t.Fatalf("unexpected line %q", l)
		}
		c.t.Fatalf("connection closed unexpectedly")
	case <-time.After(d):
	}
}

// F6030: the read deadline armed for the IDLE command line cut IDLE short.
func TestRound121IdleSurvivesReadTimeout(t *testing.T) {
	c, _, _ := r121Server(t, func(s *Server) { s.SetReadTimeout(300 * time.Millisecond) }, nil)
	r121Login(t, c)
	c.cmd("a1 SELECT INBOX")
	c.write("a2 IDLE\r\n")
	c.until("+ ")
	c.expectSilence(900 * time.Millisecond) // 3x readTimeout
	c.write("DONE\r\n")
	if l := r121Last(c.until("a2 ")); !strings.HasPrefix(l, "a2 OK") {
		t.Fatalf("IDLE end: %q", l)
	}
	if l := r121Last(c.cmd("a3 NOOP")); !strings.HasPrefix(l, "a3 OK") {
		t.Fatalf("NOOP after IDLE: %q", l)
	}
}

// F6030: with no read timeout the forced wake-up deadline survived IDLE and
// killed the connection at the next command.
func TestRound121IdleThenCommandNoReadTimeout(t *testing.T) {
	c, _, _ := r121Server(t, func(s *Server) { s.SetReadTimeout(0) }, nil)
	r121Login(t, c)
	c.cmd("a1 SELECT INBOX")
	c.write("a2 IDLE\r\n")
	c.until("+ ")
	c.write("DONE\r\n")
	c.until("a2 OK")
	time.Sleep(50 * time.Millisecond)
	if l := r121Last(c.cmd("a3 NOOP")); !strings.HasPrefix(l, "a3 OK") {
		t.Fatalf("NOOP after IDLE: %q", l)
	}
}

// The server-side idle limit is an autologout, not a tagged completion.
func TestRound121IdleTimeoutAutologout(t *testing.T) {
	c, _, _ := r121Server(t, func(s *Server) { s.SetIdleTimeout(150 * time.Millisecond) }, nil)
	r121Login(t, c)
	c.cmd("a1 SELECT INBOX")
	c.write("a2 IDLE\r\n")
	c.until("+ ")
	out := c.until("* BYE")
	if !strings.Contains(r121Last(out), "BYE") {
		t.Fatalf("got %q", out)
	}
}

// F6031: an EXPUNGE done by another session / delivery agent reached no
// IDLE client.
func TestRound121IdleReceivesExpunge(t *testing.T) {
	c, ms, _ := r121Server(t, nil, nil)
	for _, s := range []string{"one", "two", "three"} {
		if err := ms.AppendMessage(r121User, "INBOX", nil, time.Now(), r121Msg(s)); err != nil {
			t.Fatal(err)
		}
	}
	r121Login(t, c)
	c.cmd("a1 SELECT INBOX")
	c.write("a2 IDLE\r\n")
	c.until("+ ")
	if err := ms.StoreFlags(r121User, "INBOX", "1,3", []string{"\\Deleted"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	if err := ms.Expunge(r121User, "INBOX"); err != nil {
		t.Fatal(err)
	}
	// Highest first so each number is valid when applied.
	for _, want := range []string{"* 3 EXPUNGE", "* 1 EXPUNGE"} {
		var got string
		for {
			got = r121Last(c.until("* "))
			if strings.HasSuffix(got, "EXPUNGE") {
				break
			}
		}
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
	c.write("DONE\r\n")
	c.until("a2 OK")
}

// F6032: quoted names with spaces / apostrophes; F6033/F6034 naming rules.
func TestRound121MailboxNaming(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	ok := func(cmd string) {
		t.Helper()
		out := c.cmd(cmd)
		if !strings.Contains(r121Last(out), " OK") {
			t.Fatalf("%s: %q", cmd, out)
		}
	}
	no := func(cmd, code string) {
		t.Helper()
		out := c.cmd(cmd)
		l := r121Last(out)
		if !strings.Contains(l, " NO") || !strings.Contains(l, code) {
			t.Fatalf("%s: want NO %s, got %q", cmd, code, out)
		}
	}
	list := func() string { return strings.Join(c.cmd(`x LIST "" *`), "\n") }

	ok(`a1 CREATE "My Folder"`)
	no(`a2 CREATE "My Folder"`, "ALREADYEXISTS")
	no(`a3 CREATE inbox`, "ALREADYEXISTS")
	no(`a4 CREATE Inbox/`, "ALREADYEXISTS")
	ok(`a5 CREATE Work/`)    // trailing delimiter is a hint only
	ok(`a6 CREATE Work/Sub`) // child
	ok(`a7 CREATE "Bob's"`)
	l := list()
	for _, want := range []string{`"My Folder"`, `"Work"`, `"Work/Sub"`, `"Bob's"`} {
		if !strings.Contains(l, want) {
			t.Fatalf("LIST lacks %s:\n%s", want, l)
		}
	}
	if strings.Contains(l, `"Work/"`) || strings.Contains(l, `"inbox"`) {
		t.Fatalf("bad names in LIST:\n%s", l)
	}
	out := strings.Join(c.cmd(`s1 STATUS "My Folder" (MESSAGES UIDNEXT)`), "\n")
	if !strings.Contains(out, `* STATUS "My Folder" (MESSAGES 0 UIDNEXT 1)`) {
		t.Fatalf("STATUS: %s", out)
	}
	ok(`a8 SUBSCRIBE "My Folder"`)
	if out := strings.Join(c.cmd(`l1 LSUB "" *`), "\n"); !strings.Contains(out, `"My Folder"`) {
		t.Fatalf("LSUB: %s", out)
	}
	ok(`a9 UNSUBSCRIBE "My Folder"`)
	if out := strings.Join(c.cmd(`l2 LSUB "" *`), "\n"); strings.Contains(out, `My Folder`) {
		t.Fatalf("LSUB after UNSUBSCRIBE: %s", out)
	}

	// RENAME moves the hierarchy and refuses bad requests.
	no(`b1 RENAME Nope Other`, "NONEXISTENT")
	no(`b2 RENAME Work Archive`, "ALREADYEXISTS")
	no(`b3 RENAME INBOX Z`, "Cannot rename INBOX")
	no(`b4 RENAME Work "Work/Sub"`, "ALREADYEXISTS")
	ok(`b5 RENAME Work "Projects 2024"`)
	l = list()
	if !strings.Contains(l, `"Projects 2024/Sub"`) || strings.Contains(l, `"Work/Sub"`) {
		t.Fatalf("children not renamed:\n%s", l)
	}
	if strings.Contains(l, `"Other"`) {
		t.Fatalf("RENAME of a missing mailbox created one:\n%s", l)
	}

	no(`c1 DELETE Nope`, "NONEXISTENT")
	no(`c2 DELETE inbox`, "Cannot delete INBOX")
	ok(`c3 DELETE "My Folder"`)
	if strings.Contains(list(), "My Folder") {
		t.Fatal("DELETE did not delete")
	}
	if l := r121Last(c.cmd(`c4 CREATE "a//b"`)); !strings.Contains(l, "NO") {
		t.Fatalf("empty hierarchy level accepted: %q", l)
	}
	if l := r121Last(c.cmd(`c5 CREATE "x*y"`)); !strings.Contains(l, "NO") {
		t.Fatalf("wildcard name accepted: %q", l)
	}
	// INBOX is case-insensitive everywhere.
	if out := strings.Join(c.cmd(`c6 SELECT iNbOx`), "\n"); !strings.Contains(out, "c6 OK") {
		t.Fatalf("SELECT iNbOx: %s", out)
	}
}

// F6035: STATUS item list validation and order.
func TestRound121StatusItems(t *testing.T) {
	c, ms, _ := r121Server(t, nil, nil)
	for _, s := range []string{"a", "b"} {
		if err := ms.AppendMessage(r121User, "INBOX", nil, time.Now(), r121Msg(s)); err != nil {
			t.Fatal(err)
		}
	}
	r121Login(t, c)
	for _, bad := range []string{
		`s1 STATUS INBOX (MESSAGES`,
		`s2 STATUS INBOX MESSAGES`,
		`s3 STATUS INBOX (MESSAGES BOGUS)`,
		`s4 STATUS INBOX (XMESSAGESX)`,
		`s5 STATUS INBOX ()`,
	} {
		if l := r121Last(c.cmd(bad)); !strings.Contains(l, " BAD") {
			t.Errorf("%s: want BAD, got %q", bad, l)
		}
	}
	out := strings.Join(c.cmd(`s6 STATUS inbox (UNSEEN MESSAGES HIGHESTMODSEQ)`), "\n")
	if !strings.Contains(out, `* STATUS "INBOX" (UNSEEN 2 MESSAGES 2 HIGHESTMODSEQ `) {
		t.Fatalf("STATUS: %s", out)
	}
}

// F6039: [UNSEEN n] is the first unseen message's sequence number.
func TestRound121SelectFirstUnseen(t *testing.T) {
	c, ms, _ := r121Server(t, nil, nil)
	for _, s := range []string{"a", "b", "c"} {
		if err := ms.AppendMessage(r121User, "INBOX", nil, time.Now(), r121Msg(s)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.StoreFlags(r121User, "INBOX", "1:2", []string{"\\Seen"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	r121Login(t, c)
	out := strings.Join(c.cmd("a1 SELECT INBOX"), "\n")
	if !strings.Contains(out, "[UNSEEN 3]") {
		t.Fatalf("want [UNSEEN 3]:\n%s", out)
	}
}

// F6032: LOGIN credentials are not mangled by quote trimming.
func TestRound121LoginCredentials(t *testing.T) {
	var gotU, gotP atomic.Value
	c, _, _ := r121Server(t, nil, func(u, p string) (bool, error) {
		gotU.Store(u)
		gotP.Store(p)
		return true, nil
	})
	out := c.cmd(`a1 LOGIN user@example.com "it's a \"pw\"'"`)
	if !strings.Contains(r121Last(out), "a1 OK") {
		t.Fatalf("login: %q", out)
	}
	if p := gotP.Load(); p != `it's a "pw"'` {
		t.Fatalf("password = %q", p)
	}
}

// F6036: literal arguments get a continuation request and are bounded.
func TestRound121LiteralArguments(t *testing.T) {
	var gotP atomic.Value
	c, _, _ := r121Server(t, nil, func(u, p string) (bool, error) {
		gotP.Store(p)
		return true, nil
	})
	c.write("a1 LOGIN {16}\r\n")
	c.until("+ ")
	c.write("user@example.com {5}\r\n")
	c.until("+ ")
	c.write("p w\"x\r\n")
	if l := r121Last(c.until("a1 ")); !strings.HasPrefix(l, "a1 OK") {
		t.Fatalf("LOGIN with literals: %q", l)
	}
	if p := gotP.Load(); p != `p w"x` {
		t.Fatalf("password = %q", p)
	}
	// Non-synchronising literal.
	if l := r121Last(c.cmd("a2 CREATE {5+}\r\nAB CD")); !strings.HasPrefix(l, "a2 OK") {
		t.Fatalf("CREATE with LITERAL+: %q", l)
	}
	// Oversized synchronising literal: refused, session stays usable.
	c.write("a3 CREATE {99999999}\r\n")
	if l := r121Last(c.until("a3 ")); !strings.Contains(l, "BAD") || !strings.Contains(l, "TOOBIG") {
		t.Fatalf("oversized literal: %q", l)
	}
	if l := r121Last(c.cmd("a4 NOOP")); !strings.HasPrefix(l, "a4 OK") {
		t.Fatalf("NOOP: %q", l)
	}
	// Oversized non-synchronising literal: cannot resync, connection closes.
	c.write("a5 CREATE {99999999+}\r\n")
	if l := r121Last(c.until("* BYE")); !strings.Contains(l, "BYE") {
		t.Fatalf("got %q", l)
	}
}
