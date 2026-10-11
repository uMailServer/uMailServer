package server

import (
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/sieve"
)

func r163Queued(t *testing.T, srv *Server) ([]string, []string) {
	t.Helper()
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatal(err)
	}
	var froms []string
	for _, e := range entries {
		froms = append(froms, e.From)
	}
	return froms, vacationQueued(t, srv)
}

// F6444 (High): auto-reply envelope sender must be null (RFC 5230/3834).
func TestR163ReplyEnvelopeSenderNull(t *testing.T) {
	srv := vacationTestServer(t)
	srv.sendVacationReply("me@example.com", "a@example.org", `{"enabled":true,"message":"away","subject":"Gone"}`)
	srv.handleSieveVacation("b@example.org", "me@example.com", sieve.VacationAction{Seconds: 60, From: "other@x.com"})
	froms, msgs := r163Queued(t, srv)
	if len(froms) != 2 {
		t.Fatalf("want 2 replies, got %d", len(froms))
	}
	for _, f := range froms {
		if f != "" {
			t.Errorf("envelope sender %q, want null", f)
		}
	}
	if !strings.Contains(strings.Join(msgs, "|"), "Subject: Gone\r\n") {
		t.Errorf("configured subject ignored: %q", msgs)
	}
}

// F6440: automated/list/self senders and angle-bracket forms get no reply.
func TestR163NoReplyToAutomatedOrSelf(t *testing.T) {
	srv := vacationTestServer(t)
	cfg := `{"enabled":true,"message":"away"}`
	for _, s := range []string{"<>", "<mailer-daemon@x.org>", "Owner-list@x.org", "list-request@x.org", "Donotreply@x.org", "ME@example.com", "<me@example.com>"} {
		srv.sendVacationReply("me@example.com", s, cfg)
	}
	srv.handleSieveVacation("owner-x@x.org", "me@example.com", sieve.VacationAction{Seconds: 60})
	srv.handleSieveVacation("me@example.com", "me@example.com", sieve.VacationAction{Seconds: 60})
	if n := len(vacationQueued(t, srv)); n != 0 {
		t.Fatalf("expected no replies, got %d", n)
	}
}

// F6441: exclude list and ignore_lists in the settings are honoured.
func TestR163ExcludeAddresses(t *testing.T) {
	srv := vacationTestServer(t)
	srv.sendVacationReply("me@example.com", "Boss@Example.org", `{"enabled":true,"message":"x","exclude_addresses":["boss@example.org"]}`)
	if n := len(vacationQueued(t, srv)); n != 0 {
		t.Fatalf("excluded sender answered")
	}
}

// F6442: non-positive interval defaults to 7 days (not 24h); positive honoured.
func TestR163DefaultIntervalSevenDays(t *testing.T) {
	srv := vacationTestServer(t)
	cfg := `{"enabled":true,"message":"x"}`
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	ageVacationReplies(srv, 25*time.Hour)
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("want 1 reply inside default 7d window, got %d", n)
	}
	ageVacationReplies(srv, 7*24*time.Hour)
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	if n := len(vacationQueued(t, srv)); n != 2 {
		t.Fatalf("want reply after window, got %d", n)
	}
}

// F6443 (High): dedup survives a restart.
func TestR163DedupPersisted(t *testing.T) {
	srv := vacationTestServer(t)
	srv.config.Server.DataDir = t.TempDir()
	cfg := `{"enabled":true,"message":"x"}`
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	// simulate restart
	srv.vacationRepliesMu.Lock()
	srv.vacationReplies = nil
	srv.vacationRepliesMu.Unlock()
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("want 1 reply across restart, got %d", n)
	}
}

// F6445: RFC 3339 dates (what the vacation Manager stores) are honoured and
// the date-only end covers its whole day.
func TestR163DateFormats(t *testing.T) {
	srv := vacationTestServer(t)
	future := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	srv.sendVacationReply("me@example.com", "a@example.org", `{"enabled":true,"message":"x","start_date":"`+future+`"}`)
	past := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	srv.sendVacationReply("me@example.com", "b@example.org", `{"enabled":true,"message":"x","end_date":"`+past+`"}`)
	if n := len(vacationQueued(t, srv)); n != 0 {
		t.Fatalf("out-of-window replies sent: %d", n)
	}
	today := time.Now().Format("2006-01-02")
	srv.sendVacationReply("me@example.com", "c@example.org", `{"enabled":true,"message":"x","end_date":"`+today+`"}`)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("end date day should be inclusive, got %d", n)
	}
}

// F6446: subject/body injection stays out of the header block.
func TestR163HeaderInjection(t *testing.T) {
	srv := vacationTestServer(t)
	srv.sendVacationReply("me@example.com", "a@example.org", `{"enabled":true,"subject":"Hi\r\nBcc: evil@x.com","message":"m"}`)
	_, msgs := r163Queued(t, srv)
	head := strings.SplitN(msgs[0], "\r\n\r\n", 2)[0]
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("header injected: %q", head)
	}
	for _, h := range []string{"Auto-Submitted: auto-replied", "Message-ID: <", "Content-Type: text/plain; charset=utf-8"} {
		if !strings.Contains(head, h) {
			t.Errorf("missing %q", h)
		}
	}
}

var _ = queue.NewManager
