package caldav

import (
	"net/http"
	"net/url"
	"testing"
)

// F6481: listed hrefs must be percent-encoded so a resource whose name holds
// '%', '?', '#' or a space is fetchable at the href the server returned.
func TestListedHrefIsPercentEncodedR168(t *testing.T) {
	server := hrefRoundTripServer(t)
	if w := hrefRoundTripRequest(t, server, "MKCALENDAR", "/dav/calendars/c", ""); w.Code != http.StatusCreated {
		t.Fatalf("MKCALENDAR = %d", w.Code)
	}
	name := "a%b?c d#e"
	ics := "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//t//EN\nBEGIN:VEVENT\nUID:u1\nDTSTART:20260101T100000Z\nSUMMARY:x\nEND:VEVENT\nEND:VCALENDAR\n"
	put := (&url.URL{Path: "/dav/calendars/c/" + name}).EscapedPath()
	if w := hrefRoundTripRequest(t, server, "PUT", put, ics); w.Code != http.StatusCreated {
		t.Fatalf("PUT %s = %d %s", put, w.Code, w.Body.String())
	}
	for _, req := range []struct{ method, path, body string }{
		{"PROPFIND", "/dav/calendars/c/", ""},
		{"REPORT", "/dav/calendars/c/", `<c:calendar-query xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:d="DAV:"><d:prop><d:getetag/></d:prop><c:filter><c:comp-filter name="VCALENDAR"/></c:filter></c:calendar-query>`},
	} {
		w := hrefRoundTripRequest(t, server, req.method, req.path, req.body)
		found := false
		for _, h := range hrefRoundTripHrefs(t, w.Body.Bytes()) {
			if h == "/dav/calendars/c/" {
				continue
			}
			found = true
			if h != put {
				t.Errorf("%s href = %q, want %q", req.method, h, put)
			}
		}
		if !found {
			t.Errorf("%s: no event href", req.method)
		}
	}
}
