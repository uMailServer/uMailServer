package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func r172Do(h http.HandlerFunc, method, url, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func r172Flags(mdb *storage.Database, user, mb, id string) []string {
	_, meta, ok := (&MailHandler{mailDB: mdb}).findUIDByMessageID(user, mb, id)
	if !ok {
		return nil
	}
	return meta.Flags
}

func TestR172MailMessageGet(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	id := r143Deliver(t, mdb, ms, "a@x.com", "Archive", "hello", nil)
	other := r143Deliver(t, mdb, ms, "b@x.com", "INBOX", "secret", nil)

	rec := r172Do(h.handleMailMessage, "GET", "/api/v1/mail/message?id="+id, "a@x.com", "")
	if rec.Code != 200 {
		t.Fatalf("get without folder: %d %s", rec.Code, rec.Body)
	}
	var m Mail
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if m.ID != id || m.Folder != "Archive" || m.Subject != "hello" || m.Read {
		t.Fatalf("bad mail %+v (GET must not mark read)", m)
	}
	// IDOR: another user's id is 404 for both lookup modes.
	for _, u := range []string{"/api/v1/mail/message?id=" + other, "/api/v1/mail/message?folder=inbox&id=" + other} {
		if rec := r172Do(h.handleMailMessage, "GET", u, "a@x.com", ""); rec.Code != 404 {
			t.Errorf("%s: %d want 404", u, rec.Code)
		}
	}
	if rec := r172Do(h.handleMailMessage, "GET", "/api/v1/mail/message", "a@x.com", ""); rec.Code != 400 {
		t.Errorf("missing id: %d", rec.Code)
	}
	if rec := r172Do(h.handleMailMessage, "GET", "/api/v1/mail/message?id="+id, "", ""); rec.Code != 401 {
		t.Errorf("unauth: %d", rec.Code)
	}
	if rec := r172Do(h.handleMailMessage, "POST", "/api/v1/mail/message?id="+id, "a@x.com", ""); rec.Code != 405 {
		t.Errorf("method: %d", rec.Code)
	}
}

func TestR172MailFlags(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	id := r143Deliver(t, mdb, ms, "a@x.com", "INBOX", "one", []string{"\\Flagged"})
	id2 := r143Deliver(t, mdb, ms, "a@x.com", "INBOX", "two", nil)
	other := r143Deliver(t, mdb, ms, "b@x.com", "INBOX", "secret", nil)

	rec := r172Do(h.handleMailFlags, "POST", "/f", "a@x.com", fmt.Sprintf(`{"ids":[%q,%q],"add":["\\Seen","answered"],"remove":["\\Flagged"]}`, id, id2))
	if rec.Code != 200 {
		t.Fatalf("flags: %d %s", rec.Code, rec.Body)
	}
	if f := r172Flags(mdb, "a@x.com", "INBOX", id); len(f) != 2 || !hasFlag(f, "\\Seen") || !hasFlag(f, "\\Answered") || hasFlag(f, "\\Flagged") {
		t.Errorf("flags after: %v", f)
	}
	// IDOR: cannot touch another user's message; nothing partially applied.
	rec = r172Do(h.handleMailFlags, "POST", "/f", "a@x.com", fmt.Sprintf(`{"ids":[%q,%q],"add":["\\Draft"]}`, id2, other))
	if rec.Code != 404 {
		t.Errorf("foreign id: %d", rec.Code)
	}
	if hasFlag(r172Flags(mdb, "a@x.com", "INBOX", id2), "\\Draft") || hasFlag(r172Flags(mdb, "b@x.com", "INBOX", other), "\\Draft") {
		t.Error("flags applied despite 404")
	}
	for name, body := range map[string]string{
		"unknown flag": fmt.Sprintf(`{"id":%q,"add":["\\Deleted"]}`, id),
		"custom flag":  fmt.Sprintf(`{"id":%q,"add":["$Phishing"]}`, id),
		"no flags":     fmt.Sprintf(`{"id":%q}`, id),
		"conflict":     fmt.Sprintf(`{"id":%q,"add":["\\Seen"],"remove":["seen"]}`, id),
		"no id":        `{"add":["\\Seen"]}`,
		"unknown key":  fmt.Sprintf(`{"id":%q,"add":["\\Seen"],"x":1}`, id),
		"bad folder":   fmt.Sprintf(`{"id":%q,"folder":"a\u0000b","add":["\\Seen"]}`, id),
		"bad json":     `{`,
	} {
		if rec := r172Do(h.handleMailFlags, "POST", "/f", "a@x.com", body); rec.Code != 400 {
			t.Errorf("%s: %d want 400", name, rec.Code)
		}
	}
	if rec := r172Do(h.handleMailFlags, "POST", "/f", "", `{}`); rec.Code != 401 {
		t.Errorf("unauth %d", rec.Code)
	}
	if rec := r172Do(h.handleMailFlags, "GET", "/f", "a@x.com", ""); rec.Code != 405 {
		t.Errorf("method %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/f", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), "user", "a@x.com"))
	req.Header.Set("Content-Type", "text/plain")
	r := httptest.NewRecorder()
	h.handleMailFlags(r, req)
	if r.Code != 415 {
		t.Errorf("content type: %d", r.Code)
	}
}

func TestR172MailFlagsConcurrentNoGhost(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	id := r143Deliver(t, mdb, ms, "a@x.com", "INBOX", "one", nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			flag := []string{"\\Seen", "\\Flagged", "\\Answered", "\\Draft"}[i%4]
			r172Do(h.handleMailFlags, "POST", "/f", "a@x.com", fmt.Sprintf(`{"id":%q,"add":[%q]}`, id, flag))
		}(i)
	}
	wg.Wait()
	if f := r172Flags(mdb, "a@x.com", "INBOX", id); len(f) != 4 {
		t.Errorf("flags %v", f)
	}
}

func TestR172MailDraft(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	rec := r172Do(h.handleMailDraft, "POST", "/d", "a@x.com", `{"to":["c@x.com"],"subject":"S","body":"v1"}`)
	if rec.Code != 200 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var out struct{ ID, Folder string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID == "" || out.Folder != "Drafts" {
		t.Fatalf("resp %s", rec.Body)
	}
	if f := r172Flags(mdb, "a@x.com", "Drafts", out.ID); !hasFlag(f, "\\Draft") {
		t.Errorf("flags %v", f)
	}
	// Update replaces the draft (new id, old gone).
	rec = r172Do(h.handleMailDraft, "PUT", "/d", "a@x.com", fmt.Sprintf(`{"id":%q,"to":["c@x.com"],"subject":"S","body":"v2"}`, out.ID))
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	var out2 struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out2)
	if out2.ID == out.ID || r143Count(mdb, "a@x.com", "Drafts") != 1 {
		t.Fatalf("not replaced: %s count=%d", rec.Body, r143Count(mdb, "a@x.com", "Drafts"))
	}
	if ms.MessageExists("a@x.com", out.ID) {
		t.Error("old draft blob not removed")
	}
	data, _ := ms.ReadMessage("a@x.com", out2.ID)
	if !strings.Contains(string(data), "v2") {
		t.Errorf("body %q", data)
	}
	// Same content again: stable id, still one draft.
	rec = r172Do(h.handleMailDraft, "PUT", "/d", "a@x.com", fmt.Sprintf(`{"id":%q,"to":["c@x.com"],"subject":"S","body":"v2"}`, out2.ID))
	if rec.Code != 200 || r143Count(mdb, "a@x.com", "Drafts") != 1 {
		t.Errorf("idempotent: %d", rec.Code)
	}
	// IDOR: another user's draft id (and a non-draft id) is 404 and untouched.
	foreign := r143Deliver(t, mdb, ms, "b@x.com", "Drafts", "bdraft", nil)
	inbox := r143Deliver(t, mdb, ms, "a@x.com", "INBOX", "mail", nil)
	for _, id := range []string{foreign, inbox, "nope"} {
		rec = r172Do(h.handleMailDraft, "PUT", "/d", "a@x.com", fmt.Sprintf(`{"id":%q,"body":"x"}`, id))
		if rec.Code != 404 {
			t.Errorf("id %s: %d want 404", id, rec.Code)
		}
	}
	if r143Count(mdb, "b@x.com", "Drafts") != 1 || !ms.MessageExists("b@x.com", foreign) {
		t.Error("foreign draft modified")
	}
	// Validation.
	for name, body := range map[string]string{
		"bad rcpt":   `{"to":["a b"]}`,
		"crlf rcpt":  `{"to":["a@x.com\r\nBcc: z@x.com"]}`,
		"long subj":  `{"subject":"` + strings.Repeat("a", 999) + `"}`,
		"put no id":  `{"body":"x"}`,
		"many rcpts": `{"to":[` + strings.Repeat(`"a@x.com",`, 100) + `"a@x.com"]}`,
	} {
		method := "POST"
		if name == "put no id" {
			method = "PUT"
		}
		if rec := r172Do(h.handleMailDraft, method, "/d", "a@x.com", body); rec.Code != 400 {
			t.Errorf("%s: %d want 400", name, rec.Code)
		}
	}
	// Empty draft is allowed; header injection through subject is neutralised.
	rec = r172Do(h.handleMailDraft, "POST", "/d", "a@x.com", `{"subject":"a\r\nBcc: z@x.com"}`)
	if rec.Code != 200 {
		t.Fatalf("empty/subject: %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	data, _ = ms.ReadMessage("a@x.com", out.ID)
	if strings.Contains(string(data), "\r\nBcc: z@x.com") {
		t.Error("header injection")
	}
	if rec := r172Do(h.handleMailDraft, "DELETE", "/d", "a@x.com", ""); rec.Code != 405 {
		t.Errorf("method %d", rec.Code)
	}
	if rec := r172Do(h.handleMailDraft, "POST", "/d", "", `{}`); rec.Code != 401 {
		t.Errorf("unauth %d", rec.Code)
	}
}

func TestR172MailListPagination(t *testing.T) {
	h, mdb, ms := r143MailSetup(t)
	for i := 0; i < 5; i++ {
		r143Deliver(t, mdb, ms, "a@x.com", "INBOX", fmt.Sprintf("m%d", i), nil)
	}
	rec := r172Do(h.handleMailList, "GET", "/api/v1/mail/inbox?limit=2&offset=1", "a@x.com", "")
	var out struct {
		Emails []Mail
		Total  int
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Total != 5 || len(out.Emails) != 2 || out.Emails[1].Subject != "m3" {
		t.Errorf("page: %d %s", rec.Code, rec.Body)
	}
	for _, q := range []string{"limit=0", "limit=501", "limit=x", "offset=-1", "offset=y"} {
		if rec := r172Do(h.handleMailList, "GET", "/api/v1/mail/inbox?"+q, "a@x.com", ""); rec.Code != 400 {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	rec = r172Do(h.handleMailList, "GET", "/api/v1/mail/inbox?offset=99", "a@x.com", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"emails":[]`) {
		t.Errorf("past end: %s", rec.Body)
	}
}
