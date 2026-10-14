package jmap

// Regression test for the illegal SetError type on non-object /set update
// values: the Email/set and Mailbox/set update loops typed a notUpdated
// entry with "invalidArguments" — a *method-level* error name (RFC 8620
// §3.6.1) that is not part of the §2.6 SetError type set — where §4.3
// defines update as Id[PatchObject] and §2.6 defines "invalidPatch" for
// exactly this condition ("The PatchObject given to update the record was
// not a valid patch").

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestSetUpdateNonObjectValueIsInvalidPatch(t *testing.T) {
	const user = "user@example.com"

	t.Run("Email/set", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		data := []byte("Subject: typed\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
		if err != nil {
			t.Fatal(err)
		}
		meta := &storage.MessageMetadata{
			MessageID:    blobID,
			InternalDate: time.Now(),
			Flags:        []string{},
			Size:         int64(len(data)),
			UID:          1,
		}
		if err := db.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					// Control: a valid patch on a real email must still apply.
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$seen": true},
					},
					// Trigger: the update value itself is not a PatchObject.
					"ghost-id": "not-a-patch-object",
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: valid keyword patch not applied: %v", resp.Args)
		}
		uids, _ := db.GetMessageUIDs(user, "INBOX")
		seen := false
		for _, uid := range uids {
			if m, _ := db.GetMessageMetadata(user, "INBOX", uid); m != nil && m.MessageID == blobID {
				for _, f := range m.Flags {
					if f == "\\Seen" {
						seen = true
					}
				}
			}
		}
		if !seen {
			t.Fatalf("FAIL: CONTROL FAILED: $seen keyword not stored after update")
		}

		notUpdated, _ := resp.Args["notUpdated"].(map[string]interface{})
		errObj, ok := notUpdated["ghost-id"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: non-object update value neither updated nor reported in notUpdated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidPatch" {
			t.Fatalf("FAIL: notUpdated SetError type = %q, want \"invalidPatch\" (RFC 8620 §2.6/§4.3; \"invalidArguments\" is a method-level error name, §3.6.1)", got)
		}
	})

	t.Run("Mailbox/set", func(t *testing.T) {
		server, db, _, cleanup := setupTestServer(t)
		defer cleanup()

		if err := db.CreateMailbox(user, "Archive"); err != nil {
			t.Fatal(err)
		}

		resp := server.handleMailboxSet(user, MethodCall{
			Name: "Mailbox/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					// Control: a valid rename patch on a real mailbox.
					"archive": map[string]interface{}{"name": "Renamed"},
					// Trigger: the update value itself is not a PatchObject.
					"inbox": "not-a-patch-object",
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated["archive"]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: valid rename patch not applied: %v", resp.Args)
		}
		if !server.mailboxExists(user, "Renamed") {
			t.Fatalf("FAIL: CONTROL FAILED: mailbox not renamed after update")
		}

		notUpdated, _ := resp.Args["notUpdated"].(map[string]interface{})
		errObj, ok := notUpdated["inbox"].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: non-object update value neither updated nor reported in notUpdated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidPatch" {
			t.Fatalf("FAIL: notUpdated SetError type = %q, want \"invalidPatch\" (RFC 8620 §2.6/§4.3; \"invalidArguments\" is a method-level error name, §3.6.1)", got)
		}
	})
}
