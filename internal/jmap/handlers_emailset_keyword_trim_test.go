package jmap

// Regression test for the Email/set keyword validate/apply mismatch:
// validateEmailPatch checked keyword names through keywordModeled, which
// TrimSpaces, while emailKeywordPatch applies jmapKeywordToIMAPFlag
// UNTRIMMED. A padded name like " $seen" therefore passed validation and
// then silently did nothing — and in the whole-property (replace) form it
// wiped ALL existing keywords while the response reported "updated" (data
// loss). RFC 8621 §2 defines keyword syntax as 1-255 characters of
// %x21-%x7e — space excluded — so padded names are invalid syntax and must
// be rejected with an "invalidProperties" SetError (RFC 8620 §2.6).
// keywordModeled now checks the name AS WRITTEN, matching the apply path.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestEmailSetRejectsPaddedKeywordNames(t *testing.T) {
	const user = "user@example.com"

	t.Run("whole-form padded keyword", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		data := []byte("Subject: w\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
		if err != nil {
			t.Fatal(err)
		}
		meta := &storage.MessageMetadata{
			MessageID: blobID, InternalDate: time.Now(), Flags: []string{"\\Flagged"},
			Size: int64(len(data)), UID: 1,
		}
		if err := db.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{" $seen": true},
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; ok {
			flags := storedFlags(t, db, user, blobID)
			hasFlagged := false
			for _, f := range flags {
				if f == "\\Flagged" {
					hasFlagged = true
				}
			}
			if !hasFlagged {
				t.Fatalf("FAIL: padded-keyword update reported \"updated\" and WIPED the existing \\Flagged keyword (flags=%v); RFC 8621 §2 keyword syntax excludes whitespace — must be rejected with invalidProperties", flags)
			}
			t.Fatalf("FAIL: padded-keyword update reported \"updated\" without effect (flags=%v); must be rejected with invalidProperties", flags)
		}
		notUpdated, _ := resp.Args["notUpdated"].(map[string]interface{})
		errObj, ok := notUpdated[blobID].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither updated nor reported in notUpdated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notUpdated SetError type = %q, want \"invalidProperties\" (RFC 8621 §2 keyword syntax excludes whitespace; RFC 8620 §2.6)", got)
		}
		// Rejected: existing keywords must be intact.
		hasFlagged := false
		for _, f := range storedFlags(t, db, user, blobID) {
			if f == "\\Flagged" {
				hasFlagged = true
			}
		}
		if !hasFlagged {
			t.Fatalf("FAIL: \\Flagged keyword lost although the update was rejected")
		}
	})

	t.Run("path-form padded keyword", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		data := []byte("Subject: p\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
		if err != nil {
			t.Fatal(err)
		}
		meta := &storage.MessageMetadata{
			MessageID: blobID, InternalDate: time.Now(), Flags: []string{"\\Flagged"},
			Size: int64(len(data)), UID: 1,
		}
		if err := db.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords/ $seen": true,
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; ok {
			t.Fatalf("FAIL: padded path-form keyword reported \"updated\" without effect; must be rejected with invalidProperties")
		}
		notUpdated, _ := resp.Args["notUpdated"].(map[string]interface{})
		errObj, ok := notUpdated[blobID].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither updated nor reported in notUpdated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notUpdated SetError type = %q, want \"invalidProperties\" (RFC 8621 §2; RFC 8620 §2.6)", got)
		}
	})

	t.Run("exact keyword control", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()

		data := []byte("Subject: c\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
		if err != nil {
			t.Fatal(err)
		}
		meta := &storage.MessageMetadata{
			MessageID: blobID, InternalDate: time.Now(), Flags: []string{},
			Size: int64(len(data)), UID: 1,
		}
		if err := db.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
			t.Fatal(err)
		}

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$seen": true},
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: exact-keyword update not applied: %v", resp.Args)
		}
		seen := false
		for _, f := range storedFlags(t, db, user, blobID) {
			if f == "\\Seen" {
				seen = true
			}
		}
		if !seen {
			t.Fatalf("FAIL: CONTROL FAILED: \\Seen flag not stored")
		}
	})
}

// storedFlags probes the stored flags of blobID synthesis-immune
// (round-21 lesson: GetMessageMetadata, not just the UID list).
func storedFlags(t *testing.T, db *storage.Database, user, blobID string) []string {
	t.Helper()
	uids, _ := db.GetMessageUIDs(user, "INBOX")
	for _, uid := range uids {
		if meta, _ := db.GetMessageMetadata(user, "INBOX", uid); meta != nil && meta.MessageID == blobID {
			return meta.Flags
		}
	}
	t.Fatalf("FAIL: email %s not found in INBOX", blobID)
	return nil
}
