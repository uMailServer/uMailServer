package jmap

// Regression test for the round-55 defect: Email/set create did not charge
// the account quota, while Email/import does (F5741: adjustQuota(+size) with
// an overQuota SetError and a rollback on store failure) — so drafts
// bypassed the account quota entirely. RFC 8620 §1.6.2/§5.3 make quota
// accounting server policy; RFC 8621 §4.6 defines the overQuota SetError for
// exactly this rejection. The fix mirrors import's pattern in the create
// loop: charge adjustQuota(+meta.Size) after the blob is stored, reject with
// overQuota when the counter refuses (the content-addressed blob stays
// behind, unreferenced, exactly like a rejected import's upload), and
// release the charge on the store-failure rollback path; destroy already
// refunds (F5742).
//
// The quota seam is installed exactly like quota_lifecycle_test.go: a `used`
// counter that errors when a charge would exceed the limit. Note the charge
// is meta.Size = the whole synthesized RFC 5322 message (headers + body).

import (
	"strings"
	"testing"
)

const r55DurLimit = int64(600)

type r55QuotaStub struct{}

func (*r55QuotaStub) Error() string { return "quota exceeded" }

var errQuotaExceededStub = &r55QuotaStub{}

func r55DurSetup(t *testing.T) (*Server, *int64, func()) {
	t.Helper()
	server, db, _, cleanup := setupTestServer(t)
	if err := db.CreateMailbox("user@example.com", "INBOX"); err != nil {
		t.Fatal(err)
	}
	used := new(int64)
	server.SetQuotaAdjustFunc(func(_ string, delta int64) error {
		if *used+delta > r55DurLimit {
			return errQuotaExceededStub
		}
		*used += delta
		return nil
	})
	return server, used, cleanup
}

// r55DurCreate creates a draft via Email/set and returns whether it was
// created, the notCreated entry on rejection, and the created Email id.
func r55DurCreate(t *testing.T, server *Server, key, bodyText string) (bool, map[string]interface{}, string) {
	t.Helper()
	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{
				key: map[string]interface{}{
					"mailboxIds": map[string]interface{}{"inbox": true},
					"subject":    "R55 durable " + key,
					"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
					"bodyValues": map[string]interface{}{
						"p1": map[string]interface{}{"value": bodyText},
					},
				},
			},
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

func TestCreateEmailChargesQuota(t *testing.T) {
	server, used, cleanup := r55DurSetup(t)
	defer cleanup()
	ok, _, _ := r55DurCreate(t, server, "d1", strings.Repeat("a", 60))
	if !ok {
		t.Fatalf("FAIL: first create rejected; the fixture must fit the limit")
	}
	if *used == 0 {
		t.Fatalf("FAIL: created draft consumed no quota (used=%d); Email/set create must charge like Email/import (F5741)", *used)
	}
}

func TestCreateEmailOverQuotaRejected(t *testing.T) {
	server, used, cleanup := r55DurSetup(t)
	defer cleanup()
	ok1, _, _ := r55DurCreate(t, server, "d1", strings.Repeat("a", 60))
	if !ok1 {
		t.Fatalf("FAIL: first create rejected; fixture must fit")
	}
	before := *used

	ok2, entry, _ := r55DurCreate(t, server, "d2", strings.Repeat("b", 400))
	if ok2 {
		t.Fatalf("FAIL: created draft past the account quota (used %d → %d + a ~560-byte draft); drafts must not bypass the quota (RFC 8620 §1.6.2, RFC 8621 §4.6 overQuota)", before, *used)
	}
	if et, _ := entry["type"].(string); et != "overQuota" {
		t.Fatalf("FAIL: rejection type = %v; want overQuota", entry["type"])
	}
	if *used != before {
		t.Fatalf("FAIL: failed create changed the quota counter (%d → %d); a rejected create must not charge", before, *used)
	}
}

// Control: destroying the created draft refunds the charge (F5742), after
// which a new create fits again — the full charge/refund lifecycle on the
// create path.
func TestCreateQuotaLifecycleChargeRefund(t *testing.T) {
	server, used, cleanup := r55DurSetup(t)
	defer cleanup()

	ok1, _, id := r55DurCreate(t, server, "d1", strings.Repeat("a", 60))
	if !ok1 {
		t.Fatalf("CONTROL FAILED: first create rejected")
	}
	if *used == 0 {
		t.Fatalf("CONTROL FAILED: no charge recorded for the created draft (used=%d)", *used)
	}

	destroy := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"destroy": []interface{}{id},
		},
		ID: "c-destroy",
	}
	dresp := server.handleEmailSet("user@example.com", destroy)
	destroyed, _ := dresp.Args["destroyed"].([]string)
	if len(destroyed) != 1 {
		t.Fatalf("CONTROL FAILED: destroy = %v", dresp.Args)
	}
	if *used != 0 {
		t.Fatalf("CONTROL FAILED: used = %d after destroying the only draft; destroy must refund the charge (F5742)", *used)
	}

	ok2, _, _ := r55DurCreate(t, server, "d3", strings.Repeat("c", 60))
	if !ok2 {
		t.Fatalf("CONTROL FAILED: create after refund rejected (used=%d)", *used)
	}
}
