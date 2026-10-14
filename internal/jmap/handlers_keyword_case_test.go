package jmap

// Regression test for the case-sensitive JMAP keyword lookups: RFC 8621 §2
// makes keywords case-insensitive ("a keyword is a case-insensitive string
// of 1-255 characters") and requires servers to return them lowercase
// ("Because JSON is case sensitive, servers MUST return keywords in
// lowercase"), but jmapKeywordToIMAPFlag was a case-sensitive switch on the
// four lowercase names, so "$Seen"/"$FLAGGED"/"$Draft" were rejected with
// invalidProperties by Email/set and Email/import. The lookup now
// case-folds (strings.ToLower) before matching, so every caller —
// keywordModeled, both emailKeywordPatch forms, handleEmailImport and the
// hasKeyword filter — behaves consistently; storage keeps the fixed-case
// IMAP flags and emission returns the lowercase JMAP form.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestKeywordsAreCaseInsensitive(t *testing.T) {
	const user = "user@example.com"

	seedEmailWithFlags := func(t *testing.T, db *storage.Database, msgStore *storage.MessageStore, subject string, flags []string) string {
		t.Helper()
		data := []byte("Subject: " + subject + "\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
		if err != nil {
			t.Fatal(err)
		}
		meta := &storage.MessageMetadata{
			MessageID:    blobID,
			InternalDate: time.Now(),
			Flags:        flags,
			Size:         int64(len(data)),
			UID:          1,
		}
		if err := db.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
			t.Fatal(err)
		}
		return blobID
	}
	flagsOf := func(t *testing.T, db *storage.Database, blobID string) []string {
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
	hasFlag := func(flags []string, want string) bool {
		for _, f := range flags {
			if f == want {
				return true
			}
		}
		return false
	}

	t.Run("Email/set whole-form mixed case", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seedEmailWithFlags(t, db, msgStore, "case1", []string{})

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$Seen": true},
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: mixed-case \"$Seen\" rejected although RFC 8621 §2 makes keywords case-insensitive: %v", resp.Args)
		}
		if !hasFlag(flagsOf(t, db, blobID), "\\Seen") {
			t.Fatalf("FAIL: \"$Seen\" accepted but \\Seen not stored")
		}
	})

	t.Run("Email/set path-form mixed case", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seedEmailWithFlags(t, db, msgStore, "case2", []string{})

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords/$FLAGGED": true,
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: mixed-case \"$FLAGGED\" path rejected although RFC 8621 §2 makes keywords case-insensitive: %v", resp.Args)
		}
		if !hasFlag(flagsOf(t, db, blobID), "\\Flagged") {
			t.Fatalf("FAIL: \"$FLAGGED\" accepted but \\Flagged not stored")
		}
	})

	t.Run("Email/import mixed case returns lowercase", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		data := []byte("Subject: case3\r\n\r\nbody\r\n")
		blobID, err := msgStore.StoreMessage(user, data)
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
						"keywords":   map[string]interface{}{"$Draft": true},
					},
				},
			},
			ID: "c1",
		})

		created, _ := resp.Args["created"].(map[string]Email)
		entry, ok := created["k1"]
		if !ok {
			t.Fatalf("FAIL: mixed-case \"$Draft\" import rejected although RFC 8621 §2 makes keywords case-insensitive: %v", resp.Args)
		}
		// §2: servers MUST return keywords in LOWERCASE.
		if !entry.Keywords["$draft"] {
			t.Fatalf("FAIL: imported keyword not returned lowercase as \"$draft\" (keywords=%v)", entry.Keywords)
		}
		if !hasFlag(flagsOf(t, db, blobID), "\\Draft") {
			t.Fatalf("FAIL: \"$Draft\" import accepted but \\Draft not stored")
		}
	})

	t.Run("exact lowercase control", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seedEmailWithFlags(t, db, msgStore, "control", []string{})

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
		if !hasFlag(flagsOf(t, db, blobID), "\\Seen") {
			t.Fatalf("FAIL: CONTROL FAILED: \\Seen flag not stored")
		}
	})
}
