package caldav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const selfMoveRegressionICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:meeting\r\nDTSTART:20261005T120000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func selfMoveRegressionServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	for _, id := range []string{"source", "target"} {
		if e := s.storage.CreateCalendar("fixture", &Calendar{ID: id}); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.storage.SaveEvent("fixture", "source", &CalendarEvent{UID: "meeting"}, selfMoveRegressionICS); e != nil {
		t.Fatal(e)
	}
	return s
}
func selfMoveRegressionMove(s *Server, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("MOVE", "/dav/calendars/source/meeting", nil)
	r.Header.Set("Destination", target)
	w := httptest.NewRecorder()
	s.handleMove(w, r, "fixture")
	return w
}
func TestMoveAcrossCalendarsPreservesEvent(t *testing.T) {
	s := selfMoveRegressionServer(t)
	w := selfMoveRegressionMove(s, "/dav/calendars/target/meeting")
	v, e := s.storage.GetEvent("fixture", "target", "meeting")
	old, oe := s.storage.GetEvent("fixture", "source", "meeting")
	ok := w.Code == http.StatusCreated && e == nil && oe == nil && v == selfMoveRegressionICS && old == ""
	fmt.Printf("CONTROL EXPECTED: cross-calendar move preserves destination ACTUAL: %v\n", ok)
	if !ok {
		t.Fatal("invalid control")
	}
}
func TestMoveToSameResourcePreservesEvent(t *testing.T) {
	s := selfMoveRegressionServer(t)
	w := selfMoveRegressionMove(s, "/dav/calendars/source/meeting")
	v, e := s.storage.GetEvent("fixture", "source", "meeting")
	if e != nil {
		t.Fatal(e)
	}
	fmt.Printf("EXPECTED: self-move preserves event ACTUAL: HTTP %d preserved=%v\n", w.Code, v == selfMoveRegressionICS)
	if v != selfMoveRegressionICS {
		t.Fatal("DEFECT F4820: successful self-move deletes event")
	}
}
func TestMoveSameResourceRepeatedAndMissing(t *testing.T) {
	s := selfMoveRegressionServer(t)
	for i := 0; i < 2; i++ {
		w := selfMoveRegressionMove(s, "/dav/calendars/source/meeting")
		v, e := s.storage.GetEvent("fixture", "source", "meeting")
		if w.Code != http.StatusNoContent || e != nil || v != selfMoveRegressionICS {
			t.Fatal(w.Code, v, e)
		}
	}
	w := selfMoveRegressionMove(s, "/dav/calendars/target/renamed")
	v, e := s.storage.GetEvent("fixture", "target", "renamed")
	if w.Code != http.StatusCreated || e != nil || !strings.Contains(v, "UID:renamed") {
		t.Fatal(w.Code, v, e)
	}
	w = selfMoveRegressionMove(s, "/dav/calendars/source/meeting")
	if w.Code != http.StatusNotFound {
		t.Fatal("missing source must fail", w.Code)
	}
}
