package jmap

import (
	"reflect"
	"testing"
)

const r294User = "hdr-user@example.com"

const r294Full = "From: a@x.test\r\nTo: b@y.test\r\nSubject: Parsed headers\r\nMessage-ID: <m1@x.test>\r\n" +
	"In-Reply-To: <p0@x.test>\r\nReferences: <p-1@x.test> <p0@x.test>\r\nCc: c@y.test\r\n" +
	"Reply-To: r@y.test\r\nSender: s@x.test\r\nDate: Wed, 14 Oct 2026 21:40:00 +0300\r\n\r\nbody\r\n"

const r294Bare = "From: a@x.test\r\nSubject: Bare\r\n\r\nbody\r\n"

func r294Import(t *testing.T, raw string) (*Server, string, func()) {
	t.Helper()
	server, db, msgStore, cleanup := setupTestServer(t)
	if err := db.CreateMailbox(r294User, "INBOX"); err != nil {
		t.Fatal(err)
	}
	blobID, err := msgStore.StoreMessage(r294User, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp := server.handleEmailImport(r294User, MethodCall{Name: "Email/import", ID: "c", Args: map[string]interface{}{
		"accountId": r294User,
		"emails":    map[string]interface{}{"i1": map[string]interface{}{"blobId": blobID, "mailboxIds": map[string]interface{}{"inbox": true}}},
	}})
	created, _ := resp.Args["created"].(map[string]Email)
	i1, ok := created["i1"]
	if !ok {
		t.Fatalf("import failed: %v", resp.Args)
	}
	return server, i1.ID, cleanup
}

func r294Get(t *testing.T, server *Server, id string, props []interface{}) interface{} {
	t.Helper()
	args := map[string]interface{}{"accountId": r294User, "ids": []interface{}{id}}
	if props != nil {
		args["properties"] = props
	}
	resp := server.handleEmailGet(r294User, MethodCall{Name: "Email/get", ID: "g", Args: args})
	switch list := resp.Args["list"].(type) {
	case []interface{}:
		if len(list) == 1 {
			return list[0]
		}
	case []Email:
		if len(list) == 1 {
			return list[0]
		}
	}
	t.Fatalf("unexpected Email/get response: %v", resp.Args)
	return nil
}

// F7680: the RFC 8621 §4.1.2 parsed-header properties come from the message
// headers (projected form).
func TestEmailGetParsedHeaderPropertiesProjected(t *testing.T) {
	server, id, cleanup := r294Import(t, r294Full)
	defer cleanup()
	got := r294Get(t, server, id, []interface{}{"messageId", "inReplyTo", "references", "sender", "replyTo", "cc", "bcc", "sentAt", "header:Message-ID:asMessageIds"}).(map[string]interface{})
	want := map[string]interface{}{
		"messageId":  []string{"m1@x.test"},
		"inReplyTo":  []string{"p0@x.test"},
		"references": []string{"p-1@x.test", "p0@x.test"},
		"sender":     []EmailAddress{{Email: "s@x.test"}},
		"replyTo":    []EmailAddress{{Email: "r@y.test"}},
		"cc":         []EmailAddress{{Email: "c@y.test"}},
		"bcc":        nil, // no Bcc header
		"sentAt":     "2026-10-14T21:40:00+03:00",
		// control: the explicit header form is unchanged
		"header:Message-ID:asMessageIds": []string{"m1@x.test"},
	}
	for k, w := range want {
		if !reflect.DeepEqual(got[k], w) {
			t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
}

// The full form (no properties argument) carries them too.
func TestEmailGetParsedHeaderPropertiesFullForm(t *testing.T) {
	server, id, cleanup := r294Import(t, r294Full)
	defer cleanup()
	e := r294Get(t, server, id, nil).(Email)
	if !reflect.DeepEqual(e.MessageID, []string{"m1@x.test"}) || !reflect.DeepEqual(e.InReplyTo, []string{"p0@x.test"}) ||
		!reflect.DeepEqual(e.References, []string{"p-1@x.test", "p0@x.test"}) || e.SentAt != "2026-10-14T21:40:00+03:00" ||
		len(e.CC) != 1 || e.CC[0].Email != "c@y.test" || len(e.ReplyTo) != 1 || len(e.Sender) != 1 {
		t.Fatalf("full form: %+v", e)
	}
}

// A message without those headers reports null, and a message without a To
// header reports no recipient instead of an empty address object.
func TestEmailGetMissingHeadersAreNull(t *testing.T) {
	server, id, cleanup := r294Import(t, r294Bare)
	defer cleanup()
	got := r294Get(t, server, id, []interface{}{"messageId", "inReplyTo", "references", "sender", "replyTo", "cc", "sentAt", "to", "from"}).(map[string]interface{})
	for _, k := range []string{"messageId", "inReplyTo", "references", "sender", "replyTo", "cc", "sentAt", "to"} {
		if got[k] != nil {
			t.Errorf("%s = %#v, want null", k, got[k])
		}
	}
	if from, _ := got["from"].([]interface{}); len(from) != 1 {
		t.Errorf("from = %#v, want the From address kept", got["from"])
	}
	// control: a message that has a To header still reports it
	server2, id2, cleanup2 := r294Import(t, r294Full)
	defer cleanup2()
	got2 := r294Get(t, server2, id2, []interface{}{"to"}).(map[string]interface{})
	if to, _ := got2["to"].([]interface{}); len(to) != 1 {
		t.Errorf("to = %#v, want the To address kept", got2["to"])
	}
}
