// Regression tests for F5582: CalDAV MOVE/COPY ran outside writeMu and lost a concurrent PUT (gated ordering, run with -race).
package caldav

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const rF5582Old = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ev1\r\nSUMMARY:old\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
const rF5582New = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ev1\r\nSUMMARY:new\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func rF5582Setup(t *testing.T) *Server {
	t.Helper()
	s := NewServer(t.TempDir(), nil)
	for _, c := range []string{"src", "dst"} {
		if e := s.storage.CreateCalendar("alice", &Calendar{ID: c}); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.storage.SaveEvent("alice", "src", &CalendarEvent{UID: "ev1"}, rF5582Old); e != nil {
		t.Fatal(e)
	}
	return s
}

func rF5582Start(s *Server, method, dest string) chan int {
	done := make(chan int, 1)
	go func() {
		r := httptest.NewRequest(method, "/dav/calendars/src/ev1", nil)
		r.SetBasicAuth("alice", "pw")
		r.Header.Set("Destination", dest)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		done <- w.Code
	}()
	return done
}

func rF5582Get(t *testing.T, s *Server, cal, uid string) string {
	t.Helper()
	v, e := s.storage.GetEvent("alice", cal, uid)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCalDAVMoveCopyWriteLockF5582Control(t *testing.T) {
	s := rF5582Setup(t)
	code := <-rF5582Start(s, "MOVE", "/dav/calendars/dst/ev1")
	ok := code/100 == 2 && rF5582Get(t, s, "dst", "ev1") == rF5582Old && rF5582Get(t, s, "src", "ev1") == ""
	fmt.Printf("CONTROL EXPECTED: uncontended MOVE succeeds ACTUAL: code=%d ok=%v\n", code, ok)
	if !ok {
		t.Fatal("invalid control")
	}
}

// TestCalDAVMoveCopyWriteLockF5582Failure holds writeMu the way an in-flight PUT does after its
// If-Match check passed. A MOVE that honours the lock cannot complete until
// the PUT finishes; the buggy MOVE completes, deletes the source, and the
// PUT's write then resurrects it next to the stale destination copy.
func TestCalDAVMoveCopyWriteLockF5582Failure(t *testing.T) {
	s := rF5582Setup(t)
	s.writeMu.Lock()
	done := rF5582Start(s, "MOVE", "/dav/calendars/dst/ev1")
	var code int
	completedUnderLock := false
	select {
	case code = <-done:
		completedUnderLock = true
	case <-time.After(500 * time.Millisecond): // liveness bound only: a fixed MOVE blocks until Unlock
	}
	// The in-flight PUT now performs its write and releases the lock.
	if e := s.storage.SaveEvent("alice", "src", &CalendarEvent{UID: "ev1"}, rF5582New); e != nil {
		t.Fatal(e)
	}
	s.writeMu.Unlock()
	if !completedUnderLock {
		code = <-done
	}
	dst := rF5582Get(t, s, "dst", "ev1")
	src := rF5582Get(t, s, "src", "ev1")
	fmt.Printf("EXPECTED: MOVE waits for the PUT; dst=new src=gone ACTUAL: code=%d completed_under_lock=%v dst_new=%v src_present=%v\n",
		code, completedUnderLock, dst == rF5582New, src != "")
	if completedUnderLock || dst != rF5582New || src != "" {
		t.Fatal("DEFECT F5582: MOVE runs outside writeMu and loses a concurrent PUT")
	}
}

// TestCalDAVMoveCopyWriteLockF5582Edges uses gated ordering only: the lock is held, the
// competing write happens, the lock is released, and only then is the
// MOVE/COPY result awaited.
func TestCalDAVMoveCopyWriteLockF5582Edges(t *testing.T) {
	for _, method := range []string{"MOVE", "COPY"} {
		for i := 0; i < 3; i++ {
			s := rF5582Setup(t)
			s.writeMu.Lock()
			done := rF5582Start(s, method, "/dav/calendars/dst/ev1")
			if e := s.storage.SaveEvent("alice", "src", &CalendarEvent{UID: "ev1"}, rF5582New); e != nil {
				t.Fatal(e)
			}
			s.writeMu.Unlock()
			code := <-done
			if code != http.StatusCreated || rF5582Get(t, s, "dst", "ev1") != rF5582New {
				t.Fatalf("%s #%d: code=%d, destination must hold the PUT's data", method, i, code)
			}
			if method == "MOVE" && rF5582Get(t, s, "src", "ev1") != "" {
				t.Fatalf("MOVE #%d left the source behind", i)
			}
			if method == "COPY" && rF5582Get(t, s, "src", "ev1") != rF5582New {
				t.Fatalf("COPY #%d changed the source", i)
			}
		}
	}
}
