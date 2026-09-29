package imap

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// uidCmdSession builds a Session backed by a real BboltMailstore so these
// tests exercise the production UID -> mailstore path, not a mock.
func uidCmdSession(t *testing.T) (*Session, *BboltMailstore, string, net.Conn) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	user := "user@example.com"
	if err := ms.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	client, srv := net.Pipe()
	s := NewSession(srv, NewServer(&Config{}, ms))
	s.state = StateSelected
	s.user = user
	s.selected = &Mailbox{Name: "INBOX"}
	s.tag = "t1"
	go io.Copy(io.Discard, client) // drain responses so handlers never block
	return s, ms, user, client
}

func appendMsgs(t *testing.T, ms *BboltMailstore, user string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		data := []byte(fmt.Sprintf("From: s%d@example.com\r\nSubject: M%d\r\n\r\nbody\r\n", i, i))
		if err := ms.AppendMessage(user, "INBOX", nil, time.Now(), data); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}
}

// mailboxMsgs returns the stored metadata for a given mailbox, in sequence
// order.
func mailboxMsgs(t *testing.T, ms *BboltMailstore, user, mailbox string) []storage.MessageMetadata {
	t.Helper()
	uids, err := ms.db.GetMessageUIDs(user, mailbox)
	if err != nil {
		t.Fatalf("GetMessageUIDs(%s): %v", mailbox, err)
	}
	var out []storage.MessageMetadata
	for _, u := range uids {
		m, err := ms.db.GetMessageMetadata(user, mailbox, u)
		if err != nil {
			t.Fatalf("GetMessageMetadata(%s): %v", mailbox, err)
		}
		out = append(out, *m)
	}
	return out
}

func hasSystemFlag(flags []string, want string) bool {
	for _, f := range flags {
		// parseFlags strips backslashes, so \Flagged is stored as "Flagged".
		if f == want {
			return true
		}
	}
	return false
}

// expungeMiddle removes the message at sequence 2 so that UIDs and sequence
// numbers diverge: with 4 appended (UIDs 1,2,3,4 at seq 1,2,3,4) then expunging
// the second, the survivors are UIDs 1,3,4 at seq 1,2,3.
func expungeMiddle(t *testing.T, ms *BboltMailstore, user string) {
	t.Helper()
	if err := ms.StoreFlags(user, "INBOX", "2", []string{`\Deleted`}, FlagAdd); err != nil {
		t.Fatalf("StoreFlags delete: %v", err)
	}
	if err := ms.Expunge(user, "INBOX"); err != nil {
		t.Fatalf("Expunge: %v", err)
	}
}

// RFC 3501 §6.4.8: a UID STORE sequence-set is interpreted as UIDs, not as
// sequence numbers. After an expunge the two differ, so treating a UID as a
// sequence number flags the wrong message.
func TestUIDStore_TargetsUIDNotSequenceNumber(t *testing.T) {
	s, ms, user, conn := uidCmdSession(t)
	defer conn.Close()
	defer ms.Close()

	appendMsgs(t, ms, user, 4)
	expungeMiddle(t, ms, user)

	// UIDs 1,3,4 now sit at seq 1,2,3; UID 3 is at sequence 2.
	if err := s.handleUIDStore([]string{"3", "+FLAGS", "(\\Flagged)"}); err != nil {
		t.Fatalf("handleUIDStore: %v", err)
	}
	for _, m := range mailboxMsgs(t, ms, user, "INBOX") {
		switch m.UID {
		case 3:
			if !hasSystemFlag(m.Flags, "Flagged") {
				t.Errorf("UID STORE 3 did not flag UID 3 (flags=%v); it was treated "+
					"as sequence number 2", m.Flags)
			}
		default:
			if hasSystemFlag(m.Flags, "Flagged") {
				t.Errorf("UID STORE 3 wrongly flagged UID %d (flags=%v); only UID 3 "+
					"should be flagged", m.UID, m.Flags)
			}
		}
	}
}

// A UID range and "*" must also resolve against UIDs, not positions.
func TestUIDStore_RangeAndStarResolveAgainstUIDs(t *testing.T) {
	s, ms, user, conn := uidCmdSession(t)
	defer conn.Close()
	defer ms.Close()

	appendMsgs(t, ms, user, 5)
	expungeMiddle(t, ms, user)
	// Survivors: UIDs 1,3,4,5 at seq 1,2,3,4.

	// UID range 3:4 -> UIDs 3 and 4 (seq 2 and 3).
	if err := s.handleUIDStore([]string{"3:4", "+FLAGS", "(\\Flagged)"}); err != nil {
		t.Fatalf("handleUIDStore 3:4: %v", err)
	}
	for _, m := range mailboxMsgs(t, ms, user, "INBOX") {
		want := m.UID == 3 || m.UID == 4
		got := hasSystemFlag(m.Flags, "Flagged")
		if want != got {
			t.Errorf("UID STORE 3:4: UID %d flagged=%v, want %v", m.UID, got, want)
		}
	}

	// "*" denotes the highest UID (5), not the highest position.
	if err := s.handleUIDStore([]string{"*", "+FLAGS", "(\\Seen)"}); err != nil {
		t.Fatalf("handleUIDStore *: %v", err)
	}
	last := mailboxMsgs(t, ms, user, "INBOX")
	if len(last) == 0 {
		t.Fatal("no messages")
	}
	highest := last[len(last)-1]
	if highest.UID != 5 || !hasSystemFlag(highest.Flags, "Seen") {
		t.Errorf("UID STORE * did not flag the highest UID (uid=%d flags=%v), want UID 5",
			highest.UID, highest.Flags)
	}
	for _, m := range last {
		if m.UID != 5 && hasSystemFlag(m.Flags, "Seen") {
			t.Errorf("UID STORE * wrongly flagged UID %d", m.UID)
		}
	}
}

// UID COPY must copy the message with the given UID, not the one at that
// position, after an expunge has shifted the positions.
func TestUIDCopy_TargetsUIDNotSequenceNumber(t *testing.T) {
	s, ms, user, conn := uidCmdSession(t)
	defer conn.Close()
	defer ms.Close()

	appendMsgs(t, ms, user, 4)
	expungeMiddle(t, ms, user)
	if err := ms.CreateMailbox(user, "Archive"); err != nil {
		t.Fatalf("CreateMailbox Archive: %v", err)
	}
	// Give UID 3 (at seq 2) a distinctive subject.
	src := mailboxMsgs(t, ms, user, "INBOX")
	for i := range src {
		if src[i].UID == 3 {
			src[i].Subject = "TARGET-UID-3"
			if err := ms.db.UpdateMessageMetadata(user, "INBOX", src[i].UID, &src[i]); err != nil {
				t.Fatalf("UpdateMessageMetadata: %v", err)
			}
		}
	}

	if err := s.handleUIDCopy([]string{"3", "Archive"}); err != nil {
		t.Fatalf("handleUIDCopy: %v", err)
	}
	dst := mailboxMsgs(t, ms, user, "Archive")
	if len(dst) != 1 {
		t.Fatalf("expected 1 copied message, got %d", len(dst))
	}
	if dst[0].Subject != "TARGET-UID-3" {
		t.Errorf("UID COPY 3 copied subject %q, want TARGET-UID-3 (wrong message copied)",
			dst[0].Subject)
	}
}

// CONTROL: the plain, non-UID commands must keep using sequence numbers, so
// the UID fix must not change their behaviour.
func TestPlainStore_StillUsesSequenceNumbers(t *testing.T) {
	s, ms, user, conn := uidCmdSession(t)
	defer conn.Close()
	defer ms.Close()

	appendMsgs(t, ms, user, 4)
	expungeMiddle(t, ms, user)

	// Sequence 2 is UID 3 after the expunge.
	if err := s.handleStore([]string{"2", "+FLAGS", "(\\Flagged)"}); err != nil {
		t.Fatalf("handleStore: %v", err)
	}
	msgs := mailboxMsgs(t, ms, user, "INBOX")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1].UID != 3 || !hasSystemFlag(msgs[1].Flags, "Flagged") {
		t.Errorf("plain STORE 2 should flag sequence 2 (uid=%d flags=%v), got uid=%d flags=%v",
			msgs[1].UID, msgs[1].Flags, msgs[1].UID, msgs[1].Flags)
	}
}
