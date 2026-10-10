// Regression tests for F5580: CalDAV MOVE/COPY replaced an existing destination despite "Overwrite: F" (RFC 4918 §10.6).
package caldav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const rF5580Src = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ev1\r\nSUMMARY:source\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
const rF5580Dst = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ev2\r\nSUMMARY:precious\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func rF5580Setup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	for _, c := range []string{"src", "dst"} {
		if e := s.storage.CreateCalendar("alice", &Calendar{ID: c}); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.storage.SaveEvent("alice", "src", &CalendarEvent{UID: "ev1"}, rF5580Src); e != nil {
		t.Fatal(e)
	}
	if e := s.storage.SaveEvent("alice", "dst", &CalendarEvent{UID: "ev2"}, rF5580Dst); e != nil {
		t.Fatal(e)
	}
	return s
}

func rF5580Do(s *Server, method, dest, overwrite string) int {
	r := httptest.NewRequest(method, "/dav/calendars/src/ev1", nil)
	r.SetBasicAuth("alice", "pw")
	r.Header.Set("Destination", dest)
	if overwrite != "" {
		r.Header.Set("Overwrite", overwrite)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w.Code
}

func rF5580Get(t *testing.T, s *Server, cal, uid string) string {
	t.Helper()
	v, e := s.storage.GetEvent("alice", cal, uid)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCalDAVOverwriteF5580Control(t *testing.T) {
	s := rF5580Setup(t)
	code := rF5580Do(s, "COPY", "/dav/calendars/dst/free", "F")
	ok := code/100 == 2 && rF5580Get(t, s, "dst", "free") != ""
	fmt.Printf("CONTROL EXPECTED: COPY to a free destination with Overwrite F succeeds ACTUAL: code=%d ok=%v\n", code, ok)
	if !ok {
		t.Fatal("invalid control")
	}
}

func TestCalDAVOverwriteF5580Failure(t *testing.T) {
	s := rF5580Setup(t)
	code := rF5580Do(s, "COPY", "/dav/calendars/dst/ev2", "F")
	unchanged := rF5580Get(t, s, "dst", "ev2") == rF5580Dst
	fmt.Printf("EXPECTED: COPY Overwrite F onto existing -> 412, dst unchanged ACTUAL: code=%d unchanged=%v\n", code, unchanged)
	s2 := rF5580Setup(t)
	mcode := rF5580Do(s2, "MOVE", "/dav/calendars/dst/ev2", "F")
	munchanged := rF5580Get(t, s2, "dst", "ev2") == rF5580Dst
	srcKept := rF5580Get(t, s2, "src", "ev1") == rF5580Src
	fmt.Printf("EXPECTED: MOVE Overwrite F onto existing -> 412, dst unchanged, src kept ACTUAL: code=%d unchanged=%v src_kept=%v\n", mcode, munchanged, srcKept)
	if code != http.StatusPreconditionFailed || !unchanged || mcode != http.StatusPreconditionFailed || !munchanged || !srcKept {
		t.Fatal("DEFECT F5580: MOVE/COPY replace an existing destination despite Overwrite: F")
	}
}

func TestCalDAVOverwriteF5580Edges(t *testing.T) {
	s := rF5580Setup(t)
	if code := rF5580Do(s, "COPY", "/dav/calendars/dst/ev2", "T"); code != http.StatusNoContent || rF5580Get(t, s, "dst", "ev2") == rF5580Dst {
		t.Fatalf("Overwrite T must replace: %d", code)
	}
	s = rF5580Setup(t)
	if code := rF5580Do(s, "COPY", "/dav/calendars/dst/ev2", " f "); code != http.StatusPreconditionFailed || rF5580Get(t, s, "dst", "ev2") != rF5580Dst {
		t.Fatalf("lower-case padded f must be honoured: %d", code)
	}
	s = rF5580Setup(t)
	if code := rF5580Do(s, "MOVE", "/dav/calendars/dst/ev2", ""); code != http.StatusNoContent || rF5580Get(t, s, "src", "ev1") != "" || rF5580Get(t, s, "dst", "ev2") == rF5580Dst {
		t.Fatalf("absent Overwrite means T: %d", code)
	}
}
