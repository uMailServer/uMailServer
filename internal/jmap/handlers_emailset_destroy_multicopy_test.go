package jmap

import "testing"

// TestEmailSetDestroyRemovesEveryMailboxCopy is the F5220 regression: an
// Email imported into several mailboxes is one object (RFC 8621 §4), so
// Email/set destroy must remove it from every mailbox. Previously only the
// first copy was deleted together with the shared blob, leaving a
// still-listed Email whose content was gone.
func TestEmailSetDestroyRemovesEveryMailboxCopy(t *testing.T) {
	srv, db, store, cleanup := setupTestServer(t)
	defer cleanup()
	const user = "alice@example.com"
	for _, m := range []string{"INBOX", "Archive"} {
		if err := db.CreateMailbox(user, m); err != nil {
			t.Fatalf("CreateMailbox(%s): %v", m, err)
		}
	}
	blob, err := store.StoreMessage(user, []byte("From: bob@example.com\r\nSubject: hi\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatalf("StoreMessage: %v", err)
	}
	imp := srv.handleEmailImport(user, MethodCall{Name: "Email/import", ID: "i", Args: map[string]interface{}{
		"emails": map[string]interface{}{"k": map[string]interface{}{
			"blobId":     blob,
			"mailboxIds": map[string]interface{}{"inbox": true, "archive": true},
		}},
	}})
	created, _ := imp.Args["created"].(map[string]Email)
	if len(created) != 1 {
		t.Fatalf("import failed: %v", imp.Args)
	}
	id := created["k"].ID

	set := srv.handleEmailSet(user, MethodCall{Name: "Email/set", ID: "s", Args: map[string]interface{}{
		"destroy": []interface{}{id},
	}})
	if destroyed, _ := set.Args["destroyed"].([]string); len(destroyed) != 1 || destroyed[0] != id {
		t.Fatalf("destroyed = %v, want [%s]", set.Args["destroyed"], id)
	}

	for _, m := range []string{"INBOX", "Archive"} {
		uids, err := db.GetMessageUIDs(user, m)
		if err != nil {
			t.Fatalf("GetMessageUIDs(%s): %v", m, err)
		}
		if len(uids) != 0 {
			t.Errorf("mailbox %s still holds %d copies of destroyed email", m, len(uids))
		}
	}
	get := srv.handleEmailGet(user, MethodCall{Name: "Email/get", ID: "g", Args: map[string]interface{}{
		"ids": []interface{}{id},
	}})
	if list, _ := get.Args["list"].([]Email); len(list) != 0 {
		t.Errorf("Email/get still lists destroyed email: %v", list)
	}
}
