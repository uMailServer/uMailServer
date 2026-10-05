package caldav

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalendarReportValidControl(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateCalendar("u", &Calendar{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:good\nEND:VEVENT\nEND:VCALENDAR"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/calendars/c", strings.NewReader("<calendar-query/>")), "u")
	if w.Code != http.StatusMultiStatus || !strings.Contains(w.Body.String(), "good") {
		t.Fatalf("CONTROL invalid: %d %s", w.Code, w.Body.String())
	}
}

func TestCalendarReportReadFailure(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateCalendar("u", &Calendar{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:good\nEND:VEVENT\nEND:VCALENDAR"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.storage.dataDir, "u", "c")
	if err := os.Symlink(dir, filepath.Join(dir, "bad.ics")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/calendars/c", strings.NewReader("<calendar-query/>")), "u")
	t.Logf("EXPECTED: storage failure HTTP500 ACTUAL: status=%d body=%s\n", w.Code, w.Body.String())
	if w.Code != http.StatusInternalServerError {
		t.Error("REPORT storage failure hidden as success")
	}
}

func TestCalendarReportEmptyCollection(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateCalendar("u", &Calendar{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/calendars/c", strings.NewReader("<calendar-query/>")), "u")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("empty collection: %d %s", w.Code, w.Body.String())
	}
}

func TestCalendarReportCompatibility(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateCalendar("u", &Calendar{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:good\nEND:VEVENT\nEND:VCALENDAR"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/calendars/c", strings.NewReader("<broken")), "u")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid XML: %d", w.Code)
	}
}
