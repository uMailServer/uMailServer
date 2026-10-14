package jmap

// Round 49: RFC 8621 §4.6 Email/set draft creation. This file supersedes
// the round-39 refusal test (handlers_emailset_create_forbidden_test.go,
// deleted): the round-39 "forbidden" refusal was type-compliant but
// deliberately deferred real creation to the recorded round-48 plan, which
// this implementation follows. Pins: creation acceptance with synthesized
// RFC 5322 bytes (generated Message-ID/Date, draft-address leniency),
// the §4.6 constraint rejections, blobNotFound, the shared keyword
// machinery (rounds 43/45/47), and delivery-style threading.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const draftUser = "draft-user@example.com"

func draftCreate(t *testing.T, server *Server, record interface{}) Response {
	t.Helper()
	return server.handleEmailSet(draftUser, MethodCall{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId": draftUser,
			"create":    map[string]interface{}{"d1": record},
		},
		ID: "c1",
	})
}

func draftRecord() map[string]interface{} {
	return map[string]interface{}{
		"mailboxIds": map[string]interface{}{"inbox": true},
		"subject":    "Draft draft",
		"from":       []interface{}{map[string]interface{}{"name": "A", "email": "a@x.test"}},
		"to":         []interface{}{map[string]interface{}{"email": "not-an-address"}},
		"keywords":   map[string]interface{}{"$draft": true},
		"textBody":   []interface{}{map[string]interface{}{"partId": "p1", "type": "text/plain"}},
		"bodyValues": map[string]interface{}{
			"p1": map[string]interface{}{"value": "Hello draft"},
		},
	}
}

func TestEmailSetCreateDraftAcceptsAndStores(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(draftUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	resp := draftCreate(t, server, draftRecord())
	created, _ := resp.Args["created"].(map[string]Email)
	d1, ok := created["d1"]
	if !ok {
		t.Fatalf("FAIL: draft create refused; want created[d1] (RFC 8621 §4.6): %v", resp.Args)
	}
	if d1.BlobID == "" || d1.Size <= 0 {
		t.Fatalf("FAIL: created[d1] = %+v; want blobId and size", d1)
	}
	if !d1.Keywords["$draft"] {
		t.Fatalf("FAIL: created keywords = %v; want $draft", d1.Keywords)
	}
	if d1.ThreadID == "" {
		t.Fatalf("FAIL: created draft has no thread id (delivery-style threading expected)")
	}

	// Raw-bytes verification: the synthesized RFC 5322 message carries the
	// generated headers, the drafted text, and draft-address leniency.
	raw, err := msgStore.ReadMessage(draftUser, d1.BlobID)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	s := string(raw)
	for _, want := range []string{
		"Message-ID: <", "Date: ", "Subject: Draft draft",
		"From: A <a@x.test>", "To: not-an-address",
		"Content-Type: text/plain; charset=utf-8", "Hello draft",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("FAIL: synthesized message missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "\r\n") {
		t.Fatalf("FAIL: synthesized message must use CRLF line endings")
	}
}

func TestEmailSetCreateDraftGeneratedAndPreservedHeaders(t *testing.T) {
	server, db, msgStore, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(draftUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// Absent Message-ID → generated; sentAt → Date.
	record := draftRecord()
	record["sentAt"] = "2020-01-02T03:04:05Z"
	resp := draftCreate(t, server, record)
	created, _ := resp.Args["created"].(map[string]Email)
	d1, ok := created["d1"]
	if !ok {
		t.Fatalf("FAIL: create refused: %v", resp.Args)
	}
	raw, err := msgStore.ReadMessage(draftUser, d1.BlobID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Message-ID: <") {
		t.Fatalf("FAIL: Message-ID not generated:\n%s", raw)
	}
	wantDate := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC1123Z)
	if !strings.Contains(string(raw), "Date: "+wantDate) {
		t.Fatalf("FAIL: Date from sentAt = missing %q:\n%s", wantDate, raw)
	}

	// Given Message-ID → preserved (exactly once).
	record = draftRecord()
	record["messageId"] = []interface{}{"<custom@x.test>"}
	resp = draftCreate(t, server, record)
	created, _ = resp.Args["created"].(map[string]Email)
	d1, ok = created["d1"]
	if !ok {
		t.Fatalf("FAIL: create with messageId refused: %v", resp.Args)
	}
	raw, err = msgStore.ReadMessage(draftUser, d1.BlobID)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "<custom@x.test>"); got != 1 {
		t.Fatalf("FAIL: given Message-ID occurrences = %d, want 1:\n%s", got, raw)
	}
}

func TestEmailSetCreateDraftConstraintViolations(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(draftUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		mutate    func(r map[string]interface{})
		wantType  string
		wantProps []string
	}{
		{"headers given", func(r map[string]interface{}) { r["headers"] = map[string]interface{}{} }, "invalidProperties", []string{"headers"}},
		{"header:Form given", func(r map[string]interface{}) { r["header:X-Custom"] = "v" }, "invalidProperties", []string{"header:X-Custom"}},
		{"bodyStructure given", func(r map[string]interface{}) { r["bodyStructure"] = map[string]interface{}{} }, "invalidProperties", []string{"bodyStructure"}},
		{"two textBody parts", func(r map[string]interface{}) {
			r["textBody"] = []interface{}{
				map[string]interface{}{"partId": "p1", "type": "text/plain"},
				map[string]interface{}{"partId": "p2", "type": "text/plain"},
			}
			r["bodyValues"] = map[string]interface{}{
				"p1": map[string]interface{}{"value": "a"},
				"p2": map[string]interface{}{"value": "b"},
			}
		}, "invalidProperties", []string{"textBody"}},
		{"partId and blobId", func(r map[string]interface{}) {
			r["textBody"] = []interface{}{map[string]interface{}{"partId": "p1", "blobId": "b", "type": "text/plain"}}
		}, "invalidProperties", []string{"textBody"}},
		{"charset with partId", func(r map[string]interface{}) {
			r["textBody"] = []interface{}{map[string]interface{}{"partId": "p1", "type": "text/plain", "charset": "iso-8859-1"}}
		}, "invalidProperties", []string{"textBody"}},
		{"isTruncated true", func(r map[string]interface{}) {
			r["bodyValues"] = map[string]interface{}{"p1": map[string]interface{}{"value": "x", "isTruncated": true}}
		}, "invalidProperties", []string{"bodyValues"}},
		{"missing mailboxIds", func(r map[string]interface{}) { delete(r, "mailboxIds") }, "invalidProperties", []string{"mailboxIds"}},
		{"unknown mailbox id", func(r map[string]interface{}) { r["mailboxIds"] = map[string]interface{}{"does-not-exist": true} }, "mailboxNotFound", nil},
		{"blob not found", func(r map[string]interface{}) {
			delete(r, "bodyValues")
			r["textBody"] = []interface{}{map[string]interface{}{"blobId": "missing-blob", "type": "text/plain"}}
		}, "blobNotFound", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := draftRecord()
			tc.mutate(record)
			resp := draftCreate(t, server, record)
			nc, _ := resp.Args["notCreated"].(map[string]interface{})
			e, _ := nc["d1"].(map[string]interface{})
			if e == nil {
				t.Fatalf("FAIL: %s accepted; want notCreated", tc.name)
			}
			if e["type"] != tc.wantType {
				t.Fatalf("FAIL: %s: type = %v, want %q", tc.name, e["type"], tc.wantType)
			}
			if tc.wantProps != nil {
				props, _ := e["properties"].([]string)
				if len(props) != 1 || props[0] != tc.wantProps[0] {
					t.Fatalf("FAIL: %s: properties = %v, want %v", tc.name, props, tc.wantProps)
				}
			}
			if tc.wantType == "blobNotFound" {
				nf, _ := e["notFound"].([]string)
				if len(nf) != 1 || nf[0] != "missing-blob" {
					t.Fatalf("FAIL: blobNotFound notFound = %v, want [missing-blob]", nf)
				}
			}
		})
	}

	// A create value that is not an object (§4.1.2) → invalidProperties.
	resp := draftCreate(t, server, "not-an-object")
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	e, _ := nc["d1"].(map[string]interface{})
	if e == nil || e["type"] != "invalidProperties" {
		t.Fatalf("FAIL: non-object create value: %v", resp.Args)
	}
}

func TestEmailSetCreateDraftKeywordMachinery(t *testing.T) {
	server, db, _, cleanup := setupTestServer(t)
	defer cleanup()
	if err := db.CreateMailbox(draftUser, "INBOX"); err != nil {
		t.Fatal(err)
	}

	// Round-47 cap via the shared gate: 101 keywords → tooManyKeywords.
	over := draftRecord()
	overKw := map[string]interface{}{}
	for i := 1; i <= maxJMAPKeywordsPerEmail+1; i++ {
		overKw[fmt.Sprintf("$k%d", i)] = true
	}
	over["keywords"] = overKw
	resp := draftCreate(t, server, over)
	nc, _ := resp.Args["notCreated"].(map[string]interface{})
	e, _ := nc["d1"].(map[string]interface{})
	if e == nil || e["type"] != "tooManyKeywords" {
		t.Fatalf("FAIL: over-cap create reported %v; want notCreated \"tooManyKeywords\"", resp.Args)
	}

	// Rounds 43/45 machinery: mixed-case arbitrary keyword stored lowercase.
	mixed := draftRecord()
	mixed["keywords"] = map[string]interface{}{"$Label1": true}
	resp = draftCreate(t, server, mixed)
	created, _ := resp.Args["created"].(map[string]Email)
	d1, ok := created["d1"]
	if !ok {
		t.Fatalf("FAIL: mixed-case keyword create refused: %v", resp.Args)
	}
	if !d1.Keywords["$label1"] {
		t.Fatalf("FAIL: mixed-case keyword = %v; want lowercase $label1", d1.Keywords)
	}
}
