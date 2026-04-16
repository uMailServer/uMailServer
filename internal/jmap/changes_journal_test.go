package jmap

import (
	"strconv"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestMailboxChanges_ReadsFromJournal(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"

	if err := db.CreateMailbox(user, "Foo"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}
	if err := db.CreateMailbox(user, "Bar"); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	resp := server.handleMailboxChanges(user, MethodCall{
		Name: "Mailbox/changes",
		Args: map[string]interface{}{"accountId": user, "sinceState": "0"},
		ID:   "1",
	})

	created, _ := resp.Args["created"].([]string)
	if len(created) != 2 {
		t.Fatalf("want 2 created mailboxes, got %d (%v)", len(created), created)
	}
	if _, ok := resp.Args["newState"].(string); !ok {
		t.Errorf("newState should be string, got %T", resp.Args["newState"])
	}
}

func TestMailboxChanges_DeltaSinceState(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"
	_ = db.CreateMailbox(user, "Foo")

	resp1 := server.handleMailboxChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": "0"},
	})
	state1 := resp1.Args["newState"].(string)

	_ = db.CreateMailbox(user, "Bar")
	_ = db.DeleteMailbox(user, "Foo")

	resp2 := server.handleMailboxChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": state1},
	})
	created, _ := resp2.Args["created"].([]string)
	destroyed, _ := resp2.Args["destroyed"].([]string)
	if len(created) != 1 || created[0] != "Bar" {
		t.Errorf("want created=[Bar], got %v", created)
	}
	if len(destroyed) != 1 || destroyed[0] != "Foo" {
		t.Errorf("want destroyed=[Foo], got %v", destroyed)
	}
}

func TestEmailChanges_FromMessageMutations(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"
	_ = db.CreateMailbox(user, "INBOX")

	for i := 0; i < 3; i++ {
		uid, _ := db.GetNextUID(user, "INBOX")
		meta := &storage.MessageMetadata{
			MessageID:    "<msg-" + strconv.Itoa(i) + "@x>",
			UID:          uid,
			Flags:        []string{"\\Recent"},
			InternalDate: time.Now(),
		}
		if err := db.StoreMessageMetadata(user, "INBOX", uid, meta); err != nil {
			t.Fatalf("StoreMessageMetadata: %v", err)
		}
	}

	resp := server.handleEmailChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": "0"},
	})
	created, _ := resp.Args["created"].([]string)
	if len(created) != 3 {
		t.Fatalf("want 3 created emails, got %d (%v)", len(created), created)
	}
}

func TestEmailChanges_DestroyOnDelete(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	uid, _ := db.GetNextUID(user, "INBOX")
	meta := &storage.MessageMetadata{
		MessageID: "<m@x>",
		UID:       uid,
	}
	_ = db.StoreMessageMetadata(user, "INBOX", uid, meta)

	resp1 := server.handleEmailChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": "0"},
	})
	state := resp1.Args["newState"].(string)

	if err := db.DeleteMessage(user, "INBOX", uid); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	resp2 := server.handleEmailChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": state},
	})
	destroyed, _ := resp2.Args["destroyed"].([]string)
	if len(destroyed) != 1 || destroyed[0] != "<m@x>" {
		t.Errorf("want destroyed=[<m@x>], got %v", destroyed)
	}
}

func TestEmailChanges_HasMoreFlag(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	user := "user@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	for i := 0; i < 10; i++ {
		uid, _ := db.GetNextUID(user, "INBOX")
		_ = db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{
			MessageID: "<m" + strconv.Itoa(i) + "@x>", UID: uid,
		})
	}

	resp := server.handleEmailChanges(user, MethodCall{
		Args: map[string]interface{}{"accountId": user, "sinceState": "0", "maxChanges": float64(4)},
	})
	if !resp.Args["hasMoreChanges"].(bool) {
		t.Errorf("hasMoreChanges should be true with 10 entries and maxChanges=4")
	}
	created, _ := resp.Args["created"].([]string)
	if len(created) != 4 {
		t.Errorf("want 4 created in this page, got %d", len(created))
	}
}

func TestChanges_AccountIdMismatchRejected(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp := server.handleEmailChanges("user@example.com", MethodCall{
		Args: map[string]interface{}{"accountId": "other@example.com", "sinceState": "0"},
	})
	if resp.Args["type"] != "accountNotFound" {
		t.Errorf("expected accountNotFound error, got %+v", resp.Args)
	}
}
