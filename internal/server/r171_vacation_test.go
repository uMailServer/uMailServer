package server

import (
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F6510 (High): vacation replies carry In-Reply-To/References and are From
// the address the sender wrote to (alias / plus address), not only the mailbox.
func TestR171VacationReplyThreadingAndAlias(t *testing.T) {
	s := newDeliveryAuditServer(t)
	round89SetVacation(t, s, "bob")
	if err := s.database.CreateAlias(&db.AliasData{Alias: "sales", Domain: "test.com", Target: "bob@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	msg := "From: s@ext.com\r\nTo: sales@test.com\r\nSubject: Quote\r\nMessage-ID: <m1@ext.com>\r\nReferences: <r0@ext.com>\r\n\r\nhello\r\n"
	if err := s.deliverMessage("s@ext.com", []string{"sales@test.com"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	round89WaitIdle(t, s)
	replies := vacationQueued(t, s)
	if len(replies) != 1 {
		t.Fatalf("want 1 reply, got %d", len(replies))
	}
	r := replies[0]
	for _, want := range []string{"From: sales@test.com\r\n", "In-Reply-To: <m1@ext.com>\r\n", "References: <r0@ext.com> <m1@ext.com>\r\n"} {
		if !strings.Contains(r, want) {
			t.Errorf("reply missing %q:\n%s", want, r)
		}
	}
}

// Replying to oneself through an alias must still be suppressed.
func TestR171VacationNoSelfReplyViaAlias(t *testing.T) {
	s := newDeliveryAuditServer(t)
	round89SetVacation(t, s, "bob")
	_ = s.database.CreateAlias(&db.AliasData{Alias: "sales", Domain: "test.com", Target: "bob@test.com", IsActive: true})
	s.sendVacationReplyCtx("bob@test.com", "bob@test.com", "sales@test.com", vacationMsgInfo{MessageID: "<a@b>"}, round89Vacation)
	if n := len(vacationQueued(t, s)); n != 0 {
		t.Fatalf("self reply queued")
	}
	s.sendVacationReplyCtx("bob@test.com", "sales@test.com", "bob@test.com", vacationMsgInfo{}, round89Vacation)
	if n := len(vacationQueued(t, s)); n != 0 {
		t.Fatalf("alias-sender self reply queued")
	}
}

func r171Inbox(t *testing.T, s *Server, user string) int {
	t.Helper()
	n, _, _, err := s.storageDB.GetMailboxCounts(user+"@test.com", "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// F6511 (High): the same mailbox named twice (directly, via alias, via plus
// address or in another case) must receive one copy, and external duplicates
// one queue entry.
func TestR171DuplicateRecipientsDeliveredOnce(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.database.CreateAlias(&db.AliasData{Alias: "sales", Domain: "test.com", Target: "bob@test.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	to := []string{"bob@test.com", "BOB@test.com", "sales@test.com", "bob+x@test.com", "far@ext.com", "FAR@ext.com"}
	if err := s.deliverMessage("s@ext.com", to, []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	if err := s.deliverMessageWithNotify("s@ext.com", to, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	if n := r171Inbox(t, s, "bob"); n != 2 { // one per call
		t.Errorf("bob got %d copies for 2 deliveries, want 2", n)
	}
	entries, _ := s.queue.GetPendingEntries()
	if len(entries) != 2 {
		t.Errorf("queued %d relay entries for 2 deliveries, want 2", len(entries))
	}
}

// F6512: a message with an absurd Received chain is a loop and is refused.
func TestR171ReceivedHopLimit(t *testing.T) {
	s := newDeliveryAuditServer(t)
	hdr := strings.Repeat("Received: from a by b; Mon, 1 Jan 2024 00:00:00 +0000\r\n", 120)
	err := s.deliverMessageWithNotify("s@ext.com", []string{"bob@test.com"}, nil, []byte(hdr+deliveryAuditMsg))
	if err == nil {
		t.Fatal("looping message accepted")
	}
	if n := r171Inbox(t, s, "bob"); n != 0 {
		t.Errorf("looping message stored")
	}
}

// F6513: Stop must be able to wait for the vacation/push goroutines a
// delivery spawned, so they do not touch closed stores.
func TestR171BackgroundTasksTracked(t *testing.T) {
	s := newDeliveryAuditServer(t)
	round89SetVacation(t, s, "bob")
	if err := s.deliverMessage("s@ext.com", []string{"bob@test.com"}, []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	s.waitBackground(5 * time.Second)
	if len(s.bgSem) != 0 {
		t.Fatal("background task still running after waitBackground")
	}
	if n := len(vacationQueued(t, s)); n != 1 {
		t.Fatalf("vacation reply not queued by the time background tasks were awaited: %d", n)
	}
}

// F6514 (High): a panic while indexing one message must not kill the index
// worker; the remaining jobs are still consumed.
func TestR171IndexWorkerSurvivesPanic(t *testing.T) {
	s := newDeliveryAuditServer(t)
	s.searchSvc = nil // IndexMessage on a nil service panics
	s.indexWork = make(chan indexJob, 4)
	for i := 0; i < 3; i++ {
		s.indexWork <- indexJob{email: "bob@test.com", uid: uint32(i + 1)}
	}
	close(s.indexWork)
	s.wg.Add(1)
	done := make(chan struct{})
	go func() { s.runIndexWorker(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker hung")
	}
	if n := len(s.indexWork); n != 0 {
		t.Fatalf("worker died on first panic, %d jobs unprocessed", n)
	}
}
