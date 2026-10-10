package imap

import (
	"strings"
	"testing"
	"time"
)

// F5643: SELECT / EXAMINE / STATUS of a mailbox that does not exist answered
// OK with an empty phantom mailbox (db.GetMailbox returns defaults), a failed
// SELECT left the previous mailbox selected, "inbox" selected a phantom
// instead of INBOX, and APPEND created a missing mailbox (RFC 3501 §6.3.1,
// §6.3.2, §6.3.10, §6.3.11: NO, [TRYCREATE] for APPEND; §6.3.1: a failed
// SELECT leaves no mailbox selected).
func TestRound82_MissingMailbox(t *testing.T) {
	c, _ := round82Server(t, round82ThreeMsgs()...)

	t.Run("control existing mailboxes", func(t *testing.T) {
		for _, cmd := range []string{"k1 SELECT Archive", "k2 EXAMINE INBOX", "k3 STATUS INBOX (MESSAGES)"} {
			if got := round75Tagged(c.cmd(cmd)); !strings.Contains(got, " OK") {
				t.Errorf("%s: %q", cmd, got)
			}
		}
	})
	for _, cmd := range []string{"s1 SELECT NoSuchBox", "s2 EXAMINE NoSuchBox", "s3 STATUS NoSuchBox (MESSAGES)"} {
		if got := round75Tagged(c.cmd(cmd)); !strings.HasPrefix(got, strings.Fields(cmd)[0]+" NO") {
			t.Errorf("DEFECT F5643: %s answered %q, want NO", cmd, got)
		}
	}
	// A failed SELECT leaves the session in the authenticated state.
	c.cmd("t0 SELECT INBOX")
	c.cmd("t1 SELECT NoSuchBox")
	out := c.cmd("t2 FETCH 1 FLAGS")
	if got := round75Line(out, "* 1 FETCH"); !strings.HasPrefix(got, "<none>") {
		t.Errorf("DEFECT F5643: FETCH after a failed SELECT still served the old mailbox: %q", got)
	}
	// "inbox" is INBOX (RFC 3501 §5.1).
	if got := round75Line(c.cmd("t3 SELECT inbox"), "* "); got != "* 3 EXISTS" {
		t.Errorf("DEFECT F5643: SELECT inbox first line %q, want \"* 3 EXISTS\"", got)
	}
	// APPEND to a missing mailbox: NO [TRYCREATE], nothing created.
	out = round82Append(t, c, "p1", "NoSuchBox", round75Msg("Subject: x"))
	if got := round75Tagged(out); !strings.HasPrefix(got, "p1 NO [TRYCREATE]") {
		t.Errorf("DEFECT F5643: APPEND to a missing mailbox answered %q, want NO [TRYCREATE]", got)
	}
	if got := strings.Join(c.cmd("l1 LIST \"\" \"*\""), "\n"); strings.Contains(got, "NoSuchBox") {
		t.Errorf("DEFECT F5643: LIST shows a mailbox that was never created: %q", got)
	}
}

// F5644: the read-receipt (MDN) hook ran on every FETCH that loaded the
// message body, including BODY.PEEK and BODYSTRUCTURE, so a client that only
// prefetched or peeked (no \Seen, never displayed) made the server send a
// "displayed" receipt on the user's behalf (RFC 8098 §2.1: only the user's
// action may trigger it; RFC 3501 §6.4.5: PEEK has no side effects).
func TestRound82_PeekDoesNotSendMDN(t *testing.T) {
	msg := round75Msg("From: a@x", "To: user@example.com", "Subject: rr",
		"Disposition-Notification-To: <a@x>", "Message-ID: <rr@x>")
	mdnSeen := func(ms *BboltMailstore) int {
		ms.mdnSentMu.Lock()
		defer ms.mdnSentMu.Unlock()
		return len(ms.mdnSent)
	}
	for _, tc := range []struct {
		cmd      string
		wantSend bool
	}{
		{"f1 UID FETCH 1 (BODY.PEEK[])", false},
		{"f2 UID FETCH 1 (BODY.PEEK[HEADER])", false},
		{"f3 UID FETCH 1 BODYSTRUCTURE", false},
		{"f4 UID FETCH 1 (BODY[])", true}, // control: a real read
		{"f5 UID FETCH 1 RFC822", true},
	} {
		t.Run(strings.Fields(tc.cmd)[0], func(t *testing.T) {
			c, ms := round82Server(t, msg)
			sent := make(chan string, 4)
			ms.SetMDNHandler(func(from, to, messageID, inReplyTo string, data []byte) error {
				sent <- to
				return nil
			})
			c.cmd("s0 SELECT INBOX")
			c.cmd(tc.cmd)
			if tc.wantSend {
				select {
				case <-sent:
				case <-time.After(5 * time.Second):
					t.Errorf("no MDN for %s", tc.cmd)
				}
				return
			}
			// checkAndSendMDN records the message synchronously before it
			// hands the receipt to its goroutine.
			if n := mdnSeen(ms); n != 0 {
				t.Errorf("DEFECT F5644: %s triggered the MDN hook", tc.cmd)
			}
		})
	}
}

// F5645: checkAndSendMDN stripped the header name with a lower-case prefix
// against the original-case line, so the canonical "Disposition-Notification-To:
// <a@x>" made the receipt go to the string "Disposition-Notification-To:
// <a@x>", and a display name ("Alice <a@x>") was passed through as is
// (RFC 8098 §2.1: the field holds an address list).
func TestRound82_MDNRecipientParsed(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"disposition-notification-to: a@x", "a@x"}, // control: already worked
		{"Disposition-Notification-To: <a@x>", "a@x"},
		{"Disposition-Notification-To: Alice <a@x>", "a@x"},
		{"Disposition-Notification-To: \"Q, A\" <a@x>", "a@x"},
	} {
		t.Run(tc.header, func(t *testing.T) {
			c, ms := round82Server(t, round75Msg("From: a@x", "Subject: rr", tc.header, "Message-ID: <rr@x>"))
			sent := make(chan string, 1)
			ms.SetMDNHandler(func(from, to, messageID, inReplyTo string, data []byte) error {
				sent <- to
				return nil
			})
			c.cmd("s0 SELECT INBOX")
			c.cmd("f1 UID FETCH 1 (BODY[])")
			select {
			case to := <-sent:
				if to != tc.want {
					t.Errorf("DEFECT F5645: MDN recipient %q, want %q", to, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("no MDN sent")
			}
		})
	}
}
