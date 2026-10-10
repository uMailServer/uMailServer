package imap

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func round102Anon(t *testing.T, cfg *tls.Config) *round82Client {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	srv := NewServer(&Config{TLSConfig: cfg}, ms)
	cc, sc := net.Pipe()
	go srv.handleConnection(sc)
	c := newRound82Client(t, cc)
	c.until("* OK")
	return c
}

// F5840: a tls.Config without any certificate must neither advertise
// STARTTLS nor answer it OK (the handshake could only fail).
func TestRound102StartTLSWithoutCertificate(t *testing.T) {
	c := round102Anon(t, &tls.Config{MinVersion: tls.VersionTLS12})
	caps := strings.Join(c.cmd("a1 CAPABILITY"), " ")
	if strings.Contains(caps, "STARTTLS") {
		t.Fatalf("DEFECT F5840: STARTTLS advertised without certificate: %s", caps)
	}
	if got := round75Tagged(c.cmd("a2 STARTTLS")); !strings.HasPrefix(got, "a2 NO") {
		t.Fatalf("DEFECT F5840: STARTTLS without certificate replied %q", got)
	}
}

// F5840: bytes pipelined behind STARTTLS are cleartext injection.
func TestRound102StartTLSRejectsPipelinedInput(t *testing.T) {
	c := round102Anon(t, &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}, MinVersion: tls.VersionTLS12})
	c.write("a1 STARTTLS\r\na2 NOOP\r\n")
	out := c.until("a1 ")
	if got := out[len(out)-1]; !strings.HasPrefix(got, "a1 BAD") {
		t.Fatalf("DEFECT F5840: pipelined STARTTLS replied %q", got)
	}
}

func round102Multi(t *testing.T, c *round82Client, tag, mailbox string, msgs ...string) []string {
	return round82Append(t, c, tag, mailbox, msgs...)
}

// F5841: MULTIAPPEND is all-or-nothing and the quota covers the sum.
func TestRound102MultiappendAtomicQuota(t *testing.T) {
	c, ms := round82Server(t)
	m1, m2 := round91Msg("one", 1000), round91Msg("two", 1000)
	ms.SetQuotaLimitFunc(func(string) int64 { return 1500 }) // each fits, the sum does not
	got := round75Tagged(round102Multi(t, c, "m1", "INBOX", m1, m2))
	if !strings.HasPrefix(got, "m1 NO [OVERQUOTA]") {
		t.Fatalf("DEFECT F5841: MULTIAPPEND over quota replied %q", got)
	}
	out := c.cmd("m2 STATUS INBOX (MESSAGES)")
	if !strings.Contains(strings.Join(out, " "), "MESSAGES 0") {
		t.Fatalf("DEFECT F5841: partial MULTIAPPEND left messages: %q", out)
	}
	if used, _ := ms.msgStore.UserUsage("user@example.com"); used != 0 {
		t.Fatalf("DEFECT F5841: blobs leaked, usage %d", used)
	}
}

// F5841: a failure part-way (invalid UTF-8 in the 2nd message) rolls back the 1st.
func TestRound102MultiappendRollbackOnFailure(t *testing.T) {
	c, _ := round82Server(t)
	good := round91Msg("one", 300)
	bad := "Subject: x\r\n\r\n\xff\xfe\r\n"
	got := round75Tagged(round102Multi(t, c, "m1", "INBOX", good, bad))
	if !strings.HasPrefix(got, "m1 NO") {
		t.Fatalf("expected NO, got %q", got)
	}
	out := c.cmd("m2 STATUS INBOX (MESSAGES)")
	if !strings.Contains(strings.Join(out, " "), "MESSAGES 0") {
		t.Fatalf("DEFECT F5841: first message of failed MULTIAPPEND kept: %q", out)
	}
}

// F5841: per-message flags of later MULTIAPPEND messages were dropped.
func TestRound102MultiappendPerMessageFlags(t *testing.T) {
	c, _ := round82Server(t)
	m1, m2 := round91Msg("one", 200), round91Msg("two", 200)
	c.write(fmt.Sprintf("m1 APPEND INBOX {%d}\r\n", len(m1)))
	c.until("+")
	c.write(fmt.Sprintf("%s (\\Seen) {%d}\r\n", m1, len(m2)))
	c.until("+")
	c.write(m2 + "\r\n")
	if got := round75Tagged(c.until("m1 ")); !strings.HasPrefix(got, "m1 OK") {
		t.Fatalf("append: %q", got)
	}
	c.cmd("m2 SELECT INBOX")
	out := strings.Join(c.cmd("m3 FETCH 1:2 (FLAGS)"), "\n")
	if !strings.Contains(out, "2 FETCH (FLAGS (\\Seen") {
		t.Fatalf("DEFECT F5841: 2nd message lost its \\Seen flag: %s", out)
	}
}

// F5842: UNSELECT (RFC 3691) returns to authenticated state without expunge.
func TestRound102Unselect(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 200))
	if caps := strings.Join(c.cmd("c0 CAPABILITY"), " "); !strings.Contains(caps, "UNSELECT") {
		t.Fatalf("UNSELECT not advertised: %s", caps)
	}
	c.cmd("u1 SELECT INBOX")
	c.cmd("u2 STORE 1 +FLAGS (\\Deleted)")
	if got := round75Tagged(c.cmd("u3 UNSELECT")); !strings.HasPrefix(got, "u3 OK") {
		t.Fatalf("DEFECT F5842: UNSELECT replied %q", got)
	}
	if got := round75Tagged(c.cmd("u4 NOOP")); !strings.HasPrefix(got, "u4 OK") {
		t.Fatalf("noop: %q", got)
	}
	if got := round75Tagged(c.cmd("u5 FETCH 1 (FLAGS)")); !strings.HasPrefix(got, "u5 BAD") {
		t.Fatalf("session still selected after UNSELECT: %q", got)
	}
	out := strings.Join(c.cmd("u6 STATUS INBOX (MESSAGES)"), " ")
	if !strings.Contains(out, "MESSAGES 1") {
		t.Fatalf("DEFECT F5842: UNSELECT expunged: %s", out)
	}
}

// F5843: LIST-EXTENDED selection and return options.
func TestRound102ListExtended(t *testing.T) {
	c, ms := round82Server(t)
	for _, n := range []string{"Sent", "Drafts", "Archive/2024"} {
		if err := ms.CreateMailbox("user@example.com", n); err != nil {
			t.Fatal(err)
		}
	}
	c.cmd("s1 SUBSCRIBE Sent")
	caps := strings.Join(c.cmd("c0 CAPABILITY"), " ")
	if !strings.Contains(caps, "LIST-EXTENDED") {
		t.Fatalf("LIST-EXTENDED not advertised: %s", caps)
	}
	out := strings.Join(c.cmd(`l1 LIST (SUBSCRIBED) "" "*"`), "\n")
	if !strings.Contains(out, `"Sent"`) || strings.Contains(out, `"Drafts"`) || !strings.Contains(out, `\Subscribed`) {
		t.Fatalf("DEFECT F5843: SUBSCRIBED selection ignored:\n%s", out)
	}
	out = strings.Join(c.cmd(`l2 LIST "" "*" RETURN (SUBSCRIBED CHILDREN SPECIAL-USE)`), "\n")
	if !strings.Contains(out, `(\HasNoChildren \Subscribed \Sent) "/" "Sent"`) ||
		!strings.Contains(out, `\Drafts) "/" "Drafts"`) || !strings.Contains(out, `(\HasChildren \Archive) "/" "Archive"`) {
		t.Fatalf("DEFECT F5843: RETURN options ignored:\n%s", out)
	}
	out = strings.Join(c.cmd(`l3 LIST (SPECIAL-USE) "" "*"`), "\n")
	if !strings.Contains(out, `"Drafts"`) || strings.Contains(out, `"INBOX"`) {
		t.Fatalf("DEFECT F5843: SPECIAL-USE selection ignored:\n%s", out)
	}
	if got := round75Tagged(c.cmd(`l4 LIST (RECURSIVEMATCH) "" "*"`)); !strings.HasPrefix(got, "l4 BAD") {
		t.Fatalf("RECURSIVEMATCH alone must be BAD: %q", got)
	}
	out = strings.Join(c.cmd(`l5 LIST "" ("Sent" "Drafts")`), "\n")
	if !strings.Contains(out, `"Sent"`) || !strings.Contains(out, `"Drafts"`) || strings.Contains(out, `"INBOX"`) {
		t.Fatalf("pattern list: %s", out)
	}
	if out = strings.Join(c.cmd(`l6 LIST "" ""`), "\n"); !strings.Contains(out, `(\Noselect) "/" ""`) {
		t.Fatalf("hierarchy delimiter query: %s", out)
	}
}

// F5844: failed-login counters are shared by IMAP listeners that share a tracker.
func TestRound102SharedAuthTracker(t *testing.T) {
	a := NewServer(&Config{}, &mockMailstore{})
	b := NewServer(&Config{}, &mockMailstore{})
	for _, s := range []*Server{a, b} {
		s.SetAuthLimits(3, time.Minute)
	}
	tr := NewAuthTracker()
	a.SetAuthTracker(tr)
	b.SetAuthTracker(tr)
	a.recordAuthFailure("10.0.0.1")
	a.recordAuthFailure("10.0.0.1")
	b.recordAuthFailure("10.0.0.1")
	if !a.isAuthLockedOut("10.0.0.1") || !b.isAuthLockedOut("10.0.0.1") {
		t.Fatal("DEFECT F5844: failures on two listeners are not summed")
	}
	a.clearAuthFailures("10.0.0.1")
	if b.isAuthLockedOut("10.0.0.1") {
		t.Fatal("success on one listener must clear the shared counter")
	}
}
