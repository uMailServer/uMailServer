package jmap

// Regression test for the wrong-typed Email/import property defect:
// handleEmailImport type-coerced blobId/mailboxIds/keywords and silently
// degraded — a non-object mailboxIds fell back to INBOX, non-boolean
// mailboxIds/keywords values were skipped, and a non-string blobId produced
// an "invalidArguments" SetError — while reporting the entry as created.
// RFC 8621 §4.8: "If the "blobId", "mailboxIds", or "keywords" properties
// are invalid (e.g., missing, wrong type, id not found), the server MUST
// reject the import with an "invalidProperties" SetError." RFC 8620 §2.6
// requires that SetError to list the invalid properties ("properties":
// String[]) and does not define "invalidArguments" as a SetError type
// (it is a method-level error name, RFC 8620 §3.6.1).
//
// Scope note: ABSENT mailboxIds keeps the round-34 lenient default (import
// into INBOX); only present-but-wrong-typed values are rejected here.

import (
	"testing"
)

func TestEmailImportRejectsWrongTypedProperties(t *testing.T) {
	const user = "user@example.com"
	cases := []struct {
		name      string
		entry     map[string]interface{}
		wantProps []string
	}{
		{
			name:      "mailboxIds not an object",
			entry:     map[string]interface{}{"blobId": "BLOB", "mailboxIds": "inbox"},
			wantProps: []string{"mailboxIds"},
		},
		{
			name:      "mailboxIds value not boolean",
			entry:     map[string]interface{}{"blobId": "BLOB", "mailboxIds": map[string]interface{}{"inbox": "yes"}},
			wantProps: []string{"mailboxIds"},
		},
		{
			name:      "keywords not an object",
			entry:     map[string]interface{}{"blobId": "BLOB", "mailboxIds": map[string]interface{}{"inbox": true}, "keywords": "$seen"},
			wantProps: []string{"keywords"},
		},
		{
			name:      "keywords value not boolean",
			entry:     map[string]interface{}{"blobId": "BLOB", "mailboxIds": map[string]interface{}{"inbox": true}, "keywords": map[string]interface{}{"$seen": "yes"}},
			wantProps: []string{"keywords"},
		},
		{
			name:      "blobId not a string",
			entry:     map[string]interface{}{"blobId": 42, "mailboxIds": map[string]interface{}{"inbox": true}},
			wantProps: []string{"blobId"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, db, msgStore, cleanup := setupTestServer(t)
			defer cleanup()

			blobID, err := msgStore.StoreMessage(user, []byte("Subject: typed\r\n\r\nbody\r\n"))
			if err != nil {
				t.Fatal(err)
			}
			entry := map[string]interface{}{}
			for k, v := range tc.entry {
				if v == "BLOB" {
					v = blobID
				}
				entry[k] = v
			}

			resp := server.handleEmailImport(user, MethodCall{
				Name: "Email/import",
				Args: map[string]interface{}{
					"accountId": user,
					"emails":    map[string]interface{}{"k1": entry},
				},
				ID: "call-1",
			})

			createdMap, _ := resp.Args["created"].(map[string]Email)
			if _, present := createdMap["k1"]; present {
				t.Fatalf("FAIL: %s: entry created; want rejection with an \"invalidProperties\" SetError (RFC 8621 §4.8)", tc.name)
			}
			notCreated, _ := resp.Args["notCreated"].(map[string]interface{})
			errObj, ok := notCreated["k1"].(map[string]interface{})
			if !ok {
				t.Fatalf("FAIL: %s: entry neither created nor reported in notCreated: %v", tc.name, resp.Args)
			}
			if got, _ := errObj["type"].(string); got != "invalidProperties" {
				t.Fatalf("FAIL: %s: SetError type = %q, want \"invalidProperties\" (RFC 8621 §4.8)", tc.name, got)
			}
			props, ok := errObj["properties"].([]string)
			if !ok || len(props) == 0 {
				t.Fatalf("FAIL: %s: SetError lacks a non-empty \"properties\" list (RFC 8620 §2.6): %v", tc.name, errObj)
			}
			// Synthesis-immune storage probe: a rejected entry must store nothing.
			uids, _ := db.GetMessageUIDs(user, "INBOX")
			for _, uid := range uids {
				if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
					t.Fatalf("FAIL: %s: copy stored in INBOX despite rejection", tc.name)
				}
			}
		})
	}
}

// TestEmailImportValidAndDefaultControls pins the behavior that must
// survive the validation gate: well-typed imports still succeed, and an
// entry with NO mailboxIds still lands in INBOX (round-34 lenient default).
func TestEmailImportValidAndDefaultControls(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()

	blobID, err := msgStore.StoreMessage(user, []byte("Subject: control\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	importEntry := func(entry map[string]interface{}) Response {
		return server.handleEmailImport(user, MethodCall{
			Name: "Email/import",
			Args: map[string]interface{}{
				"accountId": user,
				"emails":    map[string]interface{}{"k1": entry},
			},
			ID: "call-1",
		})
	}
	copiesInInbox := func() int {
		uids, _ := db.GetMessageUIDs(user, "INBOX")
		n := 0
		for _, uid := range uids {
			if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
				n++
			}
		}
		return n
	}

	// Control 1: well-typed import succeeds.
	resp := importEntry(map[string]interface{}{
		"blobId":     blobID,
		"mailboxIds": map[string]interface{}{"inbox": true},
		"keywords":   map[string]interface{}{"$seen": true},
	})
	createdMap, _ := resp.Args["created"].(map[string]Email)
	if _, present := createdMap["k1"]; !present {
		t.Fatalf("FAIL: CONTROL FAILED: valid import rejected: %v", resp.Args)
	}
	if n := copiesInInbox(); n != 1 {
		t.Fatalf("FAIL: CONTROL FAILED: copies of %s in INBOX = %d, want 1", blobID, n)
	}

	// Control 2: absent mailboxIds keeps the INBOX default (round 34).
	server2, db2, msgStore2, cleanup2 := setupTestServer(t)
	defer cleanup2()
	blobID2, err := msgStore2.StoreMessage(user, []byte("Subject: default\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp2 := server2.handleEmailImport(user, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": user,
			"emails":    map[string]interface{}{"k1": map[string]interface{}{"blobId": blobID2}},
		},
		ID: "call-1",
	})
	createdMap2, _ := resp2.Args["created"].(map[string]Email)
	if _, present := createdMap2["k1"]; !present {
		t.Fatalf("FAIL: CONTROL FAILED: absent mailboxIds no longer defaults to INBOX: %v", resp2.Args)
	}
	uids, _ := db2.GetMessageUIDs(user, "INBOX")
	found := false
	for _, uid := range uids {
		if meta, _ := db2.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("FAIL: CONTROL FAILED: absent-mailboxIds import not stored in INBOX")
	}
}
