package caldav

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F7130: RFC 4791 §9.9 allows time-range with only start or only end.
func TestOpenEndedTimeRange_F7130(t *testing.T) {
	server := calendarQueryFilterServer(t)
	cases := map[string]struct {
		attrs   string
		wantIn  bool
		wantOut bool
	}{
		"start only": {`start="20260401T000000Z"`, false, true},
		"end only":   {`end="20260401T000000Z"`, true, false},
	}
	for name, c := range cases {
		w := calendarQueryPOST(t, server, eventQueryXML(`<c:comp-filter name="VEVENT"><c:time-range `+c.attrs+`/></c:comp-filter>`))
		if w.Code != http.StatusMultiStatus {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		b := w.Body.String()
		if strings.Contains(b, "evt-in-range") != c.wantIn || strings.Contains(b, "evt-out-range") != c.wantOut {
			t.Errorf("%s: wrong match set: %s", name, b)
		}
	}
}

func TestOpenEndedTimeRangeRecurring_F7130(t *testing.T) {
	server := calendarQueryFilterServer(t)
	req := httptest.NewRequest(http.MethodPut, "/dav/calendars/cal-1/daily", strings.NewReader(
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//p//EN\r\nBEGIN:VEVENT\r\nUID:daily\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	req.SetBasicAuth("alice", "pw")
	server.ServeHTTP(httptest.NewRecorder(), req)
	for _, attrs := range []string{`start="20300101T000000Z"`, `end="20200101T000000Z"`} {
		w := calendarQueryPOST(t, server, eventQueryXML(`<c:comp-filter name="VEVENT"><c:time-range `+attrs+`/></c:comp-filter>`))
		has := strings.Contains(w.Body.String(), "daily")
		if w.Code != http.StatusMultiStatus || has != strings.HasPrefix(attrs, "start") {
			t.Errorf("%s: code %d has=%v", attrs, w.Code, has)
		}
	}
	w := calendarQueryPOST(t, server, eventQueryXML(`<c:comp-filter name="VEVENT"><c:time-range/></c:comp-filter>`))
	if w.Code != http.StatusForbidden {
		t.Errorf("empty time-range: %d", w.Code)
	}
}
