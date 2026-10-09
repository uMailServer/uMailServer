package server

// Regression tests for F5117: vacation auto-replies keep the line breaks of
// a multi-line vacation text (as CRLF); only header values are flattened.

import (
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

func vacationBodyQueued(t *testing.T, srv *Server, rcpt string) string {
	t.Helper()
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	for _, e := range entries {
		if len(e.To) == 1 && e.To[0] == rcpt {
			b, err := os.ReadFile(e.MessagePath)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			msg := string(b)
			i := strings.Index(msg, "\r\n\r\n")
			if i < 0 {
				t.Fatalf("no header/body separator:\n%q", msg)
			}
			return msg[i+4:]
		}
	}
	t.Fatalf("no queued reply to %s", rcpt)
	return ""
}

func TestVacationBodyNormalisesLineBreaks(t *testing.T) {
	for in, want := range map[string]string{
		"one":              "one",
		"a\nb":             "a\r\nb",
		"a\r\nb\r\n":       "a\r\nb\r\n",
		"a\rb":             "a\r\nb",
		"a\n\nb":           "a\r\n\r\nb",
		"mixed\r\n\n\rend": "mixed\r\n\r\n\r\nend",
	} {
		if got := vacationBody(in); got != want {
			t.Errorf("vacationBody(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSieveVacationKeepsBodyLines(t *testing.T) {
	srv := junkRTServer(t, false)
	srv.handleSieveVacation("bob@remote.example", "alice@test.com",
		sieve.VacationAction{Subject: "Away\r\nBcc: x@evil.example", Body: "I am away.\nBack Monday.\r\n\r\n-- Alice", Seconds: 60})
	if got, want := vacationBodyQueued(t, srv, "bob@remote.example"), "I am away.\r\nBack Monday.\r\n\r\n-- Alice"; got != want {
		t.Fatalf("F5117: body = %q, want %q", got, want)
	}
	entries, _ := srv.queue.GetPendingEntries()
	b, _ := os.ReadFile(entries[0].MessagePath)
	if strings.Contains(string(b), "\r\nBcc:") {
		t.Fatalf("subject CR/LF not stripped:\n%s", b)
	}
}

func TestAccountVacationKeepsBodyLines(t *testing.T) {
	srv := junkRTServer(t, false)
	srv.sendVacationReply("alice@test.com", "dave@remote.example", `{"enabled":true,"message":"Out until 5th.\nUrgent: call 555.\n"}`)
	if got, want := vacationBodyQueued(t, srv, "dave@remote.example"), "Out until 5th.\r\nUrgent: call 555.\r\n"; got != want {
		t.Fatalf("F5117: body = %q, want %q", got, want)
	}
}
