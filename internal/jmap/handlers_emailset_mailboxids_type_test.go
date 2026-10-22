package jmap

// RFC 8621 §4.1.2 types mailboxIds as Id[Boolean]. The round-35 import gate
// rejects wrong-typed values with an invalidProperties SetError listing the
// property (§4.8: wrong type → invalidProperties MUST) after adjudicating
// "non-boolean mailboxIds values were silently skipped" a defect. Email/set
// create shares the property, and its inline loop must follow the same
// contract: a non-boolean value (a client typo like "yes" instead of true)
// must reject the create — silently ignoring it would deliver to fewer
// mailboxes than requested while the response reports the Email as created.
// Absent mailboxIds keeps its own invalidProperties rejection, and legal
// false entries (explicitly not a target) stay legal.

import (
	"testing"
)

func TestEmailSetCreateRejectsNonBoolMailboxIds(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{
				"d1": map[string]interface{}{
					// "archive" carries a string where Id[Boolean] wants true.
					"mailboxIds": map[string]interface{}{"inbox": true, "archive": "yes"},
					"subject":    "r22 type gate",
					"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
					"bodyValues": map[string]interface{}{
						"p1": map[string]interface{}{"value": "hello"},
					},
				},
			},
		},
		ID: "c1",
	}
	resp := server.handleEmailSet(user, call)

	created, _ := resp.Args["created"].(map[string]Email)
	if _, bad := created["d1"]; bad {
		t.Fatalf("FAIL: create with a non-boolean mailboxIds value reported as created (RFC 8621 §4.1.2: mailboxIds is Id[Boolean]; the wrong-typed entry was silently skipped)")
	}
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	entry, ok := nc["d1"].(map[string]interface{})
	if !ok {
		t.Fatalf("FAIL: notCreated[d1] missing: created=%v notCreated=%v", created, nc)
	}
	if entry["type"] != "invalidProperties" {
		t.Fatalf("FAIL: notCreated[d1] SetError type = %v, want invalidProperties (RFC 8621 §4.8: wrong type → invalidProperties)", entry["type"])
	}
	props, _ := entry["properties"].([]string)
	listed := false
	for _, p := range props {
		if p == "mailboxIds" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("FAIL: invalidProperties does not list mailboxIds: %v", entry)
	}
}

// Control: an all-boolean mailboxIds create keeps working before and after.
func TestEmailSetCreateAcceptsBoolMailboxIds(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateMailbox(user, "Archive"); err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{
				"d1": map[string]interface{}{
					// false is a legal Id[Boolean] value: Archive is simply
					// not a target.
					"mailboxIds": map[string]interface{}{"inbox": true, "archive": false},
					"subject":    "r22 control",
					"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
					"bodyValues": map[string]interface{}{
						"p1": map[string]interface{}{"value": "hello"},
					},
				},
			},
		},
		ID: "c2",
	}
	resp := server.handleEmailSet(user, call)
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["d1"]
	if !ok {
		t.Fatalf("FAIL: legal all-boolean create rejected: created=%v notCreated=%v", created, resp.Args["notCreated"])
	}
	if len(e.MailboxIDs) != 1 {
		t.Fatalf("FAIL: legal create delivered to %d mailboxes, want 1 (only the true entry): %v", len(e.MailboxIDs), e.MailboxIDs)
	}
}
