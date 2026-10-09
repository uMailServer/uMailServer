// Regression tests for F5088: PUT/DELETE ignored If-Match/If-None-Match (RFC 7232), allowing lost updates.
// Promoted from .temp_files/case_F5088_caldav_preconditions_test.go.

package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func a5088Do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func a5088ICS(summary string) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:ev1\r\nSUMMARY:" + summary + "\r\nDTSTART:20260101T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func a5088Setup(t *testing.T) (*Server, string) {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	if w := a5088Do(s, "MKCALENDAR", "/dav/calendars/cal", "", nil); w.Code != http.StatusCreated {
		t.Fatalf("fixture %d", w.Code)
	}
	w := a5088Do(s, "PUT", "/dav/calendars/cal/ev1", a5088ICS("original"), nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("fixture put %d", w.Code)
	}
	return s, w.Header().Get("ETag")
}

func a5088Get(s *Server) string {
	return a5088Do(s, "GET", "/dav/calendars/cal/ev1", "", nil).Body.String()
}

func TestRegressionF5088Control(t *testing.T) {
	s, etag := a5088Setup(t)
	w := a5088Do(s, "PUT", "/dav/calendars/cal/ev1", a5088ICS("updated"), map[string]string{"If-Match": etag})
	ok := w.Code < 300 && strings.Contains(a5088Get(s), "updated")
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5088Failure(t *testing.T) {
	s, _ := a5088Setup(t)
	w := a5088Do(s, "PUT", "/dav/calendars/cal/ev1", a5088ICS("clobbered"), map[string]string{"If-Match": `"stale-etag"`})
	clobbered := strings.Contains(a5088Get(s), "clobbered")
	w2 := a5088Do(s, "PUT", "/dav/calendars/cal/ev1", a5088ICS("created-twice"), map[string]string{"If-None-Match": "*"})
	overwritten := strings.Contains(a5088Get(s), "created-twice")
	if w.Code != http.StatusPreconditionFailed || clobbered || w2.Code != http.StatusPreconditionFailed || overwritten {
		t.Fatal("DEFECT F5088: CalDAV PUT ignores If-Match/If-None-Match (lost update)")
	}
}

func TestRegressionF5088Edges(t *testing.T) {
	s, etag := a5088Setup(t)
	// DELETE with stale If-Match must not delete.
	if w := a5088Do(s, "DELETE", "/dav/calendars/cal/ev1", "", map[string]string{"If-Match": `"stale"`}); w.Code != http.StatusPreconditionFailed {
		t.Fatalf("DELETE stale If-Match = %d, want 412", w.Code)
	}
	if !strings.Contains(a5088Get(s), "original") {
		t.Fatal("event deleted despite failed precondition")
	}
	// If-Match on a missing resource fails.
	if w := a5088Do(s, "PUT", "/dav/calendars/cal/ev2", strings.ReplaceAll(a5088ICS("x"), "ev1", "ev2"), map[string]string{"If-Match": "*"}); w.Code != http.StatusPreconditionFailed {
		t.Fatalf("PUT If-Match:* on missing = %d, want 412", w.Code)
	}
	// If-None-Match:* creates a new resource.
	if w := a5088Do(s, "PUT", "/dav/calendars/cal/ev3", strings.ReplaceAll(a5088ICS("x"), "ev1", "ev3"), map[string]string{"If-None-Match": "*"}); w.Code != http.StatusCreated {
		t.Fatalf("PUT If-None-Match:* on new = %d, want 201", w.Code)
	}
	// Matching If-Match in a list + DELETE succeeds.
	if w := a5088Do(s, "DELETE", "/dav/calendars/cal/ev1", "", map[string]string{"If-Match": `"other", ` + etag}); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE matching If-Match list = %d, want 204", w.Code)
	}
	// Weak validator never matches If-Match (strong comparison, RFC 7232 §2.3.2).
	a5088Do(s, "PUT", "/dav/calendars/cal/ev1", a5088ICS("again"), nil)
	cur := a5088Do(s, "GET", "/dav/calendars/cal/ev1", "", nil).Header().Get("ETag")
	if w := a5088Do(s, "DELETE", "/dav/calendars/cal/ev1", "", map[string]string{"If-Match": "W/" + cur}); w.Code != http.StatusPreconditionFailed {
		t.Fatalf("weak If-Match = %d, want 412", w.Code)
	}
}
