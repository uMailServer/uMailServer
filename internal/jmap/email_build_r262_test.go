package jmap

import (
	"bytes"
	"net/mail"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func r262Store(t *testing.T) *storage.MessageStore {
	t.Helper()
	ms, err := storage.NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// F7381: a create with no body parts (a subject-only draft) builds an empty
// text/plain message instead of failing with blobNotFound for blob "".
func TestEmailBuildWithoutBodyParts(t *testing.T) {
	ms := r262Store(t)
	raw, berr := buildRFC5322Email("u", map[string]interface{}{"subject": "draft"}, ms)
	if berr != nil {
		t.Fatalf("draft without body rejected: %s %v", berr.setErrType, berr.description)
	}
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil || m.Header.Get("Subject") != "draft" {
		t.Fatalf("parse: %v", err)
	}
	// with attachments the empty main part still precedes them
	bv := map[string]interface{}{"1": map[string]interface{}{"value": "att"}}
	_, berr = buildRFC5322Email("u", map[string]interface{}{
		"bodyValues":  bv,
		"attachments": []interface{}{map[string]interface{}{"partId": "1", "type": "text/plain"}},
	}, ms)
	if berr != nil {
		t.Fatalf("attachments without text body rejected: %v", berr.description)
	}
}

// F7382: CR/LF in any header-bound property must be rejected, not emitted.
func TestEmailBuildRejectsLineBreaks(t *testing.T) {
	ms := r262Store(t)
	bv := map[string]interface{}{"1": map[string]interface{}{"value": "hi"}}
	with := func(k string, v interface{}) map[string]interface{} {
		return map[string]interface{}{k: v, "textBody": []interface{}{map[string]interface{}{"partId": "1"}}, "bodyValues": bv}
	}
	att := func(k, v string) map[string]interface{} {
		m := with("subject", "ok")
		m["attachments"] = []interface{}{map[string]interface{}{"partId": "1", "type": "text/plain", k: v}}
		return m
	}
	addr := func(k, email, name string) map[string]interface{} {
		return with(k, []interface{}{map[string]interface{}{"email": email, "name": name}})
	}
	for _, bad := range []string{"x\r\nBcc: v@evil.example", "x\nBcc: v@evil.example", "x\rBcc: v@evil.example"} {
		cases := map[string]map[string]interface{}{
			"subject":     with("subject", bad),
			"messageId":   with("messageId", []interface{}{bad}),
			"inReplyTo":   with("inReplyTo", []interface{}{bad}),
			"references":  with("references", []interface{}{bad}),
			"to.email":    addr("to", bad, ""),
			"from.name":   addr("from", "a@b.example", bad),
			"att.cid":     att("cid", bad),
			"att.name":    att("name", bad),
			"att.charset": att("charset", bad),
			"att.disp":    att("disposition", bad),
		}
		for name, c := range cases {
			raw, berr := buildRFC5322Email("u", c, ms)
			if berr == nil || berr.setErrType != "invalidProperties" {
				t.Errorf("%s %q: err=%v, want invalidProperties (raw has Bcc: %v)", name, bad, berr, bytes.Contains(raw, []byte("Bcc:")))
			}
		}
	}
	// control: ordinary values (including a tab) are still accepted
	ok := addr("to", "a@b.example", "A\tB")
	ok["subject"] = "hello world"
	if _, berr := buildRFC5322Email("u", ok, ms); berr != nil {
		t.Fatalf("clean values rejected: %v", berr.description)
	}
}
