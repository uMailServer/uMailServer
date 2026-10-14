package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r296Do(s *Server, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	req.SetBasicAuth("alice@x.test", "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

// F7700: MKCALENDAR only creates a calendar directly under /dav/calendars/; a
// deeper path is a missing intermediate collection (409), not the first
// segment's calendar with the rest ignored.
func TestMkcalendarDeepPathRefused(t *testing.T) {
	for _, path := range []string{
		"/dav/calendars/other/extra/",
		"/dav/calendars/other/extra/segments/",
		"/dav/calendars/other/a%2Fb",
	} {
		s := NewServer(t.TempDir(), nil)
		if w := r296Do(s, "MKCALENDAR", path); w.Code != http.StatusConflict {
			t.Errorf("%s = %d, want 409", path, w.Code)
		}
		if cal, _ := s.storage.GetCalendar("alice@x.test", "other"); cal != nil {
			t.Errorf("%s created calendar %q", path, "other")
		}
	}
}

func TestMkcalendarPlainPathsStillWork(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	for _, path := range []string{"/dav/calendars/c1", "/dav/calendars/c2/"} {
		if w := r296Do(s, "MKCALENDAR", path); w.Code != http.StatusCreated {
			t.Errorf("%s = %d, want 201", path, w.Code)
		}
	}
	// control: re-creating an existing calendar is still refused (F5584)
	if w := r296Do(s, "MKCALENDAR", "/dav/calendars/c1/"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("existing calendar = %d, want 405", w.Code)
	}
}
