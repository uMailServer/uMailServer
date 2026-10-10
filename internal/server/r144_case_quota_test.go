package server

// Regression tests for audit round 144 (F6260-F6263): address case policy at
// the server entry points and the mailbox-full sentinel for quota refusals.

import (
	"errors"
	"testing"

	"github.com/umailserver/umailserver/internal/smtp"
	"golang.org/x/crypto/bcrypt"
)

// F6260: authenticate looked the account up with the raw login string, so
// "Bob@Test.com" failed against the lowercase-stored account.
func TestR144AuthenticateNormalisesAddress(t *testing.T) {
	s := newDeliveryAuditServer(t)
	acc, err := s.database.GetAccount("test.com", "bob")
	if err != nil {
		t.Fatal(err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	acc.PasswordHash = string(h)
	if err := s.database.UpdateAccount(acc); err != nil {
		t.Fatal(err)
	}
	for _, login := range []string{"bob@test.com", "Bob@Test.COM", "  BOB@test.com "} {
		ok, err := s.authenticate(login, "pw")
		if err != nil || !ok {
			t.Errorf("authenticate(%q) = %v, %v; want success", login, ok, err)
		}
		if _, err := s.getUserSecret(login); err != nil {
			t.Errorf("getUserSecret(%q): %v", login, err)
		}
	}
}

// F6261: a quota refusal must wrap smtp.ErrMailboxFull (452 4.2.2) and still
// carry the "quota exceeded" text, through the multi-recipient wrappers.
func TestR144QuotaWrapsMailboxFull(t *testing.T) {
	s := newDeliveryAuditServer(t)
	err := s.deliverLocal("alice", "test.com", "s@ext.com", []byte(deliveryAuditMsg))
	if !errors.Is(err, smtp.ErrMailboxFull) {
		t.Fatalf("deliverLocal err = %v; want wrapping ErrMailboxFull", err)
	}
	err = s.deliverMessageWithNotify("s@ext.com", []string{"Alice@TEST.com"}, nil, []byte(deliveryAuditMsg))
	if !errors.Is(err, smtp.ErrMailboxFull) {
		t.Fatalf("deliverMessage err = %v; want wrapping ErrMailboxFull", err)
	}
}

// F6262: catch-all targets and delivery are case-insensitive.
func TestR144DeliverLocalNormalisesCase(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverLocal("Bob", "Test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("deliverLocal mixed case: %v", err)
	}
	if inboxCount(t, s, "bob@test.com") != 1 {
		t.Error("message not stored under the lowercase address")
	}
}

// F6263: the vacation dedup key was case-sensitive, so a sender writing their
// address in a different case got a second auto-reply inside the interval.
func TestR144VacationDedupCaseInsensitive(t *testing.T) {
	srv := vacationTestServer(t)
	cfg := `{"enabled":true,"message":"away"}`
	srv.sendVacationReply("me@example.com", "a@example.org", cfg)
	srv.sendVacationReply("Me@Example.com", "A@Example.org", cfg)
	if n := len(vacationQueued(t, srv)); n != 1 {
		t.Fatalf("expected 1 reply, got %d", n)
	}
}
