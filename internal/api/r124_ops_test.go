package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func r124Server(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/r124.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewServer(database, nil, Config{TokenExpiry: time.Hour}), database
}

func r124Do(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// F6060: domain names are validated strictly and stored lower-case.
func TestR124CreateDomainRejectsBadNames(t *testing.T) {
	s, _ := r124Server(t)
	for _, name := range []string{"bad name.com", "a\r\nb.com", "a@b.com", "-a.com", "a..com", "a;b.com", "exa\x00mple.com"} {
		b, _ := json.Marshal(map[string]string{"name": name})
		rec := r124Do(s.handleDomains, http.MethodPost, "/api/v1/domains", string(b))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: got %d, want 400", name, rec.Code)
		}
	}
}

func TestR124CreateDomainLowercases(t *testing.T) {
	s, database := r124Server(t)
	rec := r124Do(s.handleDomains, http.MethodPost, "/api/v1/domains", `{"name":"Example.COM"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if _, err := database.GetDomain("example.com"); err != nil {
		t.Fatalf("domain not stored lower-case: %v", err)
	}
}

// F6061: the queue list shows every undelivered entry (same set as the stats
// queue_size), returns [] when empty, and supports a status filter.
func TestR124QueueListIncludesFailedAndEmptyArray(t *testing.T) {
	s, database := r124Server(t)
	rec := r124Do(s.handleQueue, http.MethodGet, "/api/v1/queue", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty queue body = %q, want []", rec.Body.String())
	}
	now := time.Now()
	for id, st := range map[string]string{"p1": "pending", "f1": "failed", "d1": "delivered"} {
		if err := database.Enqueue(&db.QueueEntry{ID: id, Status: st, From: "a@b.c", To: []string{"x@y.z"}, NextRetry: now}); err != nil {
			t.Fatal(err)
		}
	}
	rec = r124Do(s.handleQueue, http.MethodGet, "/api/v1/queue", "")
	var out []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d entries, want pending+failed: %s", len(out), rec.Body)
	}
	rec = r124Do(s.handleQueue, http.MethodGet, "/api/v1/queue?status=failed", "")
	out = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 1 || out[0]["id"] != "f1" {
		t.Fatalf("status filter: %s", rec.Body)
	}
	rec = r124Do(s.handleQueue, http.MethodGet, "/api/v1/queue?limit=1", "")
	out = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out) != 1 {
		t.Fatalf("limit: %s", rec.Body)
	}
	if rec := r124Do(s.handleQueue, http.MethodGet, "/api/v1/queue?limit=abc", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", rec.Code)
	}
}

// F6062: retrying an in-flight or finished entry must not reset it (it would
// be delivered twice / resurrected); a missing entry is 404.
func TestR124QueueRetryStateGuard(t *testing.T) {
	s, database := r124Server(t)
	for id, st := range map[string]string{"s1": "sending", "d1": "delivered", "f1": "failed"} {
		_ = database.Enqueue(&db.QueueEntry{ID: id, Status: st, NextRetry: time.Now().Add(time.Hour)})
	}
	for _, id := range []string{"s1", "d1"} {
		rec := r124Do(s.handleQueueDetail, http.MethodPost, "/api/v1/queue/"+id, "")
		if rec.Code != http.StatusConflict {
			t.Errorf("retry %s: got %d, want 409", id, rec.Code)
		}
		e, _ := database.GetQueueEntry(id)
		if e.Status == "pending" {
			t.Errorf("retry %s reset status", id)
		}
	}
	if rec := r124Do(s.handleQueueDetail, http.MethodPost, "/api/v1/queue/f1", ""); rec.Code != http.StatusOK {
		t.Errorf("retry failed: %d", rec.Code)
	}
	if rec := r124Do(s.handleQueueDetail, http.MethodPost, "/api/v1/queue/", ""); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("empty id: %d", rec.Code)
	}
}

// F6064: static serving must not panic on directory paths or hand SPA html to
// missing asset files.
func TestR124StaticDirectoryAndMissingAsset(t *testing.T) {
	s, _ := r124Server(t)
	mfs := fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("x=1")},
	}
	s.webmailFS = NewEmbedFSAdapter(mfs)
	s.adminFS = NewEmbedFSAdapter(mfs)
	for _, p := range []string{"/assets", "/admin/assets"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusInternalServerError {
			t.Errorf("%s: 500 (directory served through ReadSeeker assertion)", p)
		}
	}
	for _, p := range []string{"/assets/missing.js", "/admin/assets/missing.css"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", p, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/inbox", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Errorf("SPA route fallback broke: %d", rec.Code)
	}
}

// F6065: failed-login bookkeeping is bounded; stale entries are pruned.
func TestR124AuthAttemptMapsPruned(t *testing.T) {
	s, _ := r124Server(t)
	old := time.Now().Add(-time.Hour)
	s.loginAttempts = map[string]*loginAttempt{"1.2.3.4": {count: 1, lastSeen: old}}
	s.accountLoginAttempts = map[string]*loginAttempt{"a@b.c": {count: 1, lastSeen: old}}
	s.totpAttempts = map[string]*totpAttempt{"a@b.c": {count: 1, lastSeen: old}}
	s.apiRateAttempts = map[string]*apiRateAttempt{"1.2.3.4": {count: 1, windowStart: old}}
	s.pruneAuthAttempts()
	if len(s.loginAttempts)+len(s.accountLoginAttempts)+len(s.totpAttempts)+len(s.apiRateAttempts) != 0 {
		t.Fatal("stale attempt entries not pruned")
	}
	// Unbounded growth from attacker-chosen emails is capped inline.
	for i := 0; i < maxAuthAttemptEntries+50; i++ {
		s.recordAccountLoginFailure(strings.Repeat("x", i%7+1) + string(rune('a'+i%26)) + time.Duration(i).String())
	}
	if len(s.accountLoginAttempts) > maxAuthAttemptEntries+1 {
		t.Fatalf("map grew to %d", len(s.accountLoginAttempts))
	}
}

// F6066: cluster responses declare JSON.
func TestR124ClusterStatusContentType(t *testing.T) {
	s, _ := r124Server(t)
	rec := r124Do(s.handleClusterStatus, http.MethodGet, "/api/v1/cluster/status", "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

// F6067: autoconfig derives the mail domain from the client-facing host prefix.
func TestR124AutoconfigDomainFromHost(t *testing.T) {
	cases := map[string]string{
		"autoconfig.example.com":       "example.com",
		"autodiscover.example.com:443": "example.com",
		"mail.example.com":             "mail.example.com",
		"example.com":                  "example.com",
		"localhost":                    "",
	}
	for host, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/mail/config-v1.1.xml", nil)
		req.Host = host
		if got := extractDomainFromRequest(req); got != want {
			t.Errorf("host %q: got %q want %q", host, got, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/x?emailaddress=bob%40Other.org", nil)
	req.Host = "autoconfig.example.com"
	if got := extractDomainFromRequest(req); got != "other.org" {
		t.Errorf("emailaddress: got %q", got)
	}
}
