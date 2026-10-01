package caldav

// Regression tests for the CalDAV href round-trip defect: the PROPFIND href
// builders used to advertise "/dav/calendars/{username}/{calendarID}/..."
// while every item handler parses the request convention
// "/dav/calendars/{calendarID}[/{eventUID}]" (the authenticated username
// scopes storage and is not part of the URL). RFC 4918 §5.3/§8.3 requires
// clients to operate on the hrefs the server returns, so a listed event was
// not fetchable (GET parsed the username as calendarID and returned 403) and
// the advertised calendar-home-set PROPFINDed empty.
//
// Note: fixtures use LF line endings; extractUIDFromICS currently keeps a
// trailing \r on CRLF lines, which is a separate known defect.

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const roundTripICS = "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//test//EN\nBEGIN:VEVENT\nUID:evt-1\nDTSTART:20260101T100000Z\nSUMMARY:Round Trip Event\nEND:VEVENT\nEND:VCALENDAR\n"

type hrefRoundTripResponse struct {
	Href string `xml:"href"`
}

type hrefRoundTripMultistatus struct {
	Responses []hrefRoundTripResponse `xml:"response"`
}

func hrefRoundTripServer(t *testing.T) *Server {
	t.Helper()
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return username == "alice" && password == "pw", nil
	})
	return server
}

func hrefRoundTripRequest(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pw")))
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	return w
}

func hrefRoundTripHrefs(t *testing.T, body []byte) []string {
	t.Helper()
	var ms hrefRoundTripMultistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		t.Fatalf("cannot parse multistatus: %v\nbody:\n%s", err, body)
	}
	hrefs := make([]string, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		hrefs = append(hrefs, r.Href)
	}
	return hrefs
}

// TestHrefRoundTrip_ListedEventIsFetchable drives the standard client flow:
// create, list calendars, list events in the advertised collection, then fetch
// the event at the exact href the server returned (RFC 4918 §8.3).
func TestHrefRoundTrip_ListedEventIsFetchable(t *testing.T) {
	server := hrefRoundTripServer(t)

	if w := hrefRoundTripRequest(t, server, "MKCALENDAR", "/dav/calendars/work-cal", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR = %d, want %d", w.Code, http.StatusCreated)
	}
	if w := hrefRoundTripRequest(t, server, "PUT", "/dav/calendars/work-cal/evt-1", roundTripICS); w.Code != http.StatusCreated {
		t.Fatalf("PUT = %d, want %d", w.Code, http.StatusCreated)
	}

	// Control: the item-handler convention must keep working.
	w := hrefRoundTripRequest(t, server, "GET", "/dav/calendars/work-cal/evt-1", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "UID:evt-1") {
		t.Fatalf("control GET /dav/calendars/work-cal/evt-1 = %d, want 200 with event body", w.Code)
	}

	// List calendars and find the advertised collection href.
	w = hrefRoundTripRequest(t, server, "PROPFIND", "/dav/calendars/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /dav/calendars/ = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	var calHref string
	for _, h := range hrefRoundTripHrefs(t, w.Body.Bytes()) {
		if strings.HasSuffix(h, "/work-cal/") {
			calHref = h
			break
		}
	}
	if calHref == "" {
		t.Fatalf("PROPFIND /dav/calendars/ did not advertise the calendar; hrefs=%v", hrefRoundTripHrefs(t, w.Body.Bytes()))
	}
	if calHref != "/dav/calendars/work-cal/" {
		t.Errorf("calendar href = %q, want /dav/calendars/work-cal/ (request convention)", calHref)
	}

	// List events in the advertised collection.
	w = hrefRoundTripRequest(t, server, "PROPFIND", calHref, "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND %q = %d, want %d", calHref, w.Code, http.StatusMultiStatus)
	}
	var evtHref string
	for _, h := range hrefRoundTripHrefs(t, w.Body.Bytes()) {
		if strings.HasSuffix(h, "/evt-1") {
			evtHref = h
			break
		}
	}
	if evtHref == "" {
		t.Fatalf("PROPFIND %q did not list the event; hrefs=%v", calHref, hrefRoundTripHrefs(t, w.Body.Bytes()))
	}

	// The contract: the listed event must be fetchable at its advertised href.
	w = hrefRoundTripRequest(t, server, "GET", evtHref, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET advertised href %q = %d, want 200", evtHref, w.Code)
	}
	if !strings.Contains(w.Body.String(), "UID:evt-1") {
		t.Errorf("GET advertised href %q returned wrong body: %q", evtHref, w.Body.String())
	}
}

// TestHrefRoundTrip_HomeSetDiscoversCalendars verifies the calendar-home-set
// advertised by the principal response lists the user's calendars when
// PROPFINDed (RFC 4791 §6.2.1 discovery).
func TestHrefRoundTrip_HomeSetDiscoversCalendars(t *testing.T) {
	server := hrefRoundTripServer(t)

	if w := hrefRoundTripRequest(t, server, "MKCALENDAR", "/dav/calendars/work-cal", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR = %d, want %d", w.Code, http.StatusCreated)
	}

	w := hrefRoundTripRequest(t, server, "PROPFIND", "/dav/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /dav/ = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	body := w.Body.String()
	marker := strings.Index(body, "calendar-home-set")
	if marker == -1 {
		t.Fatalf("principal response has no calendar-home-set:\n%s", body)
	}
	urlStart := strings.Index(body[marker:], "/dav/calendars/")
	if urlStart == -1 {
		t.Fatalf("calendar-home-set contains no /dav/calendars/ URL:\n%s", body)
	}
	rest := body[marker+urlStart:]
	homeSet := rest
	if end := strings.IndexAny(rest, "&<"); end != -1 {
		homeSet = rest[:end]
	}

	w = hrefRoundTripRequest(t, server, "PROPFIND", homeSet, "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND advertised home-set %q = %d, want %d", homeSet, w.Code, http.StatusMultiStatus)
	}
	if !strings.Contains(w.Body.String(), "Calendar") {
		t.Errorf("PROPFIND advertised home-set %q returned no calendars; body:\n%s", homeSet, w.Body.String())
	}
}

// TestHandleCalendarPropfind_ConventionBoundaries pins the request-convention
// parsing of per-collection PROPFINDs: trailing slash, specific event href,
// and the ownership gate for unknown calendars.
func TestHandleCalendarPropfind_ConventionBoundaries(t *testing.T) {
	server := hrefRoundTripServer(t)

	cal := &Calendar{ID: "work-cal", Name: "Work Calendar"}
	if err := server.storage.CreateCalendar("alice", cal); err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	if err := server.storage.SaveEvent("alice", "work-cal", &CalendarEvent{UID: "evt-1"}, roundTripICS); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	// Calendar collection with trailing slash lists the calendar and its events.
	w := hrefRoundTripRequest(t, server, "PROPFIND", "/dav/calendars/work-cal/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND trailing slash = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Work Calendar") || !strings.Contains(body, "evt-1") {
		t.Errorf("PROPFIND /dav/calendars/work-cal/ should list calendar and events; body:\n%s", body)
	}

	// Specific event href returns exactly that event.
	w = hrefRoundTripRequest(t, server, "PROPFIND", "/dav/calendars/work-cal/evt-1", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND event href = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	hrefs := hrefRoundTripHrefs(t, w.Body.Bytes())
	if len(hrefs) != 1 || !strings.HasSuffix(hrefs[0], "/work-cal/evt-1") {
		t.Errorf("PROPFIND specific event hrefs = %v, want one /dav/calendars/work-cal/evt-1", hrefs)
	}

	// Unknown calendar: 207 with no resource responses (ownership gate holds).
	w = hrefRoundTripRequest(t, server, "PROPFIND", "/dav/calendars/missing-cal/", "")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND unknown calendar = %d, want %d", w.Code, http.StatusMultiStatus)
	}
	if hrefs := hrefRoundTripHrefs(t, w.Body.Bytes()); len(hrefs) != 0 {
		t.Errorf("PROPFIND unknown calendar returned responses %v, want none", hrefs)
	}
}
