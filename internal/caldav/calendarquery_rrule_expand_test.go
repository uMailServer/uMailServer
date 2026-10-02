package caldav

// Regression tests for the RRULE expansion + CALDAV:expand/limit design:
// (1) a recurring event matches a calendar-query time-range when ANY
// generated instance intersects the range (RFC 4791 §9.9.1) — previously the
// base DTSTART alone was tested, so a DAILY event starting outside the range
// was never reported; (2) CALDAV:expand in calendar-data expands the
// recurrence set into per-instance VEVENTs (RECURRENCE-ID) inside the expand
// window, capped by CALDAV:limit/nresults (RFC 4791 §9.6.5).
//
// Supported RRULE subset (documented): FREQ=DAILY|WEEKLY|MONTHLY|YEARLY,
// INTERVAL, COUNT, UNTIL. Other parts (BYDAY, BYMONTH, ...) degrade the
// event to base-only matching.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rruleExpandServer(t *testing.T) *Server {
	server := NewServer(t.TempDir(), nil)
	mk := httptest.NewRequest("MKCALENDAR", "/dav/calendars/cal-1", nil)
	mk.SetBasicAuth("alice", "pw")
	mkw := httptest.NewRecorder()
	server.ServeHTTP(mkw, mk)
	if mkw.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): MKCALENDAR = %d, want 201", mkw.Code)
	}
	return server
}

func rrulePut(t *testing.T, server *Server, path, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("CONTROL FAILED (harness): PUT %s = %d, want 2xx", path, w.Code)
	}
}

const rruleDailyEvent = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:rrule-daily\r\nSUMMARY:daily standup\r\nDTSTART:20260301T090000Z\r\nDTEND:20260301T100000Z\r\nRRULE:FREQ=DAILY;COUNT=10\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// TestCalendarQueryRRULEMatchesExpandedInstance: a DAILY event whose base
// DTSTART (March 1) lies OUTSIDE the query range still matches when one of
// its generated instances (March 5) intersects it.
func TestCalendarQueryRRULEMatchesExpandedInstance(t *testing.T) {
	server := rruleExpandServer(t)
	rrulePut(t, server, "/dav/calendars/cal-1/rrule-daily", rruleDailyEvent)

	body := eventQueryXML(`<c:comp-filter name="VEVENT">` +
		`<c:time-range start="20260305T000000Z" end="20260306T000000Z"/>` +
		`</c:comp-filter>`)
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "rrule-daily") {
		t.Fatalf("FAIL: the recurring event was not matched — no instance of the March-1 DAILY series intersects March 5, or expansion is not applied (RFC 4791 §9.9.1)")
	}
}

// TestCalendarQueryExpandProducesInstancesWithLimit: CALDAV:expand expands
// the recurrence set into per-instance VEVENTs inside the expand window and
// CALDAV:limit/nresults caps how many are returned.
func TestCalendarQueryExpandProducesInstancesWithLimit(t *testing.T) {
	server := rruleExpandServer(t)
	rrulePut(t, server, "/dav/calendars/cal-1/rrule-daily", rruleDailyEvent)

	// Expand March 1-5: four daily instances (01..04); limit to two.
	reqBody := `<x:calendar-query xmlns:x="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:">` +
		`<d:prop><x:calendar-data><x:expand start="20260301T000000Z" end="20260305T000000Z"/><x:limit><x:nresults>2</x:nresults></x:limit></x:calendar-data></d:prop>` +
		`<x:filter><x:comp-filter name="VCALENDAR"><x:comp-filter name="VEVENT"/></x:comp-filter></x:filter>` +
		`</x:calendar-query>`
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(reqBody))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	respBody := w.Body.String()
	instances := strings.Count(respBody, "RECURRENCE-ID")
	if instances != 2 {
		t.Fatalf("FAIL: expanded response contains %d RECURRENCE-ID instances, want exactly 2 (the limit)", instances)
	}
	if !strings.Contains(respBody, "20260301T090000Z") || !strings.Contains(respBody, "20260302T090000Z") {
		t.Fatalf("FAIL: the first two daily instances (Mar 1, Mar 2 09:00Z) are missing from the expanded response")
	}
	if strings.Contains(respBody, "20260303T090000Z") {
		t.Fatalf("FAIL: the response contains a third instance despite limit=2")
	}
}

// TestCalendarQueryNonRecurringExpandControl: a non-recurring event whose
// base start lies inside the expand window is still reported (control for
// the expansion path).
func TestCalendarQueryNonRecurringExpandControl(t *testing.T) {
	server := rruleExpandServer(t)
	rrulePut(t, server, "/dav/calendars/cal-1/plain-in",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:plain-in\r\nSUMMARY:plain inside\r\nDTSTART:20260302T120000Z\r\nDTEND:20260302T130000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

	reqBody := `<x:calendar-query xmlns:x="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:">` +
		`<d:prop><x:calendar-data><x:expand start="20260301T000000Z" end="20260305T000000Z"/></x:calendar-data></d:prop>` +
		`<x:filter><x:comp-filter name="VCALENDAR"><x:comp-filter name="VEVENT"/></x:comp-filter></x:filter>` +
		`</x:calendar-query>`
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(reqBody))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("CONTROL FAILED: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "plain-in") {
		t.Fatalf("CONTROL FAILED: the non-recurring event inside the expand window was not reported")
	}
}
