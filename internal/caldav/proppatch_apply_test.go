package caldav

import (
	"net/http"
	"strings"
	"testing"
)

const proppatchSetBody = `<?xml version="1.0"?><D:propertyupdate xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:I="http://apple.com/ns/ical/"><D:set><D:prop><D:displayname>Renamed</D:displayname><C:calendar-description>New desc</C:calendar-description><I:calendar-color>#FF0000FF</I:calendar-color></D:prop></D:set></D:propertyupdate>`

// F5653: PROPPATCH must apply the change or report a per-property failure.
func TestCalDAVProppatchF5653Control(t *testing.T) {
	if w := davDo(davServer(t), "PROPPATCH", "/dav/calendars/missing", "", proppatchSetBody, true); w.Code != http.StatusForbidden {
		t.Fatalf("control: missing calendar code=%d, want 403", w.Code)
	}
}

func TestCalDAVProppatchF5653Failure(t *testing.T) {
	s := davServer(t)
	w := davDo(s, "PROPPATCH", "/dav/calendars/c1/", "", proppatchSetBody, true)
	cal, _ := s.storage.GetCalendar("alice", "c1")
	if cal.Name != "Renamed" || cal.Description != "New desc" || cal.Color != "#FF0000FF" {
		t.Errorf("DEFECT F5653\nEXPECTED: displayname/description/color applied (or a per-property failure reported)\nACTUAL: code=%d name=%q desc=%q color=%q body=%q", w.Code, cal.Name, cal.Description, cal.Color, w.Body.String())
	}
}

func TestCalDAVProppatchF5653Edges(t *testing.T) {
	s := davServer(t)
	w := davDo(s, "PROPPATCH", "/dav/calendars/c1/", "", proppatchSetBody, true)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("applied PROPPATCH code=%d, want 207\n%s", w.Code, w.Body.String())
	}
	root := parseNSXML(t, w.Body.Bytes())
	if root.Name.Space != testNSDAV || root.Name.Local != "multistatus" {
		t.Errorf("root=%v", root.Name)
	}
	// PROPFIND reflects the change.
	pf := davDo(s, "PROPFIND", "/dav/calendars/c1/", "0", "", true)
	if p := propOf(parseNSXML(t, pf.Body.Bytes()), "/dav/calendars/c1/", testNSDAV, "displayname"); p == nil || p.Text != "Renamed" {
		t.Errorf("PROPFIND displayname not updated:\n%s", pf.Body.String())
	}

	// A protected property fails the whole request atomically: 403 for it,
	// 424 for the others, nothing applied.
	s2 := davServer(t)
	bad := `<D:propertyupdate xmlns:D="DAV:"><D:set><D:prop><D:displayname>X</D:displayname><D:getetag>"1"</D:getetag></D:prop></D:set></D:propertyupdate>`
	w = davDo(s2, "PROPPATCH", "/dav/calendars/c1/", "", bad, true)
	if w.Code != http.StatusMultiStatus || !strings.Contains(w.Body.String(), "403") || !strings.Contains(w.Body.String(), "424") {
		t.Errorf("atomic failure code=%d body=%s", w.Code, w.Body.String())
	}
	if cal, _ := s2.storage.GetCalendar("alice", "c1"); cal.Name != "Work" {
		t.Errorf("failed PROPPATCH partially applied: name=%q", cal.Name)
	}

	// PROPPATCH on an event URL must not rename the calendar.
	s3 := davServer(t)
	w = davDo(s3, "PROPPATCH", "/dav/calendars/c1/e1", "", proppatchSetBody, true)
	if cal, _ := s3.storage.GetCalendar("alice", "c1"); cal.Name != "Work" {
		t.Errorf("event-URL PROPPATCH renamed the calendar (code=%d): name=%q", w.Code, cal.Name)
	}

	// Malformed or empty body is rejected, not silently accepted.
	if w := davDo(davServer(t), "PROPPATCH", "/dav/calendars/c1/", "", "<not-xml", true); w.Code != http.StatusBadRequest {
		t.Errorf("malformed body code=%d, want 400", w.Code)
	}
	// remove of displayname clears it.
	s4 := davServer(t)
	rm := `<D:propertyupdate xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><D:remove><D:prop><C:calendar-description/></D:prop></D:remove></D:propertyupdate>`
	if w := davDo(s4, "PROPPATCH", "/dav/calendars/c1/", "", rm, true); w.Code != http.StatusMultiStatus {
		t.Fatalf("remove code=%d", w.Code)
	}
	if cal, _ := s4.storage.GetCalendar("alice", "c1"); cal.Description != "" {
		t.Errorf("remove did not clear description: %q", cal.Description)
	}
}
