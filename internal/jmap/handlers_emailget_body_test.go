package jmap

// Round 50: RFC 8621 §4.2 — the FULL form of Email/get (properties omitted)
// must carry body properties (textBody/htmlBody/bodyValues), same as the
// projected form. Before this round, handleEmailGet returned the bare
// storageToJMAPEmail struct with no body properties when `properties` was
// omitted; the projected path already augmented via augmentEmailProps.
// Also pins the fetch*BodyValues / maxBodyValueBytes controls on the
// projected path (§4.2).
//
// In-package typing notes: the full-form list is []Email (fields, not a JSON
// map) and the projected bodyValues are map[string]EmailBodyValue.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

const bodyGetUser = "bodyget-user@example.com"

const bodyGetMIME = "From: a@x.test\r\nTo: b@y.test\r\nSubject: BodyGet\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"BG\"\r\n\r\n--BG\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHello plain bodyget\r\n--BG\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello html bodyget</p>\r\n--BG--\r\n"

func bodyGetSetup(t *testing.T) (*Server, *storage.MessageStore, string, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox(bodyGetUser, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(bodyGetUser, []byte(bodyGetMIME))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(bodyGetUser, MethodCall{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId": bodyGetUser,
			"emails": map[string]interface{}{
				"i1": map[string]interface{}{
					"blobId":     blobID,
					"mailboxIds": map[string]interface{}{"inbox": true},
				},
			},
		},
		ID: "c-import",
	})
	created, _ := resp.Args["created"].(map[string]Email)
	i1, ok := created["i1"]
	if !ok {
		t.Fatalf("INVALID setup: import failed: %v", resp.Args)
	}
	return server, msgStore, i1.ID, cleanup
}

func TestEmailGetFullFormIncludesBodyProperties(t *testing.T) {
	server, _, id, cleanup := bodyGetSetup(t)
	defer cleanup()

	// Full form: properties omitted ⇒ all properties (§4.2). The list is
	// []Email (Go-typed fields, not a JSON map).
	resp := server.handleEmailGet(bodyGetUser, MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": bodyGetUser,
			"ids":       []interface{}{id},
		},
		ID: "c1",
	})
	list, ok := resp.Args["list"].([]Email)
	if !ok || len(list) != 1 {
		t.Fatalf("FAIL: list = %v", resp.Args)
	}
	e := list[0]
	if len(e.TextBody) == 0 || len(e.HTMLBody) == 0 {
		t.Fatalf("FAIL: full-form Email/get omitted body properties: textBody=%v htmlBody=%v", e.TextBody, e.HTMLBody)
	}
	if e.TextBody[0].Type != "text/plain" {
		t.Fatalf("FAIL: textBody[0].Type = %q; want text/plain", e.TextBody[0].Type)
	}
	if e.TextBody[0].PartID == "" {
		t.Fatalf("FAIL: textBody[0] has no server-assigned partId: %+v", e.TextBody[0])
	}
}

func TestEmailGetProjectedBodyValuesControl(t *testing.T) {
	server, _, id, cleanup := bodyGetSetup(t)
	defer cleanup()

	// Projected form: fetch flags govern which bodyValues are populated
	// (§4.2). In-package the values are map[string]EmailBodyValue.
	resp := server.handleEmailGet(bodyGetUser, MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId":           bodyGetUser,
			"ids":                 []interface{}{id},
			"properties":          []interface{}{"textBody", "bodyValues"},
			"fetchTextBodyValues": true,
			"fetchHTMLBodyValues": true,
			"maxBodyValueBytes":   float64(1000000),
		},
		ID: "c1",
	})
	list, ok := resp.Args["list"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("FAIL: list = %v", resp.Args)
	}
	obj, _ := list[0].(map[string]interface{})
	bv, ok := obj["bodyValues"].(map[string]EmailBodyValue)
	if !ok || len(bv) == 0 {
		t.Fatalf("FAIL: bodyValues = %#v; want populated values", obj["bodyValues"])
	}
	found := map[string]bool{}
	for _, entry := range bv {
		found[entry.Value] = true
	}
	if !found["Hello plain bodyget"] || !found["<p>Hello html bodyget</p>"] {
		t.Fatalf("FAIL: bodyValues = %#v; want both part values", bv)
	}
}

func TestEmailGetBodyValuesTruncation(t *testing.T) {
	server, _, id, cleanup := bodyGetSetup(t)
	defer cleanup()

	resp := server.handleEmailGet(bodyGetUser, MethodCall{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId":           bodyGetUser,
			"ids":                 []interface{}{id},
			"properties":          []interface{}{"bodyValues"},
			"fetchTextBodyValues": true,
			"maxBodyValueBytes":   float64(5),
		},
		ID: "c1",
	})
	list, ok := resp.Args["list"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("FAIL: list = %v", resp.Args)
	}
	obj, _ := list[0].(map[string]interface{})
	bv, ok := obj["bodyValues"].(map[string]EmailBodyValue)
	if !ok || len(bv) == 0 {
		t.Fatalf("FAIL: bodyValues = %#v; want populated values", obj["bodyValues"])
	}
	for _, entry := range bv {
		if len(entry.Value) > 5 {
			t.Fatalf("FAIL: value not truncated to 5 bytes: %q", entry.Value)
		}
		if !entry.IsTruncated {
			t.Fatalf("FAIL: isTruncated = false; want true for %q", entry.Value)
		}
	}
}
