package jmap

// Regression tests for F5145 and F5146 (Mailbox/set).
//
// F5145: RFC 8621 §2.5 - destroying a Mailbox that still has emails must
// fail with mailboxHasEmail unless onDestroyRemoveEmails is true. The handler
// used to delete the mailbox together with all of its messages.
//
// F5146: Mailbox/get advertises INBOX with mayRename=false and
// mayDelete=false, so Mailbox/set must refuse to rename or destroy it.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const mailboxSetDestroyUser = "alice@example.com"

func newMailboxSetDestroyServer(t *testing.T) (*Server, *storage.Database) {
	t.Helper()
	srv, db, _, cleanup := setupTestServer(t)
	t.Cleanup(cleanup)
	for _, m := range []string{"INBOX", "Projects", "Empty"} {
		if err := db.CreateMailbox(mailboxSetDestroyUser, m); err != nil {
			t.Fatalf("create %s: %v", m, err)
		}
	}
	meta := &storage.MessageMetadata{MessageID: "m-proj-1", UID: 1, InternalDate: time.Now(), Subject: "keep me"}
	if err := db.StoreMessageMetadata(mailboxSetDestroyUser, "Projects", 1, meta); err != nil {
		t.Fatalf("store: %v", err)
	}
	return srv, db
}

func mailboxSetCall(srv *Server, args map[string]interface{}) Response {
	args["accountId"] = mailboxSetDestroyUser
	return srv.handleMailboxSet(mailboxSetDestroyUser, MethodCall{Name: "Mailbox/set", Args: args, ID: "c1"})
}

func setErrorType(resp Response, field, id string) interface{} {
	m, _ := resp.Args[field].(map[string]interface{})
	e, _ := m[id].(map[string]interface{})
	if e == nil {
		return nil
	}
	return e["type"]
}

func TestMailboxSetDestroyNonEmptyRequiresOnDestroyRemoveEmails(t *testing.T) {
	srv, db := newMailboxSetDestroyServer(t)

	resp := mailboxSetCall(srv, map[string]interface{}{"destroy": []interface{}{"Projects", "Empty"}})
	if got := setErrorType(resp, "notDestroyed", "Projects"); got != "mailboxHasEmail" {
		t.Fatalf("notDestroyed[Projects].type = %v, want mailboxHasEmail (args %v)", got, resp.Args)
	}
	if d, _ := resp.Args["destroyed"].([]string); len(d) != 1 || d[0] != "Empty" {
		t.Fatalf("destroyed = %v, want [Empty]", resp.Args["destroyed"])
	}
	if uids, _ := db.GetMessageUIDs(mailboxSetDestroyUser, "Projects"); len(uids) != 1 {
		t.Fatalf("Projects lost its message: %d uids remain", len(uids))
	}

	resp = mailboxSetCall(srv, map[string]interface{}{"destroy": []interface{}{"Projects"}, "onDestroyRemoveEmails": true})
	if d, _ := resp.Args["destroyed"].([]string); len(d) != 1 || d[0] != "Projects" {
		t.Fatalf("with onDestroyRemoveEmails destroyed = %v, want [Projects] (args %v)", resp.Args["destroyed"], resp.Args)
	}
}

func TestMailboxSetInboxCannotBeRenamedOrDestroyed(t *testing.T) {
	srv, _ := newMailboxSetDestroyServer(t)

	resp := mailboxSetCall(srv, map[string]interface{}{"update": map[string]interface{}{"inbox": map[string]interface{}{"name": "Old"}}})
	if got := setErrorType(resp, "notUpdated", "inbox"); got != "forbidden" {
		t.Fatalf("rename inbox: notUpdated type = %v, want forbidden (args %v)", got, resp.Args)
	}
	resp = mailboxSetCall(srv, map[string]interface{}{"destroy": []interface{}{"inbox"}, "onDestroyRemoveEmails": true})
	if got := setErrorType(resp, "notDestroyed", "inbox"); got != "forbidden" {
		t.Fatalf("destroy inbox: notDestroyed type = %v, want forbidden (args %v)", got, resp.Args)
	}
	if !srv.mailboxExists(mailboxSetDestroyUser, "INBOX") {
		t.Fatal("INBOX no longer exists")
	}

	// A non-rename update of the inbox and a rename of another mailbox
	// still succeed.
	resp = mailboxSetCall(srv, map[string]interface{}{"update": map[string]interface{}{
		"inbox": map[string]interface{}{"name": "INBOX"},
		"Empty": map[string]interface{}{"name": "Renamed"},
	}})
	up, _ := resp.Args["updated"].(map[string]interface{})
	if _, ok := up["inbox"]; !ok {
		t.Fatalf("no-op inbox update not reported updated: %v", resp.Args)
	}
	if _, ok := up["Empty"]; !ok || !srv.mailboxExists(mailboxSetDestroyUser, "Renamed") {
		t.Fatalf("custom rename failed: %v", resp.Args)
	}
}
