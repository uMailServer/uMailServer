package jmap

// Regression tests for the Email/set keyword patch-path defect: handleEmailSet
// matched only the literal "keywords" property, so the RFC 8620 §4.3 /
// RFC 8621 §4.3 patch form ("keywords/$flagged": true|null) — the sanctioned
// way to add or remove a single keyword — was silently ignored while the
// response still reported the Email as updated. The full-property form
// ("keywords": {...}) is a property set and correctly REPLACES all keywords;
// that contract is pinned as the control here.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func emailFlags(t *testing.T, db *storage.Database, user string) []string {
	t.Helper()
	uids, err := db.GetMessageUIDs(user, "INBOX")
	if err != nil {
		t.Fatalf("uids: %v", err)
	}
	meta, err := db.GetMessageMetadata(user, "INBOX", uids[0])
	if err != nil || meta == nil {
		t.Fatalf("metadata: %v", err)
	}
	return meta.Flags
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func setCall(user, emailID string, patch map[string]interface{}) MethodCall {
	return MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": user,
			"update":    map[string]interface{}{emailID: patch},
		},
		ID: "call-1",
	}
}

// RFC 8621 §4.3: "keywords/$flagged": true adds the keyword while every other
// keyword is preserved.
func TestEmailSet_KeywordPathAddsSingleKeyword(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	msg := mkMsg(1, "msg-1", "Keywords", 100)
	msg.Flags = []string{"\\Seen"}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, msg); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, setCall(user, "msg-1", map[string]interface{}{
		"keywords/$flagged": true,
	}))

	flags := emailFlags(t, db, user)
	if !hasFlag(flags, "\\Flagged") {
		t.Fatalf("FAIL: patch path keywords/$flagged=true was ignored (flags = %v)", flags)
	}
	if !hasFlag(flags, "\\Seen") {
		t.Fatalf("FAIL: patch path dropped the pre-existing \\Seen keyword (flags = %v)", flags)
	}
}

// RFC 8620 §4.3 patch semantics: null removes the property entry.
func TestEmailSet_KeywordPathNullRemovesKeyword(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	msg := mkMsg(1, "msg-1", "Keywords", 100)
	msg.Flags = []string{"\\Seen", "\\Flagged"}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, msg); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, setCall(user, "msg-1", map[string]interface{}{
		"keywords/$flagged": nil,
	}))

	flags := emailFlags(t, db, user)
	if hasFlag(flags, "\\Flagged") {
		t.Fatalf("FAIL: patch path keywords/$flagged=null did not remove the keyword (flags = %v)", flags)
	}
	if !hasFlag(flags, "\\Seen") {
		t.Fatalf("FAIL: patch path removed an unrelated keyword (flags = %v)", flags)
	}
}

// RFC 8620 §4.3: a patch value that is neither bool nor null is invalid and
// must reject the update with invalidPatch — not silently remove the keyword.
func TestEmailSet_KeywordPathInvalidValueRejected(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	msg := mkMsg(1, "msg-1", "Keywords", 100)
	msg.Flags = []string{"\\Seen"}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, msg); err != nil {
		t.Fatal(err)
	}

	resp := server.handleEmailSet(user, setCall(user, "msg-1", map[string]interface{}{
		"keywords/$flagged": "yes",
	}))

	nu, _ := resp.Args["notUpdated"].(map[string]interface{})
	entry, _ := nu["msg-1"].(map[string]interface{})
	if entry == nil || entry["type"] != "invalidPatch" {
		t.Fatalf("FAIL: invalid patch value was not rejected with invalidPatch (response: %+v)", resp)
	}
	flags := emailFlags(t, db, user)
	if hasFlag(flags, "\\Flagged") {
		t.Fatalf("FAIL: invalid patch value changed the keyword state (flags = %v)", flags)
	}
	if !hasFlag(flags, "\\Seen") {
		t.Fatalf("FAIL: invalid patch value altered existing flags (flags = %v)", flags)
	}
}

// Control: the full-property form sets every keyword, replacing the others
// (RFC 8620 §4.3 property set). This passed before the fix and must keep
// passing — it pins the boundary between the two forms.
func TestEmailSet_KeywordPropertyFormReplaces(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()

	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	msg := mkMsg(1, "msg-1", "Keywords", 100)
	msg.Flags = []string{"\\Seen"}
	if err := db.StoreMessageMetadata(user, "INBOX", 1, msg); err != nil {
		t.Fatal(err)
	}

	server.handleEmailSet(user, setCall(user, "msg-1", map[string]interface{}{
		"keywords": map[string]interface{}{"$answered": true},
	}))

	flags := emailFlags(t, db, user)
	if len(flags) != 1 || flags[0] != "\\Answered" {
		t.Fatalf("CONTROL FAILED (harness): property form must replace keywords, got %v", flags)
	}
}
