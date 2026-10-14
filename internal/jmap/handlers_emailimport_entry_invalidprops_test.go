package jmap

// Regression test for the illegal SetError type on non-object Email/import
// entries: the per-entry loop typed a notCreated entry with
// "invalidArguments" — a *method-level* error name (RFC 8620 §3.6.1) that is
// not part of the §2.6 SetError type set — where §4.3 defines the create
// argument as Id[EmailImport] (RFC 8621 §4.8) and §2.6 assigns
// "invalidProperties" to "(create; update): The record given is invalid in
// some way". Round 35 already converted the property-level gate INSIDE a
// well-formed record to invalidProperties; this test pins the record-level
// site.

import (
	"testing"
)

func TestEmailImportNonObjectEntryUsesInvalidProperties(t *testing.T) {
	const user = "user@example.com"

	t.Run("non-object entry value", func(t *testing.T) {
		server, _, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		if _, err := msgStore.StoreMessage(user, []byte("Subject: typed\r\n\r\nbody\r\n")); err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails": map[string]interface{}{
					"k1": "not-an-object",
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		if _, ok := created["k1"]; ok {
			t.Fatalf("FAIL: non-object entry was created; want rejection")
		}
		notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
		errObj, ok := notCreated["k1"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither created nor reported in notCreated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notCreated SetError type = %q, want \"invalidProperties\" (RFC 8620 §2.6: the record given is invalid per the type specification; \"invalidArguments\" is a method-level error name, §3.6.1)", got)
		}
	})

	t.Run("valid import control", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		blobID, err := msgStore.StoreMessage(user, []byte("Subject: control\r\n\r\nbody\r\n"))
		if err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails": map[string]interface{}{
					"k2": map[string]interface{}{
						"blobId":     blobID,
						"mailboxIds": map[string]interface{}{"inbox": true},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		if _, ok := created["k2"]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: valid import rejected: %v", resp.Args)
		}
		// Synthesis-immune storage probe (round-21 lesson).
		uids, _ := db.GetMessageUIDs(user, "INBOX")
		found := false
		for _, uid := range uids {
			if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
				found = true
			}
		}
		if !found {
			t.Fatalf("FAIL: CONTROL FAILED: imported message not stored in INBOX")
		}
	})
}
