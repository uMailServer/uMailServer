package jmap

// Regression test for the round-57 defect: rejected Email/set creates left
// their internally synthesized content-addressed blob behind — a true
// orphan (the client never received its blobId, no mailbox references it,
// destroy cannot reach it) — and, because round 55 placed the quota charge
// BEFORE the keywords/mailbox validation gates, a rejection there also
// leaked the charge. (An import-rejected blob is NOT an orphan: it is
// client-uploaded, upload-charged (F5740) and re-importable by blobId.)
//
// The fix: the quota charge moved AFTER the validation gates (charge
// exactly on commit; the only post-charge rejection is storeEmailCopies
// serverFail, which already refunds), and releaseSyntheticBlob
// (best-effort DeleteMessage) runs at every post-store rejection site —
// safe by construction because DeleteMessage refuses via the blob
// reference checker (installed in production, internal/jmap/server.go:164
// → db.BlobReferenced) when the blob is referenced elsewhere (identical
// content shared with a live Email).

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

const r57DurLimit = int64(600)

func r57DurSetup(t *testing.T) (*Server, *storage.MessageStore, *int64, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	used := new(int64)
	server.SetQuotaAdjustFunc(func(_ string, delta int64) error {
		if *used+delta > r57DurLimit {
			return errQuotaExceededStub
		}
		*used += delta
		return nil
	})
	return server, msgStore, used, cleanup
}

// r57DurCreate returns (created, notCreated entry, created id).
func r57DurCreate(t *testing.T, server *Server, key, bodyText string, keywords interface{}, mailboxID string) (bool, map[string]interface{}, string) {
	t.Helper()
	create := map[string]interface{}{
		"mailboxIds": map[string]interface{}{mailboxID: true},
		"subject":    "R57 durable " + key,
		"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
		"bodyValues": map[string]interface{}{
			"p1": map[string]interface{}{"value": bodyText},
		},
	}
	if keywords != nil {
		create["keywords"] = keywords
	}
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{key: create},
		},
		ID: "c-" + key,
	}
	resp := server.handleEmailSet("user@example.com", call)
	cr, _ := resp.Args["created"].(map[string]Email)
	if e, ok := cr[key]; ok {
		return true, nil, e.ID
	}
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	entry, _ := nc[key].(map[string]interface{})
	return false, entry, ""
}

func TestCreateOverQuotaReleasesBlobAndCharge(t *testing.T) {
	server, msgStore, used, cleanup := r57DurSetup(t)
	defer cleanup()
	ok1, _, _ := r57DurCreate(t, server, "d1", strings.Repeat("a", 60), nil, "inbox")
	if !ok1 {
		t.Fatalf("FAIL: first create rejected; the fixture must fit the limit")
	}
	before, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	ok2, entry, _ := r57DurCreate(t, server, "d2", strings.Repeat("b", 400), nil, "inbox")
	if ok2 {
		t.Fatalf("FAIL: oversized draft created; fixture expects an overQuota rejection")
	}
	if et, _ := entry["type"].(string); et != "overQuota" {
		t.Fatalf("FAIL: rejection type = %v; want overQuota", entry["type"])
	}
	after, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("FAIL: rejected create left %d orphan blob bytes behind (%d → %d); the synthesized blob must be released on rejection", after-before, before, after)
	}
	if *used != before {
		t.Fatalf("FAIL: rejected create left a quota charge (%d → %d)", before, *used)
	}
}

func TestCreateKeywordRejectReleasesBlobAndCharge(t *testing.T) {
	server, msgStore, used, cleanup := r57DurSetup(t)
	defer cleanup()
	before, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	ok, entry, _ := r57DurCreate(t, server, "k1", "body text", map[string]interface{}{"bad keyword": true}, "inbox")
	if ok {
		t.Fatalf("FAIL: invalid keyword accepted; fixture expects an invalidProperties rejection")
	}
	if et, _ := entry["type"].(string); et != "invalidProperties" {
		t.Fatalf("FAIL: rejection type = %v; want invalidProperties", et)
	}
	after, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("FAIL: keyword-rejected create left %d orphan blob bytes behind (%d → %d)", after-before, before, after)
	}
	if *used != 0 {
		t.Fatalf("FAIL: keyword-rejected create left a quota charge (used=%d); the charge must not survive a rejected create", *used)
	}
}

func TestCreateMailboxNotFoundReleasesBlobAndCharge(t *testing.T) {
	server, msgStore, used, cleanup := r57DurSetup(t)
	defer cleanup()
	before, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	ok, entry, _ := r57DurCreate(t, server, "m1", "body text", nil, "does-not-exist")
	if ok {
		t.Fatalf("FAIL: unknown mailbox accepted; fixture expects a mailboxNotFound rejection")
	}
	if et, _ := entry["type"].(string); et != "mailboxNotFound" {
		t.Fatalf("FAIL: rejection type = %v; want mailboxNotFound", et)
	}
	after, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("FAIL: mailboxNotFound create left %d orphan blob bytes behind (%d → %d)", after-before, before, after)
	}
	if *used != 0 {
		t.Fatalf("FAIL: mailboxNotFound create left a quota charge (used=%d)", *used)
	}
}

// Control: a successful create RETAINS its blob and its charge — the fix
// must only release rejected creates, never live ones.
func TestCreateSuccessRetainsBlobAndCharge(t *testing.T) {
	server, msgStore, used, cleanup := r57DurSetup(t)
	defer cleanup()
	before, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	ok, _, _ := r57DurCreate(t, server, "d1", strings.Repeat("a", 60), nil, "inbox")
	if !ok {
		t.Fatalf("CONTROL FAILED: create rejected; the fixture must fit")
	}
	after, err := msgStore.UserUsage("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("CONTROL FAILED: successful create retained no blob bytes (%d → %d)", before, after)
	}
	if *used == 0 {
		t.Fatalf("CONTROL FAILED: successful create charged no quota (round-55 behavior broken)")
	}
}
