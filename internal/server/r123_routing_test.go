package server

// Regression tests for inbound recipient routing (audit round 123,
// F6050-F6056): recipient normalisation, alias chains/loops/external
// targets, plus-addressing, relay NOTIFY alignment and the Sieve-path
// redirect loop check.

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
)

func pendingTo(t *testing.T, s *Server) map[string]*db.QueueEntry {
	t.Helper()
	entries, err := s.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	m := map[string]*db.QueueEntry{}
	for _, e := range entries {
		for _, to := range e.To {
			m[to] = e
		}
	}
	return m
}

func inboxCount(t *testing.T, s *Server, email string) int {
	t.Helper()
	n := 0
	for uid := uint32(1); uid < 20; uid++ {
		if m, err := s.storageDB.GetMessageMetadata(email, "INBOX", uid); err == nil && m != nil && m.MessageID != "" {
			n++
		}
	}
	return n
}

func mkAlias(t *testing.T, s *Server, alias, target string) {
	t.Helper()
	if err := s.database.CreateAlias(&db.AliasData{Domain: "test.com", Alias: alias, Target: target, IsActive: true}); err != nil {
		t.Fatal(err)
	}
}

// F6050: a recipient differing only in case or with a trailing dot on the
// domain was not found as a local domain and was queued for outbound relay.
func TestR123RecipientCaseAndTrailingDot(t *testing.T) {
	for _, rcpt := range []string{"Bob@TEST.com", "bob@test.com.", "BOB@Test.Com."} {
		s := newDeliveryAuditServer(t)
		if err := s.deliverMessageWithNotify("s@ext.com", []string{rcpt}, nil, []byte(deliveryAuditMsg)); err != nil {
			t.Fatalf("%s: %v", rcpt, err)
		}
		if inboxCount(t, s, "bob@test.com") != 1 {
			t.Errorf("%s: not delivered to bob's INBOX", rcpt)
		}
		if len(pendingTo(t, s)) != 0 {
			t.Errorf("%s: was queued for relay instead of delivered locally", rcpt)
		}
	}
}

// F6051: an alias whose target is an external address was delivered "locally"
// to a non-existent mailbox and bounced.
func TestR123AliasToExternalRelays(t *testing.T) {
	s := newDeliveryAuditServer(t)
	mkAlias(t, s, "ext", "someone@other.org")
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"ext@test.com"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if _, ok := pendingTo(t, s)["someone@other.org"]; !ok {
		t.Fatalf("message not queued to the external alias target: %v", pendingTo(t, s))
	}
}

// F6052: chained aliases were resolved one hop only; loops must fail, not hang.
func TestR123AliasChainAndLoop(t *testing.T) {
	s := newDeliveryAuditServer(t)
	mkAlias(t, s, "a1", "a2@test.com")
	mkAlias(t, s, "a2", "bob@test.com")
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"a1@test.com"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("chain: %v", err)
	}
	if inboxCount(t, s, "bob@test.com") != 1 {
		t.Error("chained alias did not reach bob")
	}
	mkAlias(t, s, "l1", "l2@test.com")
	mkAlias(t, s, "l2", "l1@test.com")
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"l1@test.com"}, nil, []byte(deliveryAuditMsg)); err == nil {
		t.Error("alias loop must be refused")
	}
}

// F6053: user+tag@ plus-addressing delivers to the base mailbox.
func TestR123PlusAddressing(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"bob+news@test.com"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if inboxCount(t, s, "bob@test.com") != 1 {
		t.Error("plus-addressed mail did not reach bob")
	}
}

// F6054: relay entries took notify[0] instead of the recipient's own value.
func TestR123RelayNotifyAligned(t *testing.T) {
	s := newDeliveryAuditServer(t)
	err := s.deliverMessageWithNotify("s@ext.com", []string{"bob@test.com", "far@other.org"}, []string{"SUCCESS", "NEVER"}, []byte(deliveryAuditMsg))
	if err != nil {
		t.Fatal(err)
	}
	e := pendingTo(t, s)["far@other.org"]
	if e == nil {
		t.Fatal("no queue entry")
	}
	if e.Notify != db.DSNNotify(queue.ParseDSNNotify("NEVER")) {
		t.Errorf("relay entry notify = %v, want NEVER", e.Notify)
	}
}

// F6055: the redirect loop check in the Sieve-action path used a `continue`
// that only advanced the inner loop, so loops were never skipped.
func TestR123SieveRedirectLoopSkipped(t *testing.T) {
	s := newDeliveryAuditServer(t)
	msg := "X-Mail-Loop: x@far.org\r\n" + deliveryAuditMsg
	if err := s.deliverMessageWithSieve("s@ext.com", []string{"bob@test.com"}, []byte(msg), []string{"redirect:x@far.org"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := pendingTo(t, s)["x@far.org"]; ok {
		t.Error("looping redirect was queued")
	}
	if !strings.Contains(msg, "x@far.org") {
		t.Fatal("setup")
	}
}
