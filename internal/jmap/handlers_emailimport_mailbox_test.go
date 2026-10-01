package jmap

// Regression tests for the phantom-mailbox defect: Email/import and the
// Email/set mailboxIds move derived the target mailbox name from a raw
// client-supplied ID (getMailboxNameFromID's default branch returns it
// verbatim) and the storage layer's CreateBucketIfNotExists then materialized
// a mailbox that was never created. RFC 8620 §2.3 requires mailboxIds to
// reference existing Mailbox objects; RFC 8621 §4.4 requires such imports and
// moves to fail with notCreated/notUpdated type "mailboxNotFound".

import (
	"testing"
)

func importCall(user, blobID, mailboxID string) MethodCall {
	return MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails": map[string]interface{}{
				"import-1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{mailboxID: true},
				},
			},
		},
		ID: "call-1",
	}
}

// Email/import into a mailbox that does not exist must not create it.
func TestEmailImport_RejectsNonexistentMailbox(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: import me\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	server.handleEmailImport(user, importCall(user, blobID, "definitely-not-created"))

	// RFC 8621 §4.4: the mailbox must exist; otherwise the import fails with
	// "mailboxNotFound" and nothing is stored. (GetMessageMetadata never
	// errors — it synthesizes empty metadata — so existence is probed via
	// the mailbox list and the message-UID enumeration.)
	if uids, _ := db.GetMessageUIDs(user, "definitely-not-created"); len(uids) != 0 {
		t.Fatalf("FAIL: import materialized a message inside the phantom mailbox \"definitely-not-created\"")
	}
	mailboxes, err := db.ListMailboxes(user)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mailboxes {
		if m == "definitely-not-created" {
			t.Fatalf("FAIL: import created the phantom mailbox \"definitely-not-created\"")
		}
	}
}

// Control: importing into an existing mailbox must keep working.
func TestEmailImport_ExistingMailboxStillWorks(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: control\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	resp := server.handleEmailImport(user, importCall(user, blobID, "archive"))
	created, _ := resp.Args["created"].(map[string]Email)
	if len(created) != 1 {
		t.Fatalf("CONTROL FAILED (harness): import into existing Archive = created %v notCreated %v", resp.Args["created"], resp.Args["notCreated"])
	}
}

// The Email/set mailboxIds move shares the defect: moving into a mailbox that
// does not exist must not silently create it.
func TestEmailSet_Move_RejectsNonexistentMailbox(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, mkMsg(1, "msg-1", "Move me", 100)); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, moveCall(user, "msg-1", "definitely-not-created", nil))

	// GetMessageMetadata never errors (it synthesizes empty metadata), so
	// both sides are probed via message-UID enumeration: the source message
	// must still live in INBOX, and the phantom mailbox must hold nothing.
	inboxUIDs, err := db.GetMessageUIDs(user, "INBOX")
	if err != nil || len(inboxUIDs) != 1 || inboxUIDs[0] != 1 {
		t.Fatalf("FAIL: source message moved although the destination did not exist (INBOX uids = %v err = %v)", inboxUIDs, err)
	}
	if uids, _ := db.GetMessageUIDs(user, "definitely-not-created"); len(uids) != 0 {
		t.Fatalf("FAIL: move materialized a message inside the phantom mailbox \"definitely-not-created\"")
	}
}
