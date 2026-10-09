// Regression tests for F5087: MKCALENDAR ".." wrote calendar metadata to the shared CalDAV root, visible to every user.
// Promoted from .temp_files/case_F5087_caldav_mkcalendar_traversal_test.go.

package caldav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func a5087Do(s *Server, user, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.SetBasicAuth(user, "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func TestRegressionF5087Control(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir, nil)
	w := a5087Do(s, "alice@x.test", "MKCALENDAR", "/dav/calendars/work")
	cal, _ := s.storage.GetCalendar("alice@x.test", "work")
	if w.Code != http.StatusCreated || cal == nil {
		t.Fatal("invalid control")
	}
}

func TestRegressionF5087Failure(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir, nil)
	w := a5087Do(s, "alice@x.test", "MKCALENDAR", "/dav/calendars/..")
	_, statErr := os.Stat(filepath.Join(dir, "caldav", ".calendar.json"))
	outside := statErr == nil
	bobSees, _ := s.storage.GetCalendar("bob@x.test", "..")
	if outside || w.Code < 400 || bobSees != nil {
		t.Fatal("DEFECT F5087: MKCALENDAR calendar-ID traversal writes outside the user's namespace")
	}
}

func TestRegressionF5087Edges(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir, nil)
	if w := a5087Do(s, "alice@x.test", "MKCALENDAR", "/dav/calendars/."); w.Code < 400 {
		t.Fatalf("MKCALENDAR '.' accepted: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "caldav", "alice_at_x.test", ".calendar.json")); err == nil {
		t.Fatal("'.' wrote calendar metadata into the user root")
	}
	// GET through '..' must not resolve a calendar even if a stray root file exists.
	_ = os.MkdirAll(filepath.Join(dir, "caldav"), 0o750)
	_ = os.WriteFile(filepath.Join(dir, "caldav", ".calendar.json"), []byte(`{"id":".."}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "caldav", "x.ics"), []byte("BEGIN:VCALENDAR\r\nUID:x\r\nEND:VCALENDAR\r\n"), 0o600)
	if w := a5087Do(s, "bob@x.test", "GET", "/dav/calendars/../x"); w.Code == http.StatusOK {
		t.Fatal("GET via '..' served a file outside the user's namespace")
	}
	// Normal calendar still works.
	if w := a5087Do(s, "alice@x.test", "MKCALENDAR", "/dav/calendars/home"); w.Code != http.StatusCreated {
		t.Fatalf("normal MKCALENDAR broke: %d", w.Code)
	}
}
