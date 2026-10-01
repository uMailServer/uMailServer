package caldav

// Regression tests for the PUT UID-identity defect: handlePut stored the
// event under the iCalendar BODY UID, so a PUT to /dav/calendars/{cal}/{uid}
// whose body UID differed created a resource unreachable at its request-URL
// (GET/DELETE 404) while the PUT reported success. RFC 4791 §5.3.2 makes the
// request-URI's UID authoritative and requires rejecting a mismatch (403).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putUIDCalendar(t *testing.T) (*Server, *httptest.ResponseRecorder) {
	t.Helper()
	server := NewServer(t.TempDir(), nil)

	mk := httptest.NewRequest("MKCALENDAR", "/dav/calendars/cal-1", nil)
	mk.SetBasicAuth("alice", "pw")
	mkw := httptest.NewRecorder()
	server.ServeHTTP(mkw, mk)
	if mkw.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): MKCALENDAR = %d, want 201", mkw.Code)
	}

	put := func(path, uid string) *httptest.ResponseRecorder {
		t.Helper()
		body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nSUMMARY:proof\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		return w
	}

	// Control: matching UIDs — the normal client case — must round-trip.
	if w := put("/dav/calendars/cal-1/evt-a", "evt-a"); w.Code < 200 || w.Code >= 300 {
		t.Fatalf("CONTROL FAILED (harness): matching-UID PUT = %d, want 2xx", w.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/dav/calendars/cal-1/evt-a", nil)
	get.SetBasicAuth("alice", "pw")
	gw := httptest.NewRecorder()
	server.ServeHTTP(gw, get)
	if gw.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED (harness): matching-UID event not fetchable: %d", gw.Code)
	}

	return server, put("/dav/calendars/cal-1/evt-2", "evt-other")
}

func TestHandlePutRejectsMismatchedUID(t *testing.T) {
	_, w := putUIDCalendar(t)
	if w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: mismatched-UID PUT = %d, want 403 per RFC 4791 §5.3.2 (a mismatch would store the event at an address the client cannot address)", w.Code)
	}
}

func TestHandlePutMismatchStoresNothing(t *testing.T) {
	server, _ := putUIDCalendar(t)

	// Nothing may be stored at the mismatched body UID either: the request
	// was rejected, so neither evt-2 nor evt-other may exist.
	for _, uid := range []string{"evt-2", "evt-other"} {
		req := httptest.NewRequest(http.MethodGet, "/dav/calendars/cal-1/"+uid, nil)
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Fatalf("FAIL: rejected PUT still stored a resource at %q", uid)
		}
	}
}
