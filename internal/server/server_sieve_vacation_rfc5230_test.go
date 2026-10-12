package server

import (
	"strings"
	"testing"
)

// RFC 5230 §4.5/§4.6 on the Sieve delivery path (R244, F7210-F7219).

const sieveVacBase = "Subject: hello\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\nMessage-ID: <c@x.example>\r\n"

func sieveVacMsg(to string, extra string) string {
	return "To: " + to + "\r\n" + extra + sieveVacBase + "\r\nhi"
}

func sieveVacHeader(msg, name string) string {
	for _, ln := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(strings.ToLower(ln), strings.ToLower(name)+":") {
			return strings.TrimSpace(ln[len(name)+1:])
		}
	}
	return ""
}

// F7210: a message not addressed (To/Cc) to the user gets no reply.
func TestSieveVacationNotAddressedToMe(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("list@lists.example", ""))
	if got := sieveVacReplies(t, srv); got != 0 {
		t.Fatalf("replied to a message not addressed to the user: %d", got)
	}
	// control: addressed via Cc
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("x@y.example", "Cc: Alice <alice@test.com>\r\n"))
	if got := sieveVacReplies(t, srv); got != 1 {
		t.Fatalf("control: Cc-addressed message got %d replies, want 1", got)
	}
}

// F7211: :addresses extends the set of own addresses; the reply is From the
// matched address.
func TestSieveVacationAddressesAndFrom(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation :addresses [\"alice@other.example\"] \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("Alice <alice@other.example>", ""))
	q := sieveDTQueued(t, srv, sieveVacSender)
	if len(q) != 1 {
		t.Fatalf("want 1 reply via :addresses, got %d", len(q))
	}
	if from := sieveVacHeader(q[0], "From"); !strings.Contains(from, "alice@other.example") {
		t.Fatalf("reply From = %q, want the matched address alice@other.example", from)
	}
}

// F7212: without :addresses the foreign address does not count.
func TestSieveVacationForeignAddressWithoutAddresses(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("alice@other.example", ""))
	if got := sieveVacReplies(t, srv); got != 0 {
		t.Fatalf("got %d replies, want 0", got)
	}
}

// F7213: different :handle values are independent suppression keys; same
// handle is suppressed.
func TestSieveVacationHandleInKey(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation :handle \"a\" \"ooo\";\n")
	m := sieveVacMsg("alice@test.com", "")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	if got := sieveVacReplies(t, srv); got != 1 {
		t.Fatalf("same handle: got %d, want 1", got)
	}
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation :handle \"b\" \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("new handle: got %d, want 2", got)
	}
}

// F7214: without :handle the key derives from subject+reason+mime, so a
// changed reason replies again.
func TestSieveVacationDerivedHandle(t *testing.T) {
	srv := junkRTServer(t, true)
	m := sieveVacMsg("alice@test.com", "")
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"one\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"two\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

// F7215: :seconds windows must also key on the handle (second claim path).
func TestSieveVacationSecondsHandleIndependent(t *testing.T) {
	srv := junkRTServer(t, true)
	m := sieveVacMsg("alice@test.com", "")
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation-seconds\"];\nvacation :seconds 600 :handle \"a\" \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	sieveDTScript(t, srv, "alice@test.com", "require [\"vacation-seconds\"];\nvacation :seconds 600 :handle \"b\" \"ooo\";\n")
	sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, m)
	if got := sieveVacReplies(t, srv); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

// F7216-F7219: RFC 3834 / RFC 5230 §4.6 message-level suppression.
func TestSieveVacationMessageHeaderSuppression(t *testing.T) {
	cases := map[string]string{
		"auto-submitted": "Auto-Submitted: auto-generated\r\n",
		"precedence":     "Precedence: bulk\r\n",
		"list-id":        "List-Id: <l.example>\r\n",
		"list-unsub":     "List-Unsubscribe: <mailto:u@l.example>\r\n",
		"auto-suppress":  "X-Auto-Response-Suppress: All\r\n",
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := junkRTServer(t, true)
			sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"ooo\";\n")
			sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("alice@test.com", h))
			if got := sieveVacReplies(t, srv); got != 0 {
				t.Fatalf("replied despite %s: %d", h, got)
			}
		})
	}
	t.Run("auto-submitted-no-control", func(t *testing.T) {
		srv := junkRTServer(t, true)
		sieveDTScript(t, srv, "alice@test.com", "require [\"vacation\"];\nvacation \"ooo\";\n")
		sieveDTSend(t, srv, sieveVacSender, []string{"alice@test.com"}, sieveVacMsg("alice@test.com", "Auto-Submitted: no\r\n"))
		if got := sieveVacReplies(t, srv); got != 1 {
			t.Fatalf("Auto-Submitted: no must reply, got %d", got)
		}
	})
}
