package jmap

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func f6645Query(t *testing.T, srv *Server, user string) []string {
	t.Helper()
	r := srv.handleEmailQuery(user, MethodCall{Name: "Email/query", ID: "q", Args: map[string]interface{}{}})
	ids, _ := r.Args["ids"].([]string)
	return ids
}

func f6645Destroy(srv *Server, user, id string) Response {
	return srv.handleEmailSet(user, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
		"destroy": []interface{}{id},
	}})
}

// TestF6645DestroyOneOfIdenticalCopies: two byte-identical messages in one
// mailbox share a blob hash. They must be two Emails and destroying one must
// leave the other (and its blob) intact.
func TestF6645DestroyOneOfIdenticalCopies(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blob, err := store.StoreMessage(user, []byte("From: b@example.com\r\nSubject: dup\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	for uid := uint32(1); uid <= 2; uid++ {
		if err := db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{UID: uid, MessageID: blob, Size: 10}); err != nil {
			t.Fatal(err)
		}
	}
	ids := f6645Query(t, srv, user)
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("EXPECTED 2 distinct Email ids, ACTUAL %v", ids)
	}
	if ids[0] != blob {
		t.Errorf("primary Email id changed: %s want plain blob hash %s", ids[0], blob)
	}
	for _, id := range ids {
		g := srv.handleEmailGet(user, MethodCall{Name: "Email/get", ID: "g", Args: map[string]interface{}{"ids": []interface{}{id}}})
		list, _ := g.Args["list"].([]Email)
		if len(list) != 1 || list[0].ID != id || list[0].BlobID != blob {
			t.Fatalf("Email/get(%s) = %+v", id, g.Args)
		}
	}
	// Destroy the extra copy: the primary survives.
	if d, _ := f6645Destroy(srv, user, ids[1]).Args["destroyed"].([]string); len(d) != 1 {
		t.Fatalf("destroy extra failed")
	}
	if got := f6645Query(t, srv, user); len(got) != 1 || got[0] != ids[0] {
		t.Fatalf("after destroying extra: %v want [%s]", got, ids[0])
	}
	if !store.MessageExists(user, blob) {
		t.Fatal("blob deleted while a copy still references it")
	}
	// Destroying the last copy removes the blob.
	f6645Destroy(srv, user, ids[0])
	if len(f6645Query(t, srv, user)) != 0 || store.MessageExists(user, blob) {
		t.Fatal("last copy destroy left Email or blob behind")
	}
}

// Destroying the primary leaves the duplicate listed and readable.
func TestF6645DestroyPrimaryKeepsDuplicate(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	blob, _ := store.StoreMessage(user, []byte("Subject: dup2\r\n\r\nbody\r\n"))
	for uid := uint32(1); uid <= 2; uid++ {
		_ = db.StoreMessageMetadata(user, "INBOX", uid, &storage.MessageMetadata{UID: uid, MessageID: blob, Size: 5})
	}
	ids := f6645Query(t, srv, user)
	if len(ids) != 2 {
		t.Fatalf("ids %v", ids)
	}
	f6645Destroy(srv, user, ids[0])
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	if len(uids) != 1 || uids[0] != 2 {
		t.Fatalf("remaining uids %v want [2]", uids)
	}
	if !store.MessageExists(user, blob) {
		t.Fatal("blob deleted although a copy remains")
	}
	if got := f6645Query(t, srv, user); len(got) != 1 {
		t.Fatalf("query after destroy %v", got)
	}
}

// Control: one copy per mailbox stays ONE Email in both mailboxes (F5220).
func TestF6645CrossMailboxStaysOneEmail(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	_ = db.CreateMailbox(user, "INBOX")
	_ = db.CreateMailbox(user, "Archive")
	blob, _ := store.StoreMessage(user, []byte("Subject: x\r\n\r\nbody\r\n"))
	for _, m := range []string{"INBOX", "Archive"} {
		_ = db.StoreMessageMetadata(user, m, 1, &storage.MessageMetadata{UID: 1, MessageID: blob, Size: 5})
	}
	if ids := f6645Query(t, srv, user); len(ids) != 1 || ids[0] != blob {
		t.Fatalf("ids %v want [%s]", ids, blob)
	}
}

func TestF6645ExtraIDRoundTripAndRejects(t *testing.T) {
	id := extraCopyID("abc", "Nested/Folder", 7)
	h, m, u, ok := parseExtraCopyID(id)
	if !ok || h != "abc" || m != "Nested/Folder" || u != 7 {
		t.Fatalf("round trip: %v %v %v %v", h, m, u, ok)
	}
	for _, bad := range []string{"abc", "abc_x_QQ", "abc_1_", "_1_QQ", "abc_1_!!"} {
		if _, _, _, ok := parseExtraCopyID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
