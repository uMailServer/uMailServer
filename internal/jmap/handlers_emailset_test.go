package jmap

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// JMAP Email/set regression: a mailboxIds move must not corrupt unrelated
// messages. When an Email is moved to a destination mailbox that already holds
// other messages, the move must (a) place the moved Email under a freshly
// allocated UID, (b) leave every pre-existing message in the destination
// untouched, and (c) remove the moved Email from its source mailbox.
// RFC 8620 §4.1.2 (Email/set): a mailboxIds change moves the Email; it must not
// alter or remove any other Email.

func mkMsg(uid uint32, id, subject string, size int64) *storage.MessageMetadata {
	return &storage.MessageMetadata{
		UID:       uid,
		MessageID: id,
		ThreadID:  "thread-" + id,
		Subject:   subject,
		From:      "sender@example.com",
		To:        "user@example.com",
		Size:      size,
		Flags:     []string{},
	}
}

func moveCall(user, emailID, destID string, keywords map[string]interface{}) MethodCall {
	update := map[string]interface{}{}
	mb := map[string]interface{}{"mailboxIds": map[string]interface{}{destID: true}}
	if keywords != nil {
		mb["keywords"] = keywords
	}
	update[emailID] = mb
	return MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{"accountId": user, "update": update},
		ID:   "call-1",
	}
}

// The destination mailbox already holds an unrelated message at uid 1. Moving a
// message from INBOX into Archive must not overwrite that unrelated message.
func TestEmailSet_Move_DoesNotClobberUnrelatedMessage(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}

	// Mover lives in INBOX at uid 1.
	if err := db.StoreMessageMetadata(user, "INBOX", 1, mkMsg(1, "msg-mover", "Move me", 100)); err != nil {
		t.Fatal(err)
	}
	// Unrelated message already in Archive, allocated via GetNextUID so Archive's
	// uidnext advances — the move therefore gets a fresh uid and any clobbering
	// must come from the save step, not from uid allocation.
	victimUID, err := db.GetNextUID(user, "Archive")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "Archive", victimUID, mkMsg(victimUID, "msg-victim", "Do not touch", 200)); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, moveCall(user, "msg-mover", "archive", nil))

	// Unrelated Archive message must survive, byte-for-byte.
	victim, err := db.GetMessageMetadata(user, "Archive", victimUID)
	if err != nil {
		t.Fatalf("unrelated Archive message disappeared: %v", err)
	}
	if victim.MessageID != "msg-victim" || victim.Subject != "Do not touch" || victim.Size != 200 {
		t.Errorf("unrelated Archive message was clobbered: MessageID=%q Subject=%q Size=%d",
			victim.MessageID, victim.Subject, victim.Size)
	}
}

// The moved message must land in the destination mailbox under a fresh UID and
// must no longer be in the source mailbox.
func TestEmailSet_Move_PlacesInDestinationAndClearsSource(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Sent"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, mkMsg(1, "msg-1", "Hello", 100)); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, moveCall(user, "msg-1", "sent", nil))

	// Source must be empty.
	if got, _ := db.GetMessageUIDs(user, "INBOX"); len(got) != 0 {
		t.Errorf("source INBOX still has %d message(s) after move", len(got))
	}
	// Destination must contain the message exactly once.
	uids, _ := db.GetMessageUIDs(user, "Sent")
	found := 0
	for _, uid := range uids {
		if m, _ := db.GetMessageMetadata(user, "Sent", uid); m != nil && m.MessageID == "msg-1" {
			found++
			if m.UID != uid {
				t.Errorf("moved message stored UID field = %d, want bucket uid %d", m.UID, uid)
			}
		}
	}
	if found != 1 {
		t.Errorf("destination Sent contains moved message %d time(s), want exactly 1", found)
	}
}

// A move combined with a keyword update must apply the keywords to the moved
// message (the keywords are the payload the save step is meant to persist).
func TestEmailSet_MoveWithKeywords_AppliesKeywordsToMovedMessage(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, mkMsg(1, "msg-1", "Flagged move", 100)); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, moveCall(user, "msg-1", "archive",
		map[string]interface{}{"$flagged": true}))

	uids, _ := db.GetMessageUIDs(user, "Archive")
	if len(uids) != 1 {
		t.Fatalf("Archive has %d messages, want 1", len(uids))
	}
	m, err := db.GetMessageMetadata(user, "Archive", uids[0])
	if err != nil || m == nil {
		t.Fatalf("moved message missing from Archive: %v", err)
	}
	flagged := false
	for _, f := range m.Flags {
		if f == "\\Flagged" {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("moved message flags = %v, want to contain \\Flagged (keyword update must survive the move)", m.Flags)
	}
}

// Control: a keywords-only update (no move) is unaffected by this fix and must
// keep working.
func TestEmailSet_KeywordsOnly_Control(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, mkMsg(1, "msg-1", "Just flags", 100)); err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update": map[string]interface{}{
				"msg-1": map[string]interface{}{"keywords": map[string]interface{}{"$seen": true}},
			},
		},
		ID: "call-1",
	}
	server.handleEmailSet(user, call)

	m, err := db.GetMessageMetadata(user, "INBOX", 1)
	if err != nil || m == nil {
		t.Fatalf("control: message vanished: %v", err)
	}
	seen := false
	for _, f := range m.Flags {
		if f == "\\Seen" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("control: flags = %v, want \\Seen", m.Flags)
	}
}
