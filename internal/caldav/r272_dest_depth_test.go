package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const r272ICS = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e1\r\nDTSTART:20240101T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func r272Do(s *Server, method, path, body, dest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth("alice@x.test", "pw")
	if dest != "" {
		req.Header.Set("Destination", dest)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

// F7462: a Destination deeper than /{calendar}/{name} (or with an encoded "/"
// in the name) is rejected instead of being truncated to its first segment.
func TestR272MoveCopyDeepDestination(t *testing.T) {
	for _, method := range []string{"COPY", "MOVE"} {
		for _, d := range []string{"/dav/calendars/c1/a%2Fb", "/dav/calendars/c1/m/n/o", "/dav/calendars/c1//x", "http://h.test/dav/calendars/c1/m/n"} {
			s := NewServer(t.TempDir(), nil)
			r272Do(s, "MKCALENDAR", "/dav/calendars/c1", "", "")
			r272Do(s, "PUT", "/dav/calendars/c1/e1", r272ICS, "")
			w := r272Do(s, method, "/dav/calendars/c1/e1", "", d)
			if w.Code != http.StatusConflict {
				t.Errorf("%s -> %q = %d want 409", method, d, w.Code)
			}
			entries, _ := s.storage.GetEventEntries("alice@x.test", "c1")
			if len(entries) != 1 || entries[0].Name != "e1" {
				t.Errorf("%s -> %q changed the calendar: %d entries", method, d, len(entries))
			}
		}
	}
	// Control: a plain destination still works, also as absolute URI.
	s := NewServer(t.TempDir(), nil)
	r272Do(s, "MKCALENDAR", "/dav/calendars/c1", "", "")
	r272Do(s, "PUT", "/dav/calendars/c1/e1", r272ICS, "")
	if w := r272Do(s, "COPY", "/dav/calendars/c1/e1", "", "http://h.test/dav/calendars/c1/e2"); w.Code != http.StatusCreated {
		t.Fatalf("valid COPY=%d", w.Code)
	}
}
