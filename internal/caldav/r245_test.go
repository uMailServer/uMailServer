package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r245ICS(uid string) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// F7220: updating returns 204; F7221: preconditions are evaluated before the UID conflict.
func TestR245PutStatusAndPreconditionOrder(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	if w := r125Do(t, s, "MKCALENDAR", "/dav/calendars/c", "alice", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR=%d", w.Code)
	}
	if w := r125Do(t, s, "PUT", "/dav/calendars/c/a", "alice", r245ICS("u1")); w.Code != http.StatusCreated {
		t.Fatalf("create=%d", w.Code)
	}
	if w := r125Do(t, s, "PUT", "/dav/calendars/c/a", "alice", r245ICS("u1")); w.Code != http.StatusNoContent {
		t.Fatalf("update=%d want 204", w.Code)
	}
	req := httptest.NewRequest("PUT", "/dav/calendars/c/b", strings.NewReader(r245ICS("u1")))
	req.SetBasicAuth("alice", "pw")
	req.Header.Set("If-Match", `"nope"`)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("got %d want 412", w.Code)
	}
	if w := r125Do(t, s, "PUT", "/dav/calendars/c/b", "alice", r245ICS("u1")); w.Code != http.StatusForbidden {
		t.Fatalf("conflict=%d want 403", w.Code)
	}
}
