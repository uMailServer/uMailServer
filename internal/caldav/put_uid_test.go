package caldav

// PUT resource-name / UID identity: the client-chosen resource name is the
// storage key and need not equal the UID in the data (RFC 4791 §4.1, §5.3.2);
// one UID may live in only one resource (no-uid-conflict).

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

// Round 134: the resource name is chosen by the client and need not equal the
// UID (Apple/Thunderbird/DAVx5 PUT <random>.ics). The name is the storage key.
func TestHandlePutAcceptsArbitraryResourceName(t *testing.T) {
	server, w := putUIDCalendar(t)
	if w.Code != http.StatusCreated {
		t.Fatalf("PUT with differing name/UID = %d, want 201", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/dav/calendars/cal-1/evt-2", nil)
	req.SetBasicAuth("alice", "pw")
	gw := httptest.NewRecorder()
	server.ServeHTTP(gw, req)
	if gw.Code != http.StatusOK || !strings.Contains(gw.Body.String(), "UID:evt-other") {
		t.Fatalf("GET by resource name = %d %q", gw.Code, gw.Body.String())
	}
	// PROPFIND hrefs use the resource name.
	pf := httptest.NewRequest("PROPFIND", "/dav/calendars/cal-1/", nil)
	pf.Header.Set("Depth", "1")
	pf.SetBasicAuth("alice", "pw")
	pw := httptest.NewRecorder()
	server.ServeHTTP(pw, pf)
	if !strings.Contains(pw.Body.String(), "/dav/calendars/cal-1/evt-2<") {
		t.Fatalf("PROPFIND missing resource-name href: %s", pw.Body.String())
	}
	// DELETE by resource name.
	del := httptest.NewRequest(http.MethodDelete, "/dav/calendars/cal-1/evt-2", nil)
	del.SetBasicAuth("alice", "pw")
	dw := httptest.NewRecorder()
	server.ServeHTTP(dw, del)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", dw.Code)
	}
}

func TestHandlePutDuplicateUIDConflicts(t *testing.T) {
	server, _ := putUIDCalendar(t)
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:evt-other\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	req := httptest.NewRequest(http.MethodPut, "/dav/calendars/cal-1/third.ics", strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "no-uid-conflict") {
		t.Fatalf("duplicate UID PUT = %d %s", w.Code, w.Body.String())
	}
	// Same resource re-PUT is not a conflict.
	req = httptest.NewRequest(http.MethodPut, "/dav/calendars/cal-1/evt-2", strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("re-PUT = %d", w.Code)
	}
}

func TestHandlePutResourceNameTraversalRejected(t *testing.T) {
	server, _ := putUIDCalendar(t)
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	for _, p := range []string{"/dav/calendars/cal-1/..%2Fx", "/dav/calendars/cal-1/a%00b", "/dav/calendars/cal-1/x/y"} {
		req := httptest.NewRequest(http.MethodPut, p, strings.NewReader(body))
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatalf("PUT %s = %d, want error", p, w.Code)
		}
	}
}
