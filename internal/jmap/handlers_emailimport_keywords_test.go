package jmap

// Regression tests for Email/import keyword handling.
//
// Round 41 made the import reject keywords the server could not store
// (interim compliance: silent drops became invalidProperties). Round 45
// implemented RFC 8621 §2 arbitrary-keyword storage end-to-end, which
// deliberately supersedes the interim rejection for keyword NAMES: any
// syntactically valid §2 keyword (1-255 chars of %x21-%x7e, excluding
// ( ) { ] % * " \) is now stored as a lowercase IMAP keyword and returned
// lowercase (§2: "servers MUST return keywords in lowercase"). Still
// rejected with invalidProperties: non-true keyword values (§2: "The value
// for each key in the object MUST be true") and syntactically invalid
// names.

import (
	"testing"
)

func TestEmailImportKeywordHandling(t *testing.T) {
	const user = "user@example.com"

	t.Run("arbitrary keyword name is stored and returned lowercase", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		blobID, err := msgStore.StoreMessage(user, []byte("Subject: kw\r\n\r\nbody\r\n"))
		if err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails": map[string]interface{}{
					"k1": map[string]interface{}{
						"blobId":     blobID,
						"mailboxIds": map[string]interface{}{"inbox": true},
						"keywords":   map[string]interface{}{"$Label1": true},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		entry, ok := created["k1"]
		if !ok {
			t.Fatalf("FAIL: arbitrary keyword import rejected (RFC 8621 §2: users may add arbitrary keywords): %v", resp.Args)
		}
		// §2: servers MUST return keywords in LOWERCASE.
		if !entry.Keywords["$label1"] {
			t.Fatalf("FAIL: imported keyword not returned lowercase as \"$label1\" (keywords=%v)", entry.Keywords)
		}
		// Synthesis-immune storage probe (round-21 lesson).
		uids, _ := db.GetMessageUIDs(user, "INBOX")
		stored := false
		for _, uid := range uids {
			if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
				for _, f := range meta.Flags {
					if f == "$label1" {
						stored = true
					}
				}
			}
		}
		if !stored {
			t.Fatalf("FAIL: \"$label1\" accepted but not stored as a flag")
		}
	})

	t.Run("non-true keyword value is rejected", func(t *testing.T) {
		server, _, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		blobID, err := msgStore.StoreMessage(user, []byte("Subject: kw2\r\n\r\nbody\r\n"))
		if err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails": map[string]interface{}{
					"k1": map[string]interface{}{
						"blobId":     blobID,
						"mailboxIds": map[string]interface{}{"inbox": true},
						"keywords":   map[string]interface{}{"$seen": false},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		if _, ok := created["k1"]; ok {
			t.Fatalf("FAIL: entry accepted despite a non-true keyword value (RFC 8621 §2: the value for each key MUST be true)")
		}
		notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
		errObj, ok := notCreated["k1"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither created nor reported in notCreated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notCreated SetError type = %q, want \"invalidProperties\" (RFC 8621 §2 + §4.8)", got)
		}
	})

	t.Run("syntactically invalid keyword name is rejected", func(t *testing.T) {
		server, _, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		blobID, err := msgStore.StoreMessage(user, []byte("Subject: kw3\r\n\r\nbody\r\n"))
		if err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails": map[string]interface{}{
					"k1": map[string]interface{}{
						"blobId":     blobID,
						"mailboxIds": map[string]interface{}{"inbox": true},
						"keywords":   map[string]interface{}{"bad keyword": true},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		if _, ok := created["k1"]; ok {
			t.Fatalf("FAIL: syntax-invalid keyword accepted (RFC 8621 §2: space is not a keyword character)")
		}
		notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
		errObj, ok := notCreated["k1"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither created nor reported in notCreated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notCreated SetError type = %q, want \"invalidProperties\" (RFC 8621 §2 syntax + §4.8)", got)
		}
	})

	t.Run("system keyword control", func(t *testing.T) {
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
					"k1": map[string]interface{}{
						"blobId":     blobID,
						"mailboxIds": map[string]interface{}{"inbox": true},
						"keywords":   map[string]interface{}{"$seen": true},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		entry, ok := created["k1"]
		if !ok {
			t.Fatalf("FAIL: CONTROL FAILED: system-keyword import rejected: %v", resp.Args)
		}
		if !entry.Keywords["$seen"] {
			t.Fatalf("FAIL: CONTROL FAILED: created object lacks $seen: %v", entry.Keywords)
		}
		// Synthesis-immune storage probe (round-21 lesson).
		uids, _ := db.GetMessageUIDs(user, "INBOX")
		seenFlag := false
		for _, uid := range uids {
			if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
				for _, f := range meta.Flags {
					if f == "\\Seen" {
						seenFlag = true
					}
				}
			}
		}
		if !seenFlag {
			t.Fatalf("FAIL: CONTROL FAILED: \\Seen flag not stored")
		}
	})
}
