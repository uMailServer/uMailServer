package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const (
	encSubject = "=?UTF-8?B?SG/Fn2dlbGRpbml6IOKAkyBUw7xya2nDp2Ugb2xhcmFr?="
	encFrom    = "=?UTF-8?B?QXnFn2UgWcSxbG1heg==?= <ayse@x.test>"
	encTo      = `"Smoke, Admin" <admin@x.test>, Other <other@x.test>`

	decSubject = "Hoşgeldiniz – Türkiçe olarak"
	decFrom    = "Ayşe Yılmaz <ayse@x.test>"
)

var decTo = []string{`"Smoke, Admin" <admin@x.test>`, "Other <other@x.test>"}

// F7721: the REST mail list and detail show the headers as text: RFC 2047
// words decoded, and the To list parsed so a quoted name keeps its comma.
func TestMailListAndDetailDecodeEncodedHeaders(t *testing.T) {
	h, db, id := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	// control: an ordinary ASCII header passes through unchanged
	if l, _ := h.getEmailsFromStorage("reader@example.com", "INBOX"); len(l) != 1 || l[0].Subject != "ordinary subject" {
		t.Fatalf("control: %+v", l)
	}
	meta := &storage.MessageMetadata{UID: 1, MessageID: id, InternalDate: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Subject: encSubject, From: encFrom, To: encTo}
	if err := db.StoreMessageMetadata("reader@example.com", "INBOX", 1, meta); err != nil {
		t.Fatal(err)
	}
	list, _ := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if list[0].Subject != decSubject || list[0].From != decFrom || !reflect.DeepEqual(list[0].To, decTo) {
		t.Errorf("list: subject=%q from=%q to=%q", list[0].Subject, list[0].From, list[0].To)
	}
	rec := httptest.NewRecorder()
	h.handleMailMessage(rec, mailMutationRequest(http.MethodGet, "/api/v1/mail/message?id="+id+"&folder=INBOX"))
	var detail struct {
		Subject, From string
		To            []string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Subject != decSubject || detail.From != decFrom || !reflect.DeepEqual(detail.To, decTo) {
		t.Errorf("detail: %+v", detail)
	}
}

// A message with no To header lists an empty array (not [""] and not null).
func TestMailListEmptyToIsEmptyArray(t *testing.T) {
	h, _, _ := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	list, _ := h.getEmailsFromStorage("reader@example.com", "INBOX")
	b, _ := json.Marshal(list[0].To)
	if string(b) != "[]" {
		t.Fatalf("To = %s, want []", b)
	}
}

// Thread views decode the same headers.
func TestThreadViewsDecodeEncodedHeaders(t *testing.T) {
	server, db, user, threadID := startThreadsTestServer(t)
	if err := db.StoreMessageMetadata(user, "INBOX", 1, &storage.MessageMetadata{UID: 1, MessageID: "m1", ThreadID: threadID,
		Subject: encSubject, From: encFrom, To: encTo, Flags: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateThread(user, &storage.Thread{ThreadID: threadID, Subject: encSubject, Participants: []string{encFrom}, MessageCount: 1}); err != nil {
		t.Fatal(err)
	}
	get := func(target string, h func(http.ResponseWriter, *http.Request)) string {
		r := httptest.NewRequest(http.MethodGet, target, nil).WithContext(context.WithValue(context.Background(), "user", user))
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	list := get("/api/v1/threads", server.handleThreads)
	if !strings.Contains(list, decSubject) || !strings.Contains(list, "Ayşe Yılmaz") || strings.Contains(list, "=?UTF-8?") {
		t.Errorf("thread list not decoded: %s", list)
	}
	detail := get("/api/v1/threads/"+threadID, server.handleThreadPath)
	if !strings.Contains(detail, decSubject) || !strings.Contains(detail, "Ayşe") || strings.Contains(detail, "=?UTF-8?") {
		t.Errorf("thread detail not decoded: %s", detail)
	}
}

// Review follow-ups: a display name in a charset we cannot convert must not
// collapse the recipient list, and a name must not be able to carry a second
// <address> that a client could take for the real one.
func TestMailListToSurvivesUnsupportedCharsetAndSmuggledAddress(t *testing.T) {
	h, db, id := mailConversionFixture(t, "From: x@y.test\r\nSubject: s\r\n\r\nbody\r\n", nil)
	meta := &storage.MessageMetadata{UID: 1, MessageID: id, InternalDate: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Subject: "s", From: "=?utf-8?B?RXZlIDx2aWN0aW1AeC50ZXN0Pg==?= <attacker@evil.test>",
		To: "=?gb2312?B?1tDOxA==?= <a@x.test>, Bob <b@x.test>"}
	if err := db.StoreMessageMetadata("reader@example.com", "INBOX", 1, meta); err != nil {
		t.Fatal(err)
	}
	list, _ := h.getEmailsFromStorage("reader@example.com", "INBOX")
	if len(list[0].To) != 2 || !strings.HasSuffix(list[0].To[0], "<a@x.test>") || list[0].To[1] != "Bob <b@x.test>" {
		t.Errorf("To = %q, want the two recipients", list[0].To)
	}
	if strings.Count(list[0].From, "<") != 1 || !strings.HasSuffix(list[0].From, "<attacker@evil.test>") {
		t.Errorf("From = %q carries a second <address>", list[0].From)
	}
}
