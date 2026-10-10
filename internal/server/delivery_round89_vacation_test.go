package server

// Regression tests for Round 89 inbound-delivery findings:
//
//   - F5710: the account vacation auto-reply was sent for mail filed into
//     Junk (spam verdict), mailing every forged spam sender (backscatter).
//   - F5711: it was also sent for a null envelope sender (a bounce).
//   - F5712: it ignored RFC 3834 / RFC 5230 §4.6 auto-reply loop guards
//     (Auto-Submitted, Precedence: bulk|list|junk, List-* headers).
//   - F5713: an account that forwards without keeping a copy was refused
//     with "quota exceeded" for mail it would never store.

import (
	"testing"
	"time"
)

const round89Vacation = `{"enabled":true,"message":"away"}`

func round89WaitIdle(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(s.bgSem) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("background tasks did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func round89SetVacation(t *testing.T, s *Server, user string) {
	t.Helper()
	a, err := s.database.GetAccount("test.com", user)
	if err != nil {
		t.Fatal(err)
	}
	a.VacationSettings = round89Vacation
	if err := s.database.UpdateAccount(a); err != nil {
		t.Fatal(err)
	}
}

// round89Replies returns the queued vacation replies (auto-replied messages).
func round89Replies(t *testing.T, s *Server) int {
	t.Helper()
	round89WaitIdle(t, s)
	entries, err := s.queue.GetPendingEntries()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.From == "bob@test.com" {
			n++
		}
	}
	return n
}

func TestRound89VacationReplyEligibility(t *testing.T) {
	const plain = deliveryAuditMsg
	withHdr := func(h string) string { return h + "\r\n" + plain }
	cases := []struct {
		name   string
		from   string
		folder string
		data   string
		want   int
	}{
		{"control inbox", "s@ext.com", "", plain, 1},
		{"junk folder", "s@ext.com", "Junk", plain, 0},
		{"null sender", "", "", plain, 0},
		{"auto-submitted", "s@ext.com", "", withHdr("Auto-Submitted: auto-generated"), 0},
		{"auto-submitted no is a person", "s@ext.com", "", withHdr("Auto-Submitted: no"), 1},
		{"precedence bulk", "s@ext.com", "", withHdr("Precedence: bulk"), 0},
		{"list-id", "s@ext.com", "", withHdr("List-Id: <l.ext.com>"), 0},
		{"list-unsubscribe", "s@ext.com", "", withHdr("List-Unsubscribe: <mailto:u@ext.com>"), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newDeliveryAuditServer(t)
			round89SetVacation(t, s, "bob")
			if err := s.deliverLocal("bob", "test.com", c.from, []byte(c.data), c.folder); err != nil {
				t.Fatalf("deliver: %v", err)
			}
			if got := round89Replies(t, s); got != c.want {
				t.Errorf("vacation replies queued = %d, want %d", got, c.want)
			}
		})
	}
}

func TestRound89ForwardWithoutCopyIgnoresQuota(t *testing.T) {
	s := newDeliveryAuditServer(t)
	a, err := s.database.GetAccount("test.com", "alice") // QuotaLimit 10: always full
	if err != nil {
		t.Fatal(err)
	}
	a.ForwardTo, a.ForwardKeepCopy = "far@ext.com", false
	if err := s.database.UpdateAccount(a); err != nil {
		t.Fatal(err)
	}
	if err := s.deliverLocal("alice", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("forward-only account refused mail it would not store: %v", err)
	}
	entries, _ := s.queue.GetPendingEntries()
	found := false
	for _, e := range entries {
		if len(e.To) == 1 && e.To[0] == "far@ext.com" {
			found = true
		}
	}
	if !found {
		t.Error("message was not forwarded")
	}
	// Control: with a kept copy the quota still applies.
	a.ForwardKeepCopy = true
	if err := s.database.UpdateAccount(a); err != nil {
		t.Fatal(err)
	}
	if err := s.deliverLocal("alice", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err == nil {
		t.Error("over-quota local copy was accepted")
	}
}
