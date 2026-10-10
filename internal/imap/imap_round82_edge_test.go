package imap

import (
	"crypto/tls"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Edge cases around the Round 82 fixes (F5640-F5647).

func TestRound82_Edge_CopyMoveBoundaries(t *testing.T) {
	c, ms := round82Server(t, round82ThreeMsgs()...)
	round82Unreadable(t, ms, 2)
	c.cmd("s0 SELECT INBOX")
	// A set that does not include the unreadable message still works.
	validity := round82UIDValidity(t, c, "Archive")
	if got, want := round75Tagged(c.cmd("e1 COPY 1,3 Archive")), "e1 OK [COPYUID "+validity+" 1,3 1:2] COPY completed"; got != want {
		t.Errorf("COPY 1,3: %q, want %q", got, want)
	}
	// A failed MOVE leaves \Deleted unset on the messages it did copy first.
	if got := round75Tagged(c.cmd("e2 MOVE 1:3 Archive")); !strings.HasPrefix(got, "e2 NO") {
		t.Errorf("MOVE 1:3: %q", got)
	}
	if got := round75Line(c.cmd("e3 UID SEARCH DELETED"), "* SEARCH"); got != "* SEARCH" {
		t.Errorf("failed MOVE flagged messages \\Deleted: %q", got)
	}
	if n := round82Messages(t, c, "Archive"); n != "2" {
		t.Errorf("Archive has %s messages after rollback, want the 2 from e1", n)
	}
	// An empty / nonexistent selection is not a failure.
	if got := round75Tagged(c.cmd("e4 UID MOVE 99 Archive")); !strings.HasPrefix(got, "e4 OK") {
		t.Errorf("UID MOVE 99: %q", got)
	}
}

func TestRound82_Edge_MoveIntoSameMailbox(t *testing.T) {
	c, _ := round82Server(t, round82ThreeMsgs()...)
	c.cmd("s0 SELECT INBOX")
	validity := round82UIDValidity(t, c, "INBOX")
	out := c.cmd("m1 MOVE 1:3 INBOX")
	if got, want := round75Line(out, "* OK [COPYUID"), "* OK [COPYUID "+validity+" 1:3 4:6] Moved"; got != want {
		t.Errorf("COPYUID %q, want %q", got, want)
	}
	if got := round82Search(c, "m2", "ALL"); got != "* SEARCH 4 5 6" {
		t.Errorf("INBOX after self-MOVE: %q, want the three copies", got)
	}
}

func TestRound82_Edge_AppendUIDVariants(t *testing.T) {
	c, _ := round82Server(t)
	validity := round82UIDValidity(t, c, "INBOX")
	msg := round75Msg("Subject: v")
	// Flags and date-time before the literal; lower-case inbox.
	c.write("a1 APPEND inbox (\\Seen) \"10-Jan-2024 12:00:00 +0000\" {" + strconv.Itoa(len(msg)) + "}\r\n")
	c.until("+")
	c.write(msg + "\r\n")
	if got, want := round75Tagged(c.until("a1 ")), "a1 OK [APPENDUID "+validity+" 1] APPEND completed"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := round75Tagged(round82Append(t, c, "a2", "INBOX", msg+"x")); !strings.Contains(got, "[APPENDUID "+validity+" 2]") {
		t.Errorf("second APPEND: %q", got)
	}
}

func TestRound82_Edge_EnvelopeVariants(t *testing.T) {
	c, _ := round82Server(t,
		round75Msg("Subject: =?UTF-8?B?Y2Fmw6k=?=", "From: undisclosed"),
		round75Msg("From: a@x", "Sender: s@x", "Reply-To: r@x", "To: Group: g1@x, g2@x;"))
	c.cmd("s0 SELECT INBOX")
	// An encoded-word subject is passed through undecoded (RFC 3501 §7.4.2);
	// everything absent is NIL, an unparsable address list is NIL.
	v, err := round82FetchItem(t, c.cmd("a1 FETCH 1 ENVELOPE"), "ENVELOPE")
	env, ok := v.([]interface{})
	if err != nil || !ok || len(env) != 10 {
		t.Fatalf("ENVELOPE %v %v", v, err)
	}
	if env[0] != nil || env[1] != "=?UTF-8?B?Y2Fmw6k=?=" {
		t.Errorf("date/subject: %#v %#v", env[0], env[1])
	}
	for i := 2; i < 10; i++ {
		if env[i] != nil {
			t.Errorf("field %d = %#v, want NIL", i, env[i])
		}
	}
	// Explicit Sender / Reply-To win over From.
	out := c.cmd("a2 UID FETCH 2 (ENVELOPE UID)")
	full := strings.Join(out, "\r\n")
	if !strings.Contains(full, `(NIL NIL "s" "x")`) || !strings.Contains(full, `(NIL NIL "r" "x")`) || !strings.Contains(full, "UID 2") {
		t.Errorf("Sender/Reply-To/UID: %q", full)
	}
}

func TestRound82_Edge_MailboxLifecycle(t *testing.T) {
	c, _ := round82Server(t)
	for _, cmd := range []string{"a1 CREATE Fresh", "a2 SELECT Fresh", "a3 SELECT INBOX", "a4 SELECT inbox", "a5 STATUS Inbox (MESSAGES)", "a6 DELETE Fresh"} {
		if got := round75Tagged(c.cmd(cmd)); !strings.HasPrefix(got, strings.Fields(cmd)[0]+" OK") {
			t.Errorf("%s: %q", cmd, got)
		}
	}
	if got := round75Tagged(c.cmd("a7 SELECT Fresh")); !strings.HasPrefix(got, "a7 NO [NONEXISTENT]") {
		t.Errorf("SELECT after DELETE: %q", got)
	}
	// Quoted names and a name that is only a prefix of an existing one.
	if got := round75Tagged(c.cmd(`a8 SELECT "Archive"`)); !strings.HasPrefix(got, "a8 OK") {
		t.Errorf("quoted: %q", got)
	}
	if got := round75Tagged(c.cmd("a9 SELECT Arch")); !strings.HasPrefix(got, "a9 NO") {
		t.Errorf("prefix of an existing name: %q", got)
	}
}

func TestRound82_Edge_MDNVariants(t *testing.T) {
	for _, tc := range []struct {
		name, cmd string
		msg       string
		wantSend  bool
	}{
		{"size only", "f1 UID FETCH 1 (FLAGS RFC822.SIZE)", round75Msg("Disposition-Notification-To: a@x"), false},
		{"non-peek section", "f2 UID FETCH 1 (BODY[HEADER])", round75Msg("Disposition-Notification-To: a@x"), true},
		{"header text in body", "f3 UID FETCH 1 (BODY[])", "Subject: s\r\n\r\nDisposition-Notification-To: evil@x\r\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ms := round82Server(t, tc.msg)
			sent := make(chan string, 2)
			ms.SetMDNHandler(func(from, to, messageID, inReplyTo string, data []byte) error {
				sent <- to
				return nil
			})
			c.cmd("s0 SELECT INBOX")
			c.cmd(tc.cmd)
			select {
			case to := <-sent:
				if !tc.wantSend {
					t.Errorf("unexpected MDN to %q", to)
				} else if to != "a@x" {
					t.Errorf("MDN to %q", to)
				}
			case <-time.After(300 * time.Millisecond):
				if tc.wantSend {
					t.Errorf("no MDN")
				}
			}
		})
	}
}

func TestRound82_Edge_Capabilities(t *testing.T) {
	// Plain auth allowed (test/loopback servers): mechanisms stay, no LOGINDISABLED.
	c, _ := round82Server(t)
	caps := round75Line(c.cmd("c1 CAPABILITY"), "* CAPABILITY")
	if !round82HasCap(caps, "AUTH=PLAIN") || round82HasCap(caps, "LOGINDISABLED") || round82HasCap(caps, "STARTTLS") || round82HasCap(caps, "IMAP4rev2") {
		t.Errorf("allowPlainAuth session: %q", caps)
	}
	// Cleartext, no TLS configuration: no STARTTLS to offer, LOGINDISABLED.
	srv := NewServer(&Config{}, nil)
	s := NewSession(nil, srv)
	if got := strings.Join(s.sessionCapabilities(), " "); round82HasCap(got, "STARTTLS") || !round82HasCap(got, "LOGINDISABLED") || round82HasCap(got, "AUTH=PLAIN") {
		t.Errorf("no TLS config: %q", got)
	}
	// Implicit TLS connection type is recognised.
	srv = NewServer(&Config{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, nil)
	if s := NewSession((*tls.Conn)(nil), srv); !s.tlsActive {
		t.Error("*tls.Conn session not marked tlsActive")
	}
	if got := round75Line(c.cmd("c2 ENABLE IMAP4rev2"), "* ENABLED"); got != "* ENABLED" {
		t.Errorf("ENABLE IMAP4rev2: %q", got)
	}
}
