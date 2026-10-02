package caldav

// Regression tests for the CalendarQuery REPORT filter: handleReport parsed
// the calendar-query XML but never applied it — every event in the calendar
// was returned regardless of comp-filter or time-range (RFC 4791 §9.9), and
// unimplemented filter shapes were silently ignored instead of answered with
// the CALDAV:supported-filter precondition (RFC 4791 §3.11).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func calendarQueryFilterServer(t *testing.T) *Server {
	t.Helper()
	server := NewServer(t.TempDir(), nil)

	mk := httptest.NewRequest("MKCALENDAR", "/dav/calendars/cal-1", nil)
	mk.SetBasicAuth("alice", "pw")
	mkw := httptest.NewRecorder()
	server.ServeHTTP(mkw, mk)
	if mkw.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): MKCALENDAR = %d, want 201", mkw.Code)
	}

	put := func(path, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("CONTROL FAILED (harness): PUT %s = %d, want 2xx", path, w.Code)
		}
	}

	put("/dav/calendars/cal-1/evt-in-range",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:evt-in-range\r\nSUMMARY:in range\r\nDTSTART:20260310T100000Z\r\nDTEND:20260310T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	put("/dav/calendars/cal-1/evt-out-range",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:evt-out-range\r\nSUMMARY:out of range\r\nDTSTART:20260510T100000Z\r\nDTEND:20260510T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	put("/dav/calendars/cal-1/todo-1",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VTODO\r\nUID:todo-1\r\nSUMMARY:a todo\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")

	return server
}

func calendarQueryPOST(t *testing.T, server *Server, xmlBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(xmlBody))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

func eventQueryXML(inner string) string {
	return `<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR">` + inner + `</c:comp-filter></c:filter>` +
		`</c:calendar-query>`
}

// TestCalendarQueryTimeRangeFiltersEvents: the time-range on VEVENT must
// select only intersecting events.
func TestCalendarQueryTimeRangeFiltersEvents(t *testing.T) {
	server := calendarQueryFilterServer(t)

	body := eventQueryXML(`<c:comp-filter name="VEVENT">` +
		`<c:time-range start="20260301T000000Z" end="20260401T000000Z"/>` +
		`</c:comp-filter>`)
	w := calendarQueryPOST(t, server, body)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "evt-in-range") {
		t.Fatalf("FAIL: the in-range event is missing from the REPORT response")
	}
	if strings.Contains(w.Body.String(), "evt-out-range") {
		t.Fatalf("FAIL: the out-of-range event was returned despite the time-range filter (the filter was ignored)")
	}
	if strings.Contains(w.Body.String(), "todo-1") {
		t.Fatalf("FAIL: a VTODO was returned by a VEVENT filter")
	}
}

// TestCalendarQueryCompFilterSelectsComponentKind: a bare VEVENT comp-filter
// selects events only; a VTODO-only calendar object is excluded.
func TestCalendarQueryCompFilterSelectsComponentKind(t *testing.T) {
	server := calendarQueryFilterServer(t)

	body := eventQueryXML(`<c:comp-filter name="VEVENT"/>`)
	w := calendarQueryPOST(t, server, body)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if strings.Contains(w.Body.String(), "todo-1") {
		t.Fatalf("FAIL: a VTODO was returned by a VEVENT comp-filter (the filter was ignored)")
	}
	if !strings.Contains(w.Body.String(), "evt-in-range") || !strings.Contains(w.Body.String(), "evt-out-range") {
		t.Fatalf("FAIL: the VEVENT comp-filter dropped real events")
	}
}

// TestCalendarQueryWithoutFilterReturnsAll: no filter must preserve the
// unfiltered behavior.
func TestCalendarQueryWithoutFilterReturnsAll(t *testing.T) {
	server := calendarQueryFilterServer(t)

	body := eventQueryXML(``)
	w := calendarQueryPOST(t, server, body)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	for _, uid := range []string{"evt-in-range", "evt-out-range", "todo-1"} {
		if !strings.Contains(w.Body.String(), uid) {
			t.Fatalf("FAIL: unfiltered REPORT is missing %s", uid)
		}
	}
}

// TestCalendarQueryUnsupportedPropFilterRejected: property filters are not
// implemented, so the server must answer the CALDAV:supported-filter
// precondition (RFC 4791 §3.11) instead of silently ignoring the filter.
func TestCalendarQueryUnsupportedPropFilterRejected(t *testing.T) {
	server := calendarQueryFilterServer(t)

	// Since prop-filter matching landed, a bare prop-filter matches by
	// property presence (207). A prop-filter carrying a param-filter remains
	// unsupported and must still answer the supported-filter precondition.
	body := eventQueryXML(`<c:comp-filter name="VEVENT"><c:prop-filter name="ATTENDEE"><c:param-filter name="PARTSTAT"><c:is-not-defined/></c:param-filter></c:prop-filter></c:comp-filter>`)
	w := calendarQueryPOST(t, server, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: unsupported filter = %d, want 403 (the filter was silently ignored)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "supported-filter") {
		t.Fatalf("FAIL: 403 response lacks the CALDAV:supported-filter precondition")
	}
}

// TestCalendarQueryZeroLengthComponentMatchesInsideRange: a DTSTART-only
// (point) event matches a time-range whose interval contains the point
// (RFC 4791 §9.9.1 zero-length rule); a point outside the range is excluded.
func TestCalendarQueryZeroLengthComponentMatchesInsideRange(t *testing.T) {
	server := NewServer(t.TempDir(), nil)
	mk := httptest.NewRequest("MKCALENDAR", "/dav/calendars/cal-1", nil)
	mk.SetBasicAuth("alice", "pw")
	mkw := httptest.NewRecorder()
	server.ServeHTTP(mkw, mk)
	if mkw.Code != http.StatusCreated {
		t.Fatalf("CONTROL FAILED (harness): MKCALENDAR = %d, want 201", mkw.Code)
	}

	put := func(path, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.SetBasicAuth("alice", "pw")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("CONTROL FAILED (harness): PUT %s = %d, want 2xx", path, w.Code)
		}
	}

	// Point events: DTSTART only (DTEND absent = zero-length).
	put("/dav/calendars/cal-1/point-in",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:point-in\r\nSUMMARY:point inside\r\nDTSTART:20260315T120000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	put("/dav/calendars/cal-1/point-out",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:point-out\r\nSUMMARY:point outside\r\nDTSTART:20260515T120000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

	body := eventQueryXML(`<c:comp-filter name="VEVENT">` +
		`<c:time-range start="20260301T000000Z" end="20260401T000000Z"/>` +
		`</c:comp-filter>`)
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(body))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "point-in") {
		t.Fatalf("FAIL: the zero-length event inside the range was not matched (RFC 4791 §9.9.1)")
	}
	if strings.Contains(w.Body.String(), "point-out") {
		t.Fatalf("FAIL: the zero-length event outside the range was matched")
	}
}
