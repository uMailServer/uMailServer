package jmap

// Regression tests for arbitrary JMAP keyword storage end-to-end (RFC 8621
// §2: "Users may add arbitrary keywords" — syntax 1-255 chars of %x21-%x7e
// excluding ( ) { ] % * " \; case-insensitive; "servers MUST return keywords
// in lowercase"). Round 45 implemented the feature across all five sites:
// import write, Email/set validate (keywordModeled is now a syntax check),
// Email/set apply (emailKeywordPatch stores arbitrary names lowercase
// verbatim), storageToJMAPEmail emission (default branch, lowercased), and
// the metaHasKeyword filter fallback (lowercased).
//
// Preserved from earlier rounds and re-pinned here: padded keyword names
// (" $seen") remain invalid syntax (round 42), and system keywords keep
// their IMAP system-flag mapping.

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestArbitraryKeywordStorageEndToEnd(t *testing.T) {
	const user = "user@example.com"

	seed := func(t *testing.T, db *storage.Database, msgStore *storage.MessageStore) string {
		t.Helper()
		data := []byte("Subject: arbitrary\r\n\r\nbody\r\n")
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

	t.Run("whole-form arbitrary keyword applies", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$label2": true},
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: arbitrary keyword \"$label2\" rejected (RFC 8621 §2: users may add arbitrary keywords): %v", resp.Args)
		}
		if !hasFlag(flagsOf(t, db, blobID), "$label2") {
			t.Fatalf("FAIL: \"$label2\" accepted but not stored")
		}
	})

	t.Run("path-form arbitrary keyword applies", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords/$label3": true,
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: arbitrary path keyword \"$label3\" rejected (RFC 8621 §2): %v", resp.Args)
		}
		if !hasFlag(flagsOf(t, db, blobID), "$label3") {
			t.Fatalf("FAIL: \"$label3\" accepted but not stored")
		}
	})

	t.Run("mixed-case arbitrary keyword is stored lowercase", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

		resp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$Label1": true},
					},
				},
			},
			ID: "c1",
		})

		updated, _ := resp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: mixed-case \"$Label1\" rejected: %v", resp.Args)
		}
		// §2: keywords are case-insensitive and stored/returned lowercase.
		if !hasFlag(flagsOf(t, db, blobID), "$label1") {
			t.Fatalf("FAIL: \"$Label1\" not stored lowercase as \"$label1\" (flags=%v)", flagsOf(t, db, blobID))
		}
	})

	t.Run("padded keyword name stays rejected", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

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
			t.Fatalf("FAIL: padded keyword name accepted (round-42 regression: RFC 8621 §2 syntax excludes whitespace)")
		}
		notUpdated, _ := resp.Args["notUpdated"].(map[string]interface{})
		errObj, ok := notUpdated[blobID].(map[string]interface{})
		if !ok {
			t.Fatalf("FAIL: entry neither updated nor reported in notUpdated: %v", resp.Args)
		}
		if got, _ := errObj["type"].(string); got != "invalidProperties" {
			t.Fatalf("FAIL: notUpdated SetError type = %q, want \"invalidProperties\" (round-42 regression)", got)
		}
	})

	t.Run("hasKeyword filter matches arbitrary keyword case-insensitively", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

		setResp := server.handleEmailSet(user, MethodCall{
			Name: "Email/set",
			Args: map[string]interface{}{
				"accountId": user,
				"update": map[string]interface{}{
					blobID: map[string]interface{}{
						"keywords": map[string]interface{}{"$label4": true},
					},
				},
			},
			ID: "c1",
		})
		updated, _ := setResp.Args["updated"].(map[string]interface{})
		if _, ok := updated[blobID]; !ok {
			t.Fatalf("FAIL: CONTROL FAILED: arbitrary keyword not applied: %v", setResp.Args)
		}

		// The filter is matched case-insensitively (§2) even though the
		// stored form is lowercase.
		qresp := server.handleEmailQuery(user, MethodCall{
			Name: "Email/query",
			Args: map[string]interface{}{
				"accountId": user,
				"filter":    map[string]interface{}{"hasKeyword": "$LABEL4"},
			},
			ID: "c2",
		})
		idsRaw, ok := qresp.Args["ids"].([]string)
		if !ok {
			t.Fatalf("FAIL: decode query ids: %v", qresp.Args)
		}
		found := false
		for _, id := range idsRaw {
			if id == blobID {
				found = true
			}
		}
		if !found {
			t.Fatalf("FAIL: hasKeyword \"$LABEL4\" did not match the stored \"$label4\" keyword (ids=%v)", idsRaw)
		}
	})

	t.Run("system keyword control", func(t *testing.T) {
		server, db, msgStore, cleanup := setupTestServer(t)
		defer cleanup()
		blobID := seed(t, db, msgStore)

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
			t.Fatalf("FAIL: CONTROL FAILED: system-keyword update not applied: %v", resp.Args)
		}
		if !hasFlag(flagsOf(t, db, blobID), "\\Seen") {
			t.Fatalf("FAIL: CONTROL FAILED: \\Seen flag not stored")
		}
	})
}
