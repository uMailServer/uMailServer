package imap

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

// preAuthClient builds an unauthenticated session over a pipe (TLS flagged on).
func preAuthClient(t *testing.T) *r183Client {
	t.Helper()
	ms, _ := r183Setup(t, 0)
	client, srv := net.Pipe()
	server := NewServer(&Config{Addr: ":0"}, ms)
	server.SetAuthFunc(func(u, p string) (bool, error) { return true, nil })
	ss := NewSession(srv, server)
	ss.tlsActive = true
	go ss.Handle()
	t.Cleanup(func() { ss.Close(); client.Close() })
	return &r183Client{t: t, c: client, r: bufio.NewReader(client), ss: ss}
}

// F6900: pre-auth NOOP/CAPABILITY must be capped per connection.
func TestF6900_PreAuthNoopCapabilityCapped(t *testing.T) {
	c := preAuthClient(t)
	sawBye := false
	for i := 0; i < maxPreAuthIdleCommands+5; i++ {
		c.n++
		c.send("a" + strings.Repeat("x", 1) + " NOOP")
		c.c.SetReadDeadline(time.Now().Add(2 * time.Second))
		l, err := c.r.ReadString('\n')
		if err != nil {
			sawBye = true
			break
		}
		if strings.HasPrefix(l, "* BYE") {
			sawBye = true
			break
		}
	}
	if !sawBye {
		t.Fatal("unbounded pre-auth NOOP accepted")
	}
}

// F6901: AUTHENTICATE PLAIN with authzid different from authcid is refused.
func TestF6901_PlainAuthzidMismatch(t *testing.T) {
	c := preAuthClient(t)
	cred := base64.StdEncoding.EncodeToString([]byte("admin@example.com\x00r183@example.com\x00pw"))
	out := joined(c.cmd("AUTHENTICATE PLAIN " + cred))
	if !strings.Contains(out, " NO ") && !strings.Contains(out, " BAD ") {
		t.Fatalf("authzid mismatch not refused: %q", out)
	}
	if strings.Contains(out, "OK AUTHENTICATE") {
		t.Fatalf("authenticated with foreign authzid: %q", out)
	}
	if c.ss.User() != "" && c.ss.State() == StateAuthenticated {
		t.Fatalf("session authenticated")
	}
}

// F6902: STATUS HIGHESTMODSEQ is refused while CONDSTORE is not advertised.
func TestF6902_StatusHighestModSeqRejected(t *testing.T) {
	ms, user := r183Setup(t, 1)
	c := newR183(t, ms, user)
	out := joined(c.cmd("STATUS INBOX (MESSAGES HIGHESTMODSEQ)"))
	if strings.Contains(out, "HIGHESTMODSEQ 0") || strings.Contains(out, "* STATUS") {
		t.Fatalf("HIGHESTMODSEQ served: %q", out)
	}
	if !strings.Contains(out, " BAD ") && !strings.Contains(out, " NO ") {
		t.Fatalf("not refused: %q", out)
	}
}

// F6903: COMPRESS=DEFLATE must not be advertised once compression is active.
func TestF6903_NoCompressCapAfterActive(t *testing.T) {
	ms, user := r183Setup(t, 0)
	c := newR183(t, ms, user)
	c.ss.compressActive = true
	out := joined(c.cmd("CAPABILITY"))
	if strings.Contains(out, "COMPRESS=DEFLATE") {
		t.Fatalf("COMPRESS advertised while active: %q", out)
	}
}

// F6904: out-of-range message sequence numbers in FETCH/STORE are an error.
func TestF6904_OutOfRangeSeq(t *testing.T) {
	ms, user := r183Setup(t, 3)
	c := newR183(t, ms, user)
	c.cmd("SELECT INBOX")
	for _, cmd := range []string{"FETCH 9 (FLAGS)", "STORE 9 +FLAGS (\\Seen)", "FETCH 9:12 (FLAGS)", "FETCH 0 (FLAGS)"} {
		out := c.cmd(cmd)
		last := out[len(out)-1]
		if !strings.Contains(last, " NO ") && !strings.Contains(last, " BAD ") {
			t.Errorf("%s: %q", cmd, last)
		}
	}
	// valid and "*" forms still OK; UID forms with unknown UIDs stay OK.
	for _, cmd := range []string{"FETCH 1:* (FLAGS)", "FETCH 3 (FLAGS)", "FETCH 2:9 (FLAGS)", "FETCH * (FLAGS)", "UID FETCH 99 (FLAGS)", "UID STORE 99 +FLAGS (\\Seen)"} {
		out := c.cmd(cmd)
		last := out[len(out)-1]
		if !strings.Contains(last, " OK ") {
			t.Errorf("%s: %q", cmd, last)
		}
	}
}

// F6905: after another session expunges, IDLE/NOOP must not announce a RECENT
// count larger than EXISTS.
func TestF6905_RecentNeverExceedsExists(t *testing.T) {
	ms, user := r183Setup(t, 4)
	a := newR183(t, ms, user)
	b := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	b.cmd("SELECT INBOX")
	b.cmd("STORE 1:4 +FLAGS (\\Deleted)")
	b.cmd("EXPUNGE")
	b.cmd("APPEND INBOX {5+}\r\nhello")
	out := joined(a.cmd("NOOP"))
	if !strings.Contains(out, "* 1 EXISTS") {
		t.Fatalf("expected 1 EXISTS: %q", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasSuffix(l, " RECENT") && l != "* 1 RECENT" && l != "* 0 RECENT" {
			t.Fatalf("RECENT exceeds EXISTS: %q", out)
		}
	}
}
