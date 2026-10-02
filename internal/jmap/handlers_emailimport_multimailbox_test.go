package jmap

// Regression test for the multi-mailbox Email/import defect: handleEmailImport
// selected only the FIRST mailbox whose mailboxIds value was true (the loop
// broke after the first entry and the collected mboxIDs map was never used),
// so importing into {inbox, archive} silently placed the message in exactly
// one mailbox — chosen by nondeterministic map iteration order — and the
// created response under-reported mailboxIds. RFC 8621 §4.6: Email/import's
// mailboxIds is Id[Boolean]; the email is imported into EVERY mailbox whose
// value is true. The round-21 mailboxExists gate (phantom-mailbox fix) must
// cover every resolved target: one missing mailbox rejects the whole entry.

import (
	"testing"
)

func TestEmailImportImportsIntoEveryMailbox(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: multi\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true, "archive": true},
					"keywords":   map[string]interface{}{"$seen": true},
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailImport(user, call)

	// Storage: synthesis-immune enumeration — a copy must exist in BOTH
	// mailboxes regardless of map iteration order.
	importedInto := func(mbox string) *int {
		uids, _ := db.GetMessageUIDs(user, mbox)
		var n int
		for _, uid := range uids {
			if meta, _ := db.GetMessageMetadata(user, mbox, uid); meta != nil && meta.MessageID == blobID {
				n++
			}
		}
		return &n
	}
	if n := *importedInto("INBOX"); n != 1 {
		t.Fatalf("FAIL: copies of %s in INBOX = %d, want 1", blobID, n)
	}
	if n := *importedInto("Archive"); n != 1 {
		t.Fatalf("FAIL: copies of %s in Archive = %d, want 1 (RFC 8621 §4.6)", blobID, n)
	}

	// Response: created[i1] reports every mailbox the message was imported into.
	// Args carries Go types here (direct handler call, no JSON round-trip).
	createdMap, _ := resp.Args["created"].(map[string]Email)
	created := createdMap["i1"]
	if len(created.MailboxIDs) != 2 {
		t.Fatalf("FAIL: created[i1].mailboxIds = %v, want 2 entries", created.MailboxIDs)
	}
	if _, in := created.MailboxIDs["inbox"]; !in {
		t.Fatalf("FAIL: created[i1].mailboxIds missing inbox: %v", created.MailboxIDs)
	}
	if _, in := created.MailboxIDs["archive"]; !in {
		t.Fatalf("FAIL: created[i1].mailboxIds missing archive: %v", created.MailboxIDs)
	}
	if !created.Keywords["$seen"] {
		t.Fatalf("FAIL: created[i1].keywords missing $seen: %v", created.Keywords)
	}
}

func TestEmailImportMissingMailboxRejectsWholeEntry(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: gated\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	// One valid target and one missing: the whole entry must be rejected
	// (mailboxNotFound) and NOTHING created — the round-21 gate now covers
	// every target, not only whichever the old loop happened to pick.
	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails": map[string]interface{}{
				"g1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true, "does-not-exist": true},
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailImport(user, call)

	createdMap, _ := resp.Args["created"].(map[string]Email)
	if _, present := createdMap["g1"]; present {
		t.Fatalf("FAIL: entry created despite a missing target mailbox")
	}
	notCreated, _ := resp.Args["notCreated"].(map[string]interface{})["g1"].(map[string]interface{})
	if got, _ := notCreated["type"].(string); got != "mailboxNotFound" {
		t.Fatalf("FAIL: notCreated[g1].type = %q, want \"mailboxNotFound\"", got)
	}
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	for _, uid := range uids {
		if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
			t.Fatalf("FAIL: message stored in INBOX despite rejection")
		}
	}
}

func TestEmailImportSingleMailboxStillWorks(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte("Subject: control\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	// Control: a single-mailbox import behaves exactly as before the fix.
	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails": map[string]interface{}{
				"c1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "call-1",
	}
	resp := server.handleEmailImport(user, call)

	createdMap, _ := resp.Args["created"].(map[string]Email)
	if _, present := createdMap["c1"]; !present {
		t.Fatalf("CONTROL FAILED: c1 missing from created (harness broken)")
	}
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	var found bool
	for _, uid := range uids {
		if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
			found = true
		}
	}
	if !found {
		t.Fatalf("CONTROL FAILED: control import not found in INBOX")
	}
}
