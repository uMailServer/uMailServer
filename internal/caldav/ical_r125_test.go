package caldav

// Round 125 regression tests (F6070-F6079): iCalendar validation on PUT and
// recurrence/time-range correctness for calendar-query and free-busy.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func r125Do(t *testing.T, s *Server, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetBasicAuth(user, "pw")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func r125Cal(uid, inner string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\n" + inner + "END:VEVENT\r\nEND:VCALENDAR\r\n"
}

func r125TimeRangeFilter(start, end string) *CompFilter {
	return &CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VEVENT", TimeRange: &TimeRange{Start: start, End: end}}}}
}

func TestPutRejectsInvalidCalendarData_F6070_F6071(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	if w := r125Do(t, s, "MKCALENDAR", "/dav/calendars/c", "alice", ""); w.Code != http.StatusCreated {
		t.Fatalf("harness MKCALENDAR=%d", w.Code)
	}
	bad := map[string]string{
		"truncated":      "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:e\r\nSUMMARY:x\r\n",
		"no-end-vcal":    "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nEND:VEVENT\r\n",
		"mismatched":     "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nEND:VTODO\r\nEND:VCALENDAR\r\n",
		"no-component":   "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n",
		"missing-uid":    "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"two-uids":       "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:f\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"leading-junk":   "junk\r\nBEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"trailing-after": "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nEND:VEVENT\r\nEND:VCALENDAR\r\nBEGIN:VCALENDAR\r\n",
	}
	for name, body := range bad {
		w := r125Do(t, s, "PUT", "/dav/calendars/c/e", "alice", body)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "valid-calendar-data") {
			t.Errorf("%s: PUT=%d body=%q, want 403 valid-calendar-data", name, w.Code, w.Body.String())
		}
	}
	// Controls: LF endings, folded UID, recurrence overrides sharing a UID, VTIMEZONE.
	good := map[string]string{
		"lf":       "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:e\nEND:VEVENT\nEND:VCALENDAR\n",
		"folded":   "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:\r\n e\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		"override": "BEGIN:VCALENDAR\r\nBEGIN:VTIMEZONE\r\nTZID:X\r\nBEGIN:STANDARD\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\nBEGIN:VEVENT\r\nUID:e\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:e\r\nRECURRENCE-ID:20260101T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
	}
	for name, body := range good {
		if w := r125Do(t, s, "PUT", "/dav/calendars/c/e", "alice", body); w.Code != http.StatusCreated {
			t.Errorf("%s: PUT=%d, want 201", name, w.Code)
		}
	}
}

func TestTimeRangeHonorsExdate_F6072(t *testing.T) {
	ics := r125Cal("e", "DTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEXDATE:20260102T100000Z\r\n")
	if compFilterMatches(r125TimeRangeFilter("20260102T000000Z", "20260103T000000Z"), ics) {
		t.Error("excluded 2026-01-02 instance matched the time-range")
	}
	if !compFilterMatches(r125TimeRangeFilter("20260103T000000Z", "20260104T000000Z"), ics) {
		t.Error("control: 2026-01-03 instance must match")
	}
}

func TestFreeBusyHonorsExdateAndOverride_F6072_F6075(t *testing.T) {
	s := NewServer(t.TempDir(), nil)
	r125Do(t, s, "MKCALENDAR", "/dav/calendars/c", "alice", "")
	body := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEXDATE:20260102T100000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:e\r\nRECURRENCE-ID:20260103T100000Z\r\nDTSTART:20260103T150000Z\r\nDTEND:20260103T160000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if w := r125Do(t, s, "PUT", "/dav/calendars/c/e", "alice", body); w.Code != http.StatusCreated {
		t.Fatalf("PUT=%d", w.Code)
	}
	q := `<?xml version="1.0"?><c:free-busy-query xmlns:c="urn:ietf:params:xml:ns:caldav"><c:time-range start="20260101T000000Z" end="20260105T000000Z"/></c:free-busy-query>`
	w := r125Do(t, s, "REPORT", "/dav/calendars/c/", "alice", q)
	out := w.Body.String()
	want := []string{"20260101T100000Z/20260101T110000Z", "20260103T150000Z/20260103T160000Z"}
	for _, x := range want {
		if !strings.Contains(out, x) {
			t.Errorf("missing busy period %s in:\n%s", x, out)
		}
	}
	for _, x := range []string{"20260102T100000Z", "20260103T100000Z"} {
		if strings.Contains(out, x) {
			t.Errorf("stale busy period %s present in:\n%s", x, out)
		}
	}
}

func TestTimeRangeHonorsTZID_F6073(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata")
	}
	_ = loc
	// 09:00 New York (EST, UTC-5) is 14:00 UTC.
	ics := r125Cal("e", "DTSTART;TZID=America/New_York:20260115T090000\r\nDTEND;TZID=America/New_York:20260115T100000\r\n")
	if compFilterMatches(r125TimeRangeFilter("20260115T090000Z", "20260115T100000Z"), ics) {
		t.Error("event at 14:00Z matched a 09:00Z-10:00Z range (TZID ignored)")
	}
	if !compFilterMatches(r125TimeRangeFilter("20260115T140000Z", "20260115T150000Z"), ics) {
		t.Error("event at 14:00Z must match 14:00Z-15:00Z")
	}
}

func TestDailyTZIDKeepsWallClockAcrossDST_F6073(t *testing.T) {
	if _, err := time.LoadLocation("America/New_York"); err != nil {
		t.Skip("no tzdata")
	}
	// Daily 09:00 NY; DST starts 2026-03-08. On 03-09 the event is 13:00Z.
	ics := r125Cal("e", "DTSTART;TZID=America/New_York:20260301T090000\r\nDTEND;TZID=America/New_York:20260301T100000\r\nRRULE:FREQ=DAILY\r\n")
	if !compFilterMatches(r125TimeRangeFilter("20260309T130000Z", "20260309T140000Z"), ics) {
		t.Error("post-DST instance should be at 13:00Z")
	}
	if compFilterMatches(r125TimeRangeFilter("20260309T140000Z", "20260309T150000Z"), ics) {
		t.Error("post-DST instance must not stay at 14:00Z")
	}
}

func TestRRuleByDayAndByMonthDay_F6074(t *testing.T) {
	// 2026-01-05 is a Monday. Weekly on MO,WE.
	ics := r125Cal("e", "DTSTART:20260105T100000Z\r\nDTEND:20260105T110000Z\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE\r\n")
	if !compFilterMatches(r125TimeRangeFilter("20260107T000000Z", "20260108T000000Z"), ics) {
		t.Error("Wednesday instance missing")
	}
	if compFilterMatches(r125TimeRangeFilter("20260106T000000Z", "20260107T000000Z"), ics) {
		t.Error("Tuesday must not match")
	}
	m := r125Cal("m", "DTSTART:20260105T100000Z\r\nDTEND:20260105T110000Z\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=5,20\r\n")
	if !compFilterMatches(r125TimeRangeFilter("20260320T000000Z", "20260321T000000Z"), m) {
		t.Error("20th of month instance missing")
	}
	if compFilterMatches(r125TimeRangeFilter("20260310T000000Z", "20260311T000000Z"), m) {
		t.Error("10th of month must not match")
	}
	// Ordinal BYDAY is supported since round 134; unsupported parts stay nil.
	if parseRRULEValue("FREQ=MONTHLY;BYDAY=1MO") == nil {
		t.Error("ordinal BYDAY must parse")
	}
	if parseRRULEValue("FREQ=DAILY;BYHOUR=9") != nil {
		t.Error("BYHOUR must stay unsupported")
	}
}

func TestOverrideReplacesMasterInstance_F6075(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:e\r\nDTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:e\r\nRECURRENCE-ID:20260102T100000Z\r\nDTSTART:20260102T200000Z\r\nDTEND:20260102T210000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if compFilterMatches(r125TimeRangeFilter("20260102T100000Z", "20260102T110000Z"), ics) {
		t.Error("moved instance still matches at its original time")
	}
	if !compFilterMatches(r125TimeRangeFilter("20260102T200000Z", "20260102T210000Z"), ics) {
		t.Error("override must match at its new time")
	}
}

func TestPropFilterIgnoresNestedAlarm_F6076(t *testing.T) {
	ics := r125Cal("e", "DTSTART:20260101T100000Z\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:wake up\r\nEND:VALARM\r\n")
	cf := &CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VEVENT", PropFilters: []PropFilter{{Name: "DESCRIPTION"}}}}}
	if compFilterMatches(cf, ics) {
		t.Error("VALARM DESCRIPTION satisfied a VEVENT prop-filter")
	}
	cf.CompFilters[0].PropFilters[0] = PropFilter{Name: "DESCRIPTION", IsNotDefined: &struct{}{}}
	if !compFilterMatches(cf, ics) {
		t.Error("is-not-defined DESCRIPTION must match a VEVENT with only an alarm description")
	}
}

func TestLongRunningDailyEventStillExpanded_F6077(t *testing.T) {
	// 20 years of daily instances precede the window; the 5000-iteration cap
	// previously stopped expansion before reaching it.
	ics := r125Cal("e", "DTSTART:20000101T100000Z\r\nDTEND:20000101T110000Z\r\nRRULE:FREQ=DAILY\r\n")
	if !compFilterMatches(r125TimeRangeFilter("20260301T000000Z", "20260302T000000Z"), ics) {
		t.Error("daily event from 2000 missing in 2026")
	}
	if got := len(rruleInstances(mustTime("20000101T100000Z"), mustTime("20000101T110000Z"), parseRRULEValue("FREQ=DAILY"), mustTime("20260301T000000Z"), mustTime("20990101T000000Z"))); got != maxRRULEInstances {
		t.Errorf("unbounded window must be capped at %d, got %d", maxRRULEInstances, got)
	}
}

func mustTime(v string) time.Time {
	t, ok := parseICSTime(v)
	if !ok {
		panic(v)
	}
	return t
}

func TestUntilDateInclusiveAndVTodoDue_F6078(t *testing.T) {
	ics := r125Cal("e", "DTSTART:20260101T100000Z\r\nDTEND:20260101T110000Z\r\nRRULE:FREQ=DAILY;UNTIL=20260103\r\n")
	if !compFilterMatches(r125TimeRangeFilter("20260103T000000Z", "20260104T000000Z"), ics) {
		t.Error("instance on the UNTIL date dropped")
	}
	todo := "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t\r\nDTSTART:20260101T100000Z\r\nDUE:20260105T100000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	cf := &CompFilter{Name: "VCALENDAR", CompFilters: []*CompFilter{{Name: "VTODO", TimeRange: &TimeRange{Start: "20260103T000000Z", End: "20260104T000000Z"}}}}
	if !compFilterMatches(cf, todo) {
		t.Error("VTODO DTSTART..DUE span must cover 2026-01-03")
	}
	dueOnly := "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t\r\nDUE:20260105T100000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	cf.CompFilters[0].TimeRange = &TimeRange{Start: "20260105T000000Z", End: "20260106T000000Z"}
	if !compFilterMatches(cf, dueOnly) {
		t.Error("DUE-only VTODO must match a range containing DUE")
	}
}

func TestUsernameCannotEscapeNamespace_F6079(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir, nil)
	for _, u := range []string{"../evil", "a/b", "..", "x\\y"} {
		w := r125Do(t, s, "MKCALENDAR", "/dav/calendars/c", u, "")
		if w.Code != http.StatusForbidden {
			t.Errorf("username %q: MKCALENDAR=%d, want 403", u, w.Code)
		}
	}
}
