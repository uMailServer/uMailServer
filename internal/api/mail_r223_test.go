package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postWith(t *testing.T, h http.HandlerFunc, ct, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	r = r.WithContext(withUser(r.Context(), "alice@example.com"))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestDecodeJSONStatus_F7000(t *testing.T) {
	var v struct{ A string }
	mk := func(ct, body string) error {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(body))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		return decodeJSON(r, &v)
	}
	if err := mk("application/json; charset=utf-8", `{"A":"x"}`); err != nil {
		t.Fatal(err)
	}
	if err := mk("", `{"A":"x"}`); err != nil {
		t.Fatalf("missing content-type must stay accepted: %v", err)
	}
	if got := jsonErrStatus(mk("text/plain", `{"A":"x"}`)); got != 415 {
		t.Errorf("text/plain = %d want 415", got)
	}
	if got := jsonErrStatus(mk("application/json", `{bad`)); got != 400 {
		t.Errorf("bad json = %d want 400", got)
	}
	// empty body with odd Content-Type is not a media-type problem
	r := httptest.NewRequest("POST", "/x", http.NoBody)
	r.Header.Set("Content-Type", "text/plain")
	if got := jsonErrStatus(decodeJSON(r, &v)); got != 400 {
		t.Errorf("empty body = %d want 400", got)
	}
}

func TestDecodeJSONTooLarge_F7001(t *testing.T) {
	var v struct{ A string }
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"A":"`+strings.Repeat("a", 100)+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Body = http.MaxBytesReader(w, r.Body, 10)
	if got := jsonErrStatus(decodeJSON(r, &v)); got != 413 {
		t.Errorf("oversized = %d want 413", got)
	}
}

func TestHandlerWrongContentType_F7000(t *testing.T) {
	s, _, _, _ := startThreadsTestServer(t)
	w := postWith(t, s.handleSetVacation, "text/plain", `{}`)
	if w.Code != 415 {
		t.Errorf("status %d want 415: %s", w.Code, w.Body)
	}
}

func TestThreadsMailboxFilter_F7002(t *testing.T) {
	s, _, user, _ := seedSharedMailboxThread(t)
	// a thread living only in Sent is hidden from INBOX and vice versa
	if err := s.mailDB.CreateMailbox(user, "Drafts"); err != nil {
		t.Fatal(err)
	}
	count := func(q string) (int, ThreadListResponse) {
		r := httptest.NewRequest("GET", "/api/v1/threads"+q, nil)
		r = r.WithContext(withUser(r.Context(), user))
		w := httptest.NewRecorder()
		s.handleThreads(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", q, w.Code, w.Body)
		}
		var resp ThreadListResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return len(resp.Threads), resp
	}
	if n, _ := count(""); n != 1 {
		t.Errorf("unfiltered = %d want 1", n)
	}
	if n, resp := count("?mailbox=sent"); n != 1 || resp.Threads[0].MessageCount != 1 {
		t.Errorf("sent = %d %+v", n, resp)
	}
	if n, _ := count("?mailbox=Drafts"); n != 0 {
		t.Errorf("drafts = %d want 0", n)
	}
	if n, _ := count("?folder=Drafts"); n != 0 {
		t.Errorf("folder=Drafts = %d want 0", n)
	}
}
