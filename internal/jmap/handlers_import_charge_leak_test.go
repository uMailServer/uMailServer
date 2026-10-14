package jmap

// Regression test for the round-58 defect: Email/import's F5741 quota
// charge sat BEFORE the applyKeywordsToMeta gate, so a keyword-rejected
// import leaked the charge (adjustQuota(+meta.Size) with no release on
// that return path — the rollback only covered storeEmailCopies
// serverFail). RFC 8620 §1.6.2/§5.3 quota semantics; RFC 8621 §4.6
// overQuota. Round 58 mirrors the round-57 create-loop fix: the charge
// moved BELOW the validation gates (charge exactly on commit), so a
// keyword-rejected import leaves the counter untouched while a valid
// import still charges (F5741) and destroy refunds (F5742).
//
// The quota seam is installed exactly like quota_lifecycle_test.go: a
// `used` counter that errors when a charge would exceed the limit.

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func r58DurSetup(t *testing.T) (*Server, *storage.MessageStore, *int64, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	used := new(int64)
	server.SetQuotaAdjustFunc(func(_ string, delta int64) error {
		if *used+delta > 5000 {
			return errQuotaExceededStub
		}
		*used += delta
		return nil
	})
	return server, msgStore, used, cleanup
}

func r58DurImport(t *testing.T, server *Server, blobID, keywords string) (bool, map[string]interface{}, string) {
	t.Helper()
	importData := map[string]interface{}{
		"blobId":     blobID,
		"mailboxIds": map[string]interface{}{"inbox": true},
	}
	if keywords != "" {
		importData["keywords"] = map[string]interface{}{keywords: true}
	}
	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"emails": map[string]interface{}{"i1": importData},
		},
		ID: "c1",
	}
	resp := server.handleEmailImport("user@example.com", call)
	cr, _ := resp.Args["created"].(map[string]Email)
	if e, ok := cr["i1"]; ok {
		return true, nil, e.ID
	}
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	entry, _ := nc["i1"].(map[string]interface{})
	return false, entry, ""
}

func TestImportKeywordRejectLeaksNoCharge(t *testing.T) {
	server, msgStore, used, cleanup := r58DurSetup(t)
	defer cleanup()
	blobID, err := msgStore.StoreMessage("user@example.com", []byte("From: a@x.test\r\nSubject: R58 durable\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	ok, entry, _ := r58DurImport(t, server, blobID, "bad keyword")
	if ok {
		t.Fatalf("FAIL: invalid keyword accepted; fixture expects an invalidProperties rejection")
	}
	if et, _ := entry["type"].(string); et != "invalidProperties" {
		t.Fatalf("FAIL: rejection type = %v; want invalidProperties", et)
	}
	if *used != 0 {
		t.Fatalf("FAIL: keyword-rejected import leaked the F5741 quota charge (used=%d); a rejected import must leave the counter untouched", *used)
	}
}

// Control: a valid import charges (F5741) and destroying it refunds
// (F5742) — the full lifecycle on the import path.
func TestImportValidChargesAndDestroyRefunds(t *testing.T) {
	server, msgStore, used, cleanup := r58DurSetup(t)
	defer cleanup()
	blobID, err := msgStore.StoreMessage("user@example.com", []byte("From: a@x.test\r\nSubject: R58 durable valid\r\n\r\n"+strings.Repeat("body ", 10)+"\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	ok, _, id := r58DurImport(t, server, blobID, "")
	if !ok {
		t.Fatalf("CONTROL FAILED: valid import rejected")
	}
	if *used == 0 {
		t.Fatalf("CONTROL FAILED: valid import charged no quota (F5741 broken)")
	}

	destroy := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"destroy": []interface{}{id},
		},
		ID: "c2",
	}
	dresp := server.handleEmailSet("user@example.com", destroy)
	destroyed, _ := dresp.Args["destroyed"].([]string)
	if len(destroyed) != 1 {
		t.Fatalf("CONTROL FAILED: destroy = %v", dresp.Args)
	}
	if *used != 0 {
		t.Fatalf("CONTROL FAILED: used = %d after destroying the only import; destroy must refund the charge (F5742)", *used)
	}
}
