package api

import (
	"encoding/json"
	"github.com/umailserver/umailserver/internal/storage"
	"testing"
	"time"
)

// F6760: GET /mail/message must return cc/bcc of a draft so the composer can restore them.
func TestR199DraftMessageReturnsCcBcc(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	user := "a@x.com"
	raw := "From: a@x.com\r\nTo: t@x.com\r\nCc: c1@x.com, c2@x.com\r\nBcc: b@x.com\r\nSubject: d\r\n\r\nbody\r\n"
	id, err := ms.StoreMessage(user, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_ = mdb.CreateMailbox(user, "Drafts")
	uid, _ := mdb.GetNextUID(user, "Drafts")
	if err := mdb.StoreMessageMetadata(user, "Drafts", uid, &storage.MessageMetadata{MessageID: id, UID: uid, InternalDate: time.Now(), Size: 10, Subject: "d", From: user, To: "t@x.com"}); err != nil {
		t.Fatal(err)
	}
	rec := r172Do(h.handleMailMessage, "GET", "/api/v1/mail/message?folder=drafts&id="+id, user, "")
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var m struct{ CC, BCC []string }
	var raw2 map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw2)
	_ = json.Unmarshal(raw2["cc"], &m.CC)
	_ = json.Unmarshal(raw2["bcc"], &m.BCC)
	if len(m.CC) != 2 || m.CC[0] != "c1@x.com" || m.CC[1] != "c2@x.com" || len(m.BCC) != 1 || m.BCC[0] != "b@x.com" {
		t.Fatalf("cc=%v bcc=%v", m.CC, m.BCC)
	}
	// Control: non-draft folders never expose Bcc.
	id2 := r143Deliver(t, mdb, ms, user, "INBOX", "x", nil)
	rec = r172Do(h.handleMailMessage, "GET", "/api/v1/mail/message?folder=inbox&id="+id2, user, "")
	var r3 map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &r3)
	if _, has := r3["bcc"]; has {
		t.Fatal("bcc leaked outside drafts")
	}
}
