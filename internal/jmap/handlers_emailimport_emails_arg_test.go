package jmap

// Regression test for the silent empty success on an absent or wrong-typed
// top-level "emails" argument in Email/import: the handler coerced
// args["emails"] to a nil map and returned a normal Email/import response
// with empty created/notCreated maps. RFC 8621 §4.8 defines "emails:
// Id[EmailImport]" as a required argument (no default), and RFC 8620 §3.6.1
// requires the method-level "invalidArguments" error when "One of the
// arguments is of the wrong type or is otherwise invalid, or a required
// argument is missing". Rounds 35/38 established that "invalidArguments" is
// legal ONLY at the method level; this site is exactly that.

import (
	"testing"
)

func TestEmailImportEmailsArgRequired(t *testing.T) {
	const user = "user@example.com"

	cases := []struct {
		name   string
		emails interface{}
	}{
		{"absent emails argument", nil},
		{"wrong-typed emails argument", "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _, msgStore, cleanup := setupTestServer(t)
			defer cleanup()

			if _, err := msgStore.StoreMessage(user, []byte("Subject: t\r\n\r\nbody\r\n")); err != nil {
				t.Fatal(err)
			}

			args := map[string]interface{}{
				"accountId": user,
			}
			if tc.emails != nil {
				args["emails"] = tc.emails
			}
			resp := server.handleEmailImport(user, MethodCall{
				Name: "Email/import",
				Args: args,
				ID:   "c1",
			})

			if resp.Name != "error" {
				t.Fatalf("FAIL: %s: response name = %q with args %v, want the method-level \"error\" response (RFC 8620 §3.6.1)", tc.name, resp.Name, resp.Args)
			}
			if got, _ := resp.Args["type"].(string); got != "invalidArguments" {
				t.Fatalf("FAIL: %s: error type = %q, want \"invalidArguments\" (RFC 8620 §3.6.1)", tc.name, got)
			}
		})
	}

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

		if resp.Name != "Email/import" {
			t.Fatalf("FAIL: CONTROL FAILED: valid import rejected: %v", resp.Args)
		}
		created, _ := resp.Args["created"].(map[string]Email)
		if _, ok := created["k2"]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: valid import produced no created[k2]: %v", resp.Args)
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
