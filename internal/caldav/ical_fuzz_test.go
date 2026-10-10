package caldav

import (
	"encoding/xml"
	"testing"
	"time"
)

func FuzzICalendarParsing(f *testing.F) {
	f.Add("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nDTSTART;TZID=Europe/Istanbul:20260101T100000\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=5\r\nEXDATE:20260105T100000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	f.Add("BEGIN:VCALENDAR\nBEGIN:VTODO\nDUE:20260101\nDURATION:P1DT2H\nEND:VTODO\nEND:VCALENDAR")
	f.Add("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=-1,31;INTERVAL=1000000000000\r\n")
	ws, we := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, s string) {
		_ = validateCalendarData(s)
		_ = extractUIDFromICS(s)
		blocks := extractComponentBlocks(s, "VEVENT")
		_ = eventOccurrences(blocks, ws, we, true)
		cf := &CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VEVENT",
			TimeRange:   &TimeRange{Start: "20260101T000000Z", End: "20260601T000000Z"},
			PropFilters: []PropFilter{{Name: "SUMMARY", TextMatch: &TextMatch{Value: "x"}}}}}}
		_ = compFilterMatches(cf, s)
		for _, b := range blocks {
			if st, en, ok := componentTimeRange(b); ok {
				_ = buildExpandedICS(s, []time.Time{st}, en.Sub(st), blocks)
			}
		}
		_ = parseRRULEValue(s)
		_, _ = parseICSDuration(s)
	})
}

func FuzzXMLRequestDecoding(f *testing.F) {
	f.Add(`<c:calendar-query xmlns:c="urn:ietf:params:xml:ns:caldav"><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="20260101T000000Z" end="20260201T000000Z"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`)
	f.Add(`<propertyupdate xmlns="DAV:"><set><prop><displayname>x</displayname></prop></set></propertyupdate>`)
	f.Add(`<propfind xmlns="DAV:"><prop><getetag/></prop></propfind>`)
	f.Fuzz(func(t *testing.T, s string) {
		var q CalendarQuery
		_ = xml.Unmarshal([]byte(s), &q)
		if q.Filter != nil {
			_ = supportedCompFilter(q.Filter.CompFilter)
			_ = compFilterMatches(q.Filter.CompFilter, "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:a\r\nEND:VEVENT\r\nEND:VCALENDAR")
		}
		var pf Propfind
		_ = xml.Unmarshal([]byte(s), &pf)
		_ = requestedProps(&pf)
		_, _ = parsePropertyUpdate([]byte(s))
		_ = reportRoot([]byte(s))
	})
}
