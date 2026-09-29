package imap

import (
	"net"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// newR23ACLSession builds a real BboltMailstore-backed Session so these tests
// drive production handleSetACL -> ParseACLRights -> SetACL, not a copy of the
// rights logic.
func newR23ACLSession(t *testing.T) (*Session, *BboltMailstore) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	client, srv := net.Pipe()
	server := NewServer(&Config{Addr: ":0"}, ms)
	sess := NewSession(srv, server)
	sess.state = StateAuthenticated
	sess.user = "owner"

	// Drain tagged responses so the handler's writes never block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { client.Close(); <-done; ms.Close() })
	return sess, ms
}

const (
	r23Owner    = "owner"
	r23Mailbox  = "mailbox"
	r23GranteeA = "bob"
)

// TestSetACL_NegativeRightsRemovesInsteadOfGranting pins RFC 4314 section 3.1:
// a leading '-' REMOVES the listed rights from the grantee's EXISTING set.
//
// The mailbox owner revoking bob's lookup+read must leave bob with only 's'.
// The defect implemented the negation as a bitwise complement (^rights), so
// "-lr" became 0b11111100 and handleSetACL stored it verbatim -- granting bob
// write-seen, delete, expunge and create instead of revoking anything.
func TestSetACL_NegativeRightsRemovesInsteadOfGranting(t *testing.T) {
	sess, ms := newR23ACLSession(t)

	const existing = storage.ACLLookup | storage.ACLRead | storage.ACLSeen // "lrs"
	if err := ms.SetACL(r23Owner, r23Mailbox, r23GranteeA, uint8(existing), r23Owner); err != nil {
		t.Fatalf("seed SetACL: %v", err)
	}

	if err := sess.handleSetACL([]string{r23Owner + ":" + r23Mailbox, r23GranteeA, "-lr"}); err != nil {
		t.Fatalf("handleSetACL: %v", err)
	}

	got, err := ms.GetACL(r23Owner, r23Mailbox, r23GranteeA)
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	// Revoking l+r from "lrs" must leave exactly 's'.
	const want = uint8(storage.ACLSeen)
	if got != want {
		t.Errorf("SETACL %s -lr: rights = %#08b (%s), want %#08b (%s); a leading '-' "+
			"must REMOVE rights from the existing set (RFC 4314 3.1), not complement "+
			"them into a grant of everything unlisted",
			r23GranteeA, got, storage.ACLRights(got), want, storage.ACLRights(want))
	}
}

// TestSetACL_NegativeOnGranteeWithNoExistingRights is the boundary the proof
// skipped: revoking from a grantee that holds nothing. RFC 4314 removal from
// an empty set is a no-op, and must not manufacture rights.
func TestSetACL_NegativeOnGranteeWithNoExistingRights(t *testing.T) {
	sess, ms := newR23ACLSession(t)

	// bob has no ACL entry at all.
	if err := sess.handleSetACL([]string{r23Owner + ":" + r23Mailbox, r23GranteeA, "-lr"}); err != nil {
		t.Fatalf("handleSetACL: %v", err)
	}

	got, err := ms.GetACL(r23Owner, r23Mailbox, r23GranteeA)
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	if got != 0 {
		t.Errorf("SETACL %s -lr with no existing rights: rights = %#08b (%s), want 0; "+
			"removal from an empty set must stay empty",
			r23GranteeA, got, storage.ACLRights(got))
	}
}

// TestSetACL_PositiveRightsGrantExactly is the control: the positive form must
// still grant exactly the listed rights. It passes with and without the fix, so
// a red result elsewhere cannot be blamed on the harness.
func TestSetACL_PositiveRightsGrantExactly(t *testing.T) {
	sess, ms := newR23ACLSession(t)

	if err := sess.handleSetACL([]string{r23Owner + ":" + r23Mailbox, r23GranteeA, "lrs"}); err != nil {
		t.Fatalf("handleSetACL: %v", err)
	}

	got, err := ms.GetACL(r23Owner, r23Mailbox, r23GranteeA)
	if err != nil {
		t.Fatalf("GetACL: %v", err)
	}
	const want = uint8(storage.ACLLookup | storage.ACLRead | storage.ACLSeen)
	if got != want {
		t.Errorf("control: SETACL %s lrs: rights = %#08b, want %#08b", r23GranteeA, got, want)
	}
}
