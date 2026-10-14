package jmap

// Regression test for the round-53 defect: the created responses of
// Email/set create and Email/import returned Email objects derived from
// storageToJMAPEmail WITHOUT the RFC 8621 §4.1.2 server-set properties
// preview and hasAttachment (zero-value struct + omitempty JSON dropped
// them). RFC 8620 §5.3 returns the created object; §4.1.2 makes both
// properties immutable and server-set — a client cannot compute them — and
// Email/get of the same message reports them (round 52), so the created
// object must agree. The original round directive ("Email/query listings")
// was premise-corrected during discovery: RFC 8621 §4.4 defines Email/query
// as ids-only, and handleEmailQuery returns "ids" only — no Email objects
// exist there. The fix: applyServerSetProps (emailbody.go) fills both
// properties from the raw message bytes at both created-response sites.

import (
	"strings"
	"testing"
)

const r53DurPDF = "JVBERi0xLjQKJcfsj6IK"

func TestCreatedEmailSetCarriesServerSetProps(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(user, []byte(r53DurPDF))
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("Round53 preview sentence. ", 30)

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{
				"d1": map[string]interface{}{
					"mailboxIds": map[string]interface{}{"inbox": true},
					"subject":    "R53 durable create",
					"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
					"attachments": []interface{}{
						map[string]interface{}{"blobId": blobID, "type": "application/pdf", "disposition": "attachment"},
					},
					"bodyValues": map[string]interface{}{
						"p1": map[string]interface{}{"value": long},
					},
				},
			},
		},
		ID: "c1",
	}
	resp := server.handleEmailSet(user, call)
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["d1"]
	if !ok {
		t.Fatalf("FAIL: created[d1] missing (notCreated=%v)", resp.Args["notCreated"])
	}
	if !e.HasAttachment {
		t.Fatalf("FAIL: created.HasAttachment = false; §4.1.2 server-set property must agree with Email/get of the same message")
	}
	if len(e.Preview) == 0 || len(e.Preview) > 256 {
		t.Fatalf("FAIL: created.Preview = %q (len %d); §4.1.2 requires a non-empty fragment ≤256 chars", e.Preview, len(e.Preview))
	}
	if !strings.HasPrefix(e.Preview, "Round53 preview sentence.") {
		t.Fatalf("FAIL: created.Preview = %q; want a fragment of the created plain body", e.Preview)
	}
}

func TestCreatedEmailImportCarriesServerSetProps(t *testing.T) {
	const user = "user@example.com"
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}
	raw := "From: a@x.test\r\nTo: b@y.test\r\nSubject: R53 durable import\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=r53dur\r\n\r\n" +
		"--r53dur\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Just a short plain body.\r\n" +
		"--r53dur\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\n\r\n" +
		r53DurPDF + "\r\n" +
		"--r53dur--\r\n"
	blobID, err := msgStore.StoreMessage(user, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "c2",
	}
	resp := server.handleEmailImport(user, call)
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["i1"]
	if !ok {
		t.Fatalf("FAIL: created[i1] missing (notCreated=%v)", resp.Args["notCreated"])
	}
	if !e.HasAttachment {
		t.Fatalf("FAIL: created.HasAttachment = false; §4.1.2 server-set property must agree with Email/get of the same message")
	}
	if e.Preview != "Just a short plain body." {
		t.Fatalf("FAIL: created.Preview = %q; want the plain-body fragment", e.Preview)
	}
}

// Control: a plain-only created message reports hasAttachment=false and the
// body fragment — the properties are computed, not blindly set true.
func TestCreatedPlainOnlyHasServerSetProps(t *testing.T) {
	const user = "user@example.com"
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatal(err)
	}

	call := MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"create": map[string]interface{}{
				"p1": map[string]interface{}{
					"mailboxIds": map[string]interface{}{"inbox": true},
					"subject":    "R53 plain control",
					"textBody":   []interface{}{map[string]interface{}{"partId": "p1"}},
					"bodyValues": map[string]interface{}{
						"p1": map[string]interface{}{"value": "Just a short plain body."},
					},
				},
			},
		},
		ID: "c3",
	}
	resp := server.handleEmailSet(user, call)
	created, _ := resp.Args["created"].(map[string]Email)
	e, ok := created["p1"]
	if !ok {
		t.Fatalf("CONTROL FAILED: created[p1] missing (notCreated=%v)", resp.Args["notCreated"])
	}
	if e.HasAttachment {
		t.Fatalf("CONTROL FAILED: plain-only created.HasAttachment = true")
	}
	if e.Preview != "Just a short plain body." {
		t.Fatalf("CONTROL FAILED: plain-only created.Preview = %q", e.Preview)
	}
}
