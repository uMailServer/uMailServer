package caldav

// Regression tests for the REPORT prop-filter extension (RFC 4791 §9.9.2):
// prop-filters now match component properties — presence, text-match with
// collation and negate-condition (§9.9.4), and is-not-defined (§9.9.3).
// Genuinely unsupported prop-filter shapes (param-filters, is-not-defined
// combined with text-match, unknown collations) still answer the
// CALDAV:supported-filter precondition (§3.11) instead of being silently
// ignored.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func propFilterAuditServer(t *testing.T) *Server {
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

	put("/dav/calendars/cal-1/evt-standup",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:evt-standup\r\nSUMMARY:standup meeting\r\nLOCATION:room 4\r\nDTSTART:20260310T100000Z\r\nDTEND:20260310T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	put("/dav/calendars/cal-1/evt-retro",
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//proof//EN\r\nBEGIN:VEVENT\r\nUID:evt-retro\r\nSUMMARY:retro planning\r\nDTSTART:20260311T100000Z\r\nDTEND:20260311T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")

	return server
}

func propFilterPOST(t *testing.T, server *Server, xmlBody string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("REPORT", "/dav/calendars/cal-1", strings.NewReader(xmlBody))
	req.SetBasicAuth("alice", "pw")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

func propFilterEventQuery(inner string) string {
	return `<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
		`<d:prop><d:getetag/><c:calendar-data/></d:prop>` +
		`<c:filter><c:comp-filter name="VCALENDAR">` + inner + `</c:comp-filter></c:filter>` +
		`</c:calendar-query>`
}

// TestCalendarQueryPropFilterTextMatchSelects: a prop-filter with text-match
// selects only events whose property contains the needle (the default
// collation is case-insensitive).
func TestCalendarQueryPropFilterTextMatchSelects(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:text-match>standup</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: the matching event is missing from the REPORT response")
	}
	if strings.Contains(w.Body.String(), "evt-retro") {
		t.Fatalf("FAIL: a non-matching event was returned by the prop-filter")
	}
}

// TestCalendarQueryPropFilterNegateCondition: negate-condition inverts the
// text-match result.
func TestCalendarQueryPropFilterNegateCondition(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:text-match negate-condition="yes">standup</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "evt-retro") {
		t.Fatalf("FAIL: the negated filter dropped the non-matching event")
	}
	if strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: the negated filter returned the matching event")
	}
}

// TestCalendarQueryPropFilterOctetCollationIsCaseSensitive: i;octet matches
// bytes, so a case-differing needle does not match.
func TestCalendarQueryPropFilterOctetCollationCaseSensitive(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:text-match collation="i;octet">Standup</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: i;octet matched case-insensitively")
	}
}

// TestCalendarQueryPropFilterDefaultCollationIsCaseInsensitive: the default
// (casemap) collation matches across case.
func TestCalendarQueryPropFilterDefaultCollationIsCaseInsensitive(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:text-match>STANDUP</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: the default collation did not match across case")
	}
}

// TestCalendarQueryPropFilterMissingPropertyMatchesNothing: a text-match over
// a property the event lacks fails the filter.
func TestCalendarQueryPropFilterMissingPropertyMatchesNothing(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="LOCATION"><c:text-match>room</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if strings.Contains(w.Body.String(), "evt-retro") {
		t.Fatalf("FAIL: an event without the property passed a text-match prop-filter")
	}
	if !strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: the event carrying the property was dropped")
	}
}

// TestCalendarQueryPropFilterIsNotDefined: is-not-defined selects events
// lacking the property.
func TestCalendarQueryPropFilterIsNotDefined(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="LOCATION"><c:is-not-defined/></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("FAIL: REPORT = %d, want 207", w.Code)
	}
	if !strings.Contains(w.Body.String(), "evt-retro") {
		t.Fatalf("FAIL: the event lacking the property was not selected by is-not-defined")
	}
	if strings.Contains(w.Body.String(), "evt-standup") {
		t.Fatalf("FAIL: an event having the property passed is-not-defined")
	}
}

// TestCalendarQueryPropFilterWithParamFilterStillUnsupported: param-filters
// remain unsupported and answer the supported-filter precondition (RFC 4791
// §3.11) instead of being silently ignored.
func TestCalendarQueryPropFilterWithParamFilterStillUnsupported(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="ATTENDEE"><c:param-filter name="PARTSTAT"><c:is-not-defined/></c:param-filter></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: REPORT = %d, want 403 (supported-filter)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "supported-filter") {
		t.Fatalf("FAIL: the 403 body lacks the CALDAV:supported-filter precondition")
	}
}

// TestCalendarQueryPropFilterMutuallyExclusiveShapeUnsupported: is-not-defined
// combined with text-match is invalid per RFC 4791 §9.9.2 and unsupported.
func TestCalendarQueryPropFilterMutuallyExclusiveShapeUnsupported(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:is-not-defined/><c:text-match>x</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: REPORT = %d, want 403 (supported-filter)", w.Code)
	}
}

// TestCalendarQueryPropFilterUnknownCollationUnsupported: an unknown
// collation is unsupported rather than silently case-folded.
func TestCalendarQueryPropFilterUnknownCollationUnsupported(t *testing.T) {
	server := propFilterAuditServer(t)

	w := propFilterPOST(t, server, propFilterEventQuery(
		`<c:comp-filter name="VEVENT">`+
			`<c:prop-filter name="SUMMARY"><c:text-match collation="i;bogus">standup</c:text-match></c:prop-filter>`+
			`</c:comp-filter>`))
	if w.Code != http.StatusForbidden {
		t.Fatalf("FAIL: REPORT = %d, want 403 (supported-filter)", w.Code)
	}
}
