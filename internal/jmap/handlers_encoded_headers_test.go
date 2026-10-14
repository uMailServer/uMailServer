package jmap

import (
	"encoding/json"
	"testing"
)

const r298User = "enc-user@example.com"

const r298Msg = "From: =?UTF-8?B?QXnFn2UgWcSxbG1heg==?= <ayse@x.test>\r\n" +
	"To: \"Smoke, Admin\" <admin@x.test>, Other <other@x.test>\r\n" +
	"Subject: =?UTF-8?B?SG/Fn2dlbGRpbml6IOKAkyBUw7xya2nDp2Ugb2xhcmFr?=\r\n" +
	"Message-ID: <enc1@x.test>\r\n\r\nbody\r\n"

func r298Import(t *testing.T, raw string) (*Server, string, func()) {
	t.Helper()
	server, db, ms, cleanup := setupTestServer(t)
	if err := db.CreateMailbox(r298User, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blob, err := ms.StoreMessage(r298User, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(r298User, MethodCall{Name: "Email/import", ID: "c", Args: map[string]interface{}{
		"accountId": r298User,
		"emails":    map[string]interface{}{"i": map[string]interface{}{"blobId": blob, "mailboxIds": map[string]interface{}{"inbox": true}}},
	}})
	created, _ := resp.Args["created"].(map[string]Email)
	if created["i"].ID == "" {
		t.Fatalf("import failed: %v", resp.Args)
	}
	return server, created["i"].ID, cleanup
}

func r298Norm(v interface{}) string {
	b, _ := json.Marshal(v)
	var x interface{}
	_ = json.Unmarshal(b, &x)
	b, _ = json.Marshal(x)
	return string(b)
}

func r298Get(server *Server, id string, props ...interface{}) map[string]interface{} {
	resp := server.handleEmailGet(r298User, MethodCall{Name: "Email/get", ID: "g", Args: map[string]interface{}{
		"accountId": r298User, "ids": []interface{}{id}, "properties": props,
	}})
	return resp.Args["list"].([]interface{})[0].(map[string]interface{})
}

func r298Query(server *Server, filter map[string]interface{}) int {
	resp := server.handleEmailQuery(r298User, MethodCall{Name: "Email/query", ID: "q", Args: map[string]interface{}{
		"accountId": r298User, "filter": filter,
	}})
	ids, _ := resp.Args["ids"].([]string)
	return len(ids)
}

// F7720: subject is the header as text; from/to are parsed address objects with
// decoded names, matching the explicit header:*:as* forms.
func TestEmailGetDecodesSubjectAndParsesAddresses(t *testing.T) {
	server, id, cleanup := r298Import(t, r298Msg)
	defer cleanup()
	got := r298Get(server, id, "subject", "from", "to", "header:Subject:asText", "header:From:asAddresses", "header:To:asAddresses")
	if got["subject"] != "Hoşgeldiniz – Türkiçe olarak" || got["subject"] != got["header:Subject:asText"] {
		t.Errorf("subject = %q (header form %q)", got["subject"], got["header:Subject:asText"])
	}
	if r298Norm(got["from"]) != r298Norm(got["header:From:asAddresses"]) {
		t.Errorf("from %s != header form %s", r298Norm(got["from"]), r298Norm(got["header:From:asAddresses"]))
	}
	if r298Norm(got["to"]) != r298Norm(got["header:To:asAddresses"]) {
		t.Errorf("to %s != header form %s", r298Norm(got["to"]), r298Norm(got["header:To:asAddresses"]))
	}
}

// The query filters compare against the text people see, not the encoded form.
func TestEmailQueryFiltersMatchDecodedHeaders(t *testing.T) {
	server, _, cleanup := r298Import(t, r298Msg)
	defer cleanup()
	for name, filter := range map[string]map[string]interface{}{
		"subject": {"subject": "hoşgeldiniz"},
		"text":    {"text": "olarak"},
		"from":    {"from": "Ayşe"},
		"to":      {"to": "Smoke, Admin"},
	} {
		if n := r298Query(server, filter); n != 1 {
			t.Errorf("%s filter %v: %d hits, want 1", name, filter, n)
		}
	}
	// control: a non-matching term finds nothing
	if n := r298Query(server, map[string]interface{}{"subject": "kesinlikle yok"}); n != 0 {
		t.Errorf("non-matching subject filter: %d hits", n)
	}
}

// An address header that does not parse is still reported (as raw text), and an
// ASCII-only message is unchanged.
func TestEmailGetUnparseableAddressKeepsRawText(t *testing.T) {
	server, id, cleanup := r298Import(t, "From: not an address <<\r\nTo: plain@x.test\r\nSubject: plain subject\r\n\r\nbody\r\n")
	defer cleanup()
	got := r298Get(server, id, "subject", "from", "to")
	if got["subject"] != "plain subject" {
		t.Errorf("subject = %q", got["subject"])
	}
	if r298Norm(got["from"]) != r298Norm([]EmailAddress{{Email: "not an address <<"}}) {
		t.Errorf("unparseable from = %s", r298Norm(got["from"]))
	}
	if r298Norm(got["to"]) != r298Norm([]EmailAddress{{Email: "plain@x.test"}}) {
		t.Errorf("to = %s", r298Norm(got["to"]))
	}
}
