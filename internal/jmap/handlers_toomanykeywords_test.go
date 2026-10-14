package jmap

// RFC 8621 §4.6: "tooManyKeywords": The change to the Email's keywords would
// exceed a server-defined maximum. Round 47 pins the server policy
// (maxJMAPKeywordsPerEmail) on both keyword write paths:
//   - Email/import: a per-entry import whose keywords exceed the cap is
//     rejected with a notCreated SetError of type "tooManyKeywords" and
//     stores nothing (§4.8's own list does not name the type; the extension
//     follows the recorded round-47 directive and §2.6's method-specific
//     SetError mechanism).
//   - Email/set: a keyword patch (path form or whole form) whose RESULT
//     would exceed the cap is rejected atomically with notUpdated
//     "tooManyKeywords" — no copy's flags change. Removals and non-keyword
//     patches always pass, so pre-existing over-cap emails stay manageable.

import (
	"fmt"
	"testing"
)

func kwsForTest(n int) map[string]interface{} {
	m := make(map[string]interface{}, n)
	for i := 1; i <= n; i++ {
		m[fmt.Sprintf("$k%d", i)] = true
	}
	return m
}

const tooManyUser = "user@example.com"

func TestEmailImportTooManyKeywords(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(tooManyUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// Over cap: rejected, nothing stored.
	overBlob, err := msgStore.StoreMessage(tooManyUser, []byte("Subject: over\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(tooManyUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"emails": map[string]interface{}{
				"over": map[string]interface{}{
					"blobId":     overBlob,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"keywords":   kwsForTest(maxJMAPKeywordsPerEmail + 1),
				},
			},
		},
		ID: "c1",
	})
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	e, _ := nc["over"].(map[string]interface{})
	if e == nil || e["type"] != "tooManyKeywords" {
		t.Fatalf("FAIL: over-cap import reported %v; want notCreated SetError type \"tooManyKeywords\"", resp.Args)
	}
	// Synthesis-immune storage probe: nothing may be stored for the rejected entry.
	uids, _ := db.GetMessageUIDs(tooManyUser, "INBOX")
	for _, uid := range uids {
		if meta, _ := db.GetMessageMetadata(tooManyUser, "INBOX", uid); meta != nil && meta.MessageID == overBlob {
			t.Fatalf("FAIL: rejected over-cap import stored a message in INBOX")
		}
	}

	// Boundary: exactly the cap is allowed.
	atBlob, err := msgStore.StoreMessage(tooManyUser, []byte("Subject: at\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp = server.handleEmailImport(tooManyUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"emails": map[string]interface{}{
				"at": map[string]interface{}{
					"blobId":     atBlob,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"keywords":   kwsForTest(maxJMAPKeywordsPerEmail),
				},
			},
		},
		ID: "c2",
	})
	created, _ := resp.Args["created"].(map[string]Email)
	at, ok := created["at"]
	if !ok {
		t.Fatalf("FAIL: import with exactly %d keywords must succeed: %v", maxJMAPKeywordsPerEmail, resp.Args)
	}
	if len(at.Keywords) != maxJMAPKeywordsPerEmail {
		t.Fatalf("FAIL: created keywords = %d, want %d", len(at.Keywords), maxJMAPKeywordsPerEmail)
	}
}

func TestEmailSetTooManyKeywords(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(tooManyUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// Seed an email at exactly the cap.
	blobID, err := msgStore.StoreMessage(tooManyUser, []byte("Subject: seed\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(tooManyUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
					"keywords":   kwsForTest(maxJMAPKeywordsPerEmail),
				},
			},
		},
		ID: "c0",
	})
	created, _ := resp.Args["created"].(map[string]Email)
	seed, ok := created["i1"]
	if !ok {
		t.Fatalf("INVALID setup: import at cap failed: %v", resp.Args)
	}
	emailID := seed.ID

	// Path form past the cap: rejected atomically, flags unchanged.
	resp = server.handleEmailSet(tooManyUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"update": map[string]interface{}{
				emailID: map[string]interface{}{"keywords/$overflow": true},
			},
		},
		ID: "c1",
	})
	nu, _ := resp.Args["notUpdated"].(map[string]interface{})
	e, _ := nu[emailID].(map[string]interface{})
	if e == nil || e["type"] != "tooManyKeywords" {
		t.Fatalf("FAIL: path-add past the cap reported %v; want notUpdated \"tooManyKeywords\"", resp.Args)
	}
	if n := len(storedFlags(t, db, tooManyUser, blobID)); n != maxJMAPKeywordsPerEmail {
		t.Fatalf("FAIL: rejected patch changed stored flags: %d, want %d", n, maxJMAPKeywordsPerEmail)
	}

	// Whole-form replace past the cap: rejected atomically.
	big := kwsForTest(maxJMAPKeywordsPerEmail + 1)
	resp = server.handleEmailSet(tooManyUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"update": map[string]interface{}{
				emailID: map[string]interface{}{"keywords": big},
			},
		},
		ID: "c2",
	})
	nu, _ = resp.Args["notUpdated"].(map[string]interface{})
	e, _ = nu[emailID].(map[string]interface{})
	if e == nil || e["type"] != "tooManyKeywords" {
		t.Fatalf("FAIL: whole-form replace past the cap reported %v; want notUpdated \"tooManyKeywords\"", resp.Args)
	}
	if n := len(storedFlags(t, db, tooManyUser, blobID)); n != maxJMAPKeywordsPerEmail {
		t.Fatalf("FAIL: rejected replace changed stored flags: %d, want %d", n, maxJMAPKeywordsPerEmail)
	}

	// Removal at the cap still works (boundary semantics: shrinking passes).
	resp = server.handleEmailSet(tooManyUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"update": map[string]interface{}{
				emailID: map[string]interface{}{"keywords/$k1": nil},
			},
		},
		ID: "c3",
	})
	updated, _ := resp.Args["updated"].(map[string]interface{})
	if _, ok := updated[emailID]; !ok {
		t.Fatalf("FAIL: removal at the cap must still apply: %v", resp.Args)
	}
	if n := len(storedFlags(t, db, tooManyUser, blobID)); n != maxJMAPKeywordsPerEmail-1 {
		t.Fatalf("FAIL: after removal flags = %d, want %d", n, maxJMAPKeywordsPerEmail-1)
	}

	// Control: one more add now fits again.
	resp = server.handleEmailSet(tooManyUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": tooManyUser,
			"update": map[string]interface{}{
				emailID: map[string]interface{}{"keywords/$k1": true},
			},
		},
		ID: "c4",
	})
	updated, _ = resp.Args["updated"].(map[string]interface{})
	if _, ok := updated[emailID]; !ok {
		t.Fatalf("CONTROL FAILED: re-add below the cap must apply: %v", resp.Args)
	}
}
