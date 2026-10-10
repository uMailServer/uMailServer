package caldav

import (
	"strings"
	"testing"
	"time"
)

func rrExpand(t *testing.T, start, rule, ws, we string) []string {
	t.Helper()
	r := parseRRULEValue(rule)
	if r == nil {
		t.Fatalf("rule %q did not parse", rule)
	}
	st := mustTime(start)
	var out []string
	for _, i := range rruleInstances(st, st.Add(time.Hour), r, mustTime(ws), mustTime(we)) {
		out = append(out, i.UTC().Format("20060102"))
	}
	return out
}

func rrEq(t *testing.T, name string, got []string, want string) {
	t.Helper()
	if g := strings.Join(got, ","); g != want {
		t.Errorf("%s:\n got  %s\n want %s", name, g, want)
	}
}

func TestRRULEOrdinalAndSetPos_R134(t *testing.T) {
	W0, W1 := "20260101T000000Z", "20270101T000000Z"
	rrEq(t, "1st Monday monthly", rrExpand(t, "20260105T090000Z", "FREQ=MONTHLY;BYDAY=1MO;COUNT=4", W0, W1),
		"20260105,20260202,20260302,20260406")
	rrEq(t, "last Friday monthly", rrExpand(t, "20260130T090000Z", "FREQ=MONTHLY;BYDAY=-1FR;COUNT=3", W0, W1),
		"20260130,20260227,20260327")
	rrEq(t, "last weekday via setpos", rrExpand(t, "20260130T090000Z", "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1;COUNT=3", W0, W1),
		"20260130,20260227,20260331")
	rrEq(t, "2nd and 4th Tue", rrExpand(t, "20260113T090000Z", "FREQ=MONTHLY;BYDAY=2TU,4TU;COUNT=4", W0, W1),
		"20260113,20260127,20260210,20260224")
	rrEq(t, "yearly 20th Monday", rrExpand(t, "20260518T090000Z", "FREQ=YEARLY;BYDAY=20MO;COUNT=2", W0, "20290101T000000Z"),
		"20260518,20270517")
	rrEq(t, "yearly thanksgiving", rrExpand(t, "20261126T090000Z", "FREQ=YEARLY;BYMONTH=11;BYDAY=4TH;COUNT=2", W0, "20290101T000000Z"),
		"20261126,20271125")
}

func TestRRULEByMonthYearDayWeekNo_R134(t *testing.T) {
	W0, W1 := "20260101T000000Z", "20300101T000000Z"
	rrEq(t, "yearly BYMONTH keeps day", rrExpand(t, "20260115T090000Z", "FREQ=YEARLY;BYMONTH=1,6;COUNT=4", W0, W1),
		"20260115,20260615,20270115,20270615")
	rrEq(t, "BYYEARDAY", rrExpand(t, "20260101T090000Z", "FREQ=YEARLY;BYYEARDAY=1,100,-1;COUNT=4", W0, W1),
		"20260101,20260410,20261231,20270101")
	rrEq(t, "BYWEEKNO 20 Monday default weekday", rrExpand(t, "20260511T090000Z", "FREQ=YEARLY;BYWEEKNO=20;COUNT=2", W0, W1),
		"20260511,20270517")
	rrEq(t, "BYWEEKNO+BYDAY", rrExpand(t, "20260101T090000Z", "FREQ=YEARLY;BYWEEKNO=1;BYDAY=MO,SU;COUNT=3", W0, W1),
		"20260104,20270104,20270110")
	rrEq(t, "weekly byday wkst=SU", rrExpand(t, "20260104T090000Z", "FREQ=WEEKLY;INTERVAL=2;BYDAY=SU,MO;WKST=SU;COUNT=4", W0, W1),
		"20260104,20260105,20260118,20260119")
	rrEq(t, "daily bymonth", rrExpand(t, "20260130T090000Z", "FREQ=DAILY;BYMONTH=2;COUNT=3", W0, W1),
		"20260201,20260202,20260203")
}

func TestRRULELeapAndCount_R134(t *testing.T) {
	W0, W1 := "20240101T000000Z", "20400101T000000Z"
	rrEq(t, "yearly Feb 29", rrExpand(t, "20240229T090000Z", "FREQ=YEARLY;COUNT=3", W0, W1),
		"20240229,20280229,20320229")
	rrEq(t, "monthly 31st", rrExpand(t, "20260131T090000Z", "FREQ=MONTHLY;COUNT=4", "20260101T000000Z", W1),
		"20260131,20260331,20260531,20260731")
	rrEq(t, "monthly -1 bymonthday", rrExpand(t, "20260131T090000Z", "FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=3", "20260101T000000Z", W1),
		"20260131,20260228,20260331")
	rrEq(t, "yearly last day of Feb", rrExpand(t, "20240229T090000Z", "FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=-1;COUNT=3", W0, W1),
		"20240229,20250228,20260228")
}

func TestRRULECountWithOverridesAndExdate_R134(t *testing.T) {
	// COUNT counts excluded/overridden instances too (RFC 5545 §3.3.10).
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:c\r\nDTSTART:20260105T090000Z\r\nDTEND:20260105T100000Z\r\nRRULE:FREQ=WEEKLY;COUNT=4\r\nEXDATE:20260112T090000Z\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:c\r\nRECURRENCE-ID:20260119T090000Z\r\nDTSTART:20260120T090000Z\r\nDTEND:20260120T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	occ := eventOccurrences(extractComponentBlocks(ics, "VEVENT"), mustTime("20260101T000000Z"), mustTime("20270101T000000Z"), false)
	var got []string
	for _, o := range occ {
		got = append(got, o.start.UTC().Format("20060102"))
	}
	rrEq(t, "count with exdate+override", got, "20260105,20260120,20260126")
}

func TestRRULEBounded_R134(t *testing.T) {
	r := parseRRULEValue("FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30")
	st := mustTime("20260101T000000Z")
	start := time.Now()
	n := len(rruleInstances(st, st.Add(time.Hour), r, mustTime("20000101T000000Z"), mustTime("99990101T000000Z")))
	if n != 0 || time.Since(start) > 3*time.Second {
		t.Fatalf("never-matching rule: n=%d took %v", n, time.Since(start))
	}
	for _, bad := range []string{"FREQ=WEEKLY;BYDAY=1MO", "FREQ=MONTHLY;BYWEEKNO=3", "FREQ=DAILY;BYYEARDAY=3", "FREQ=YEARLY;BYMONTH=13", "FREQ=YEARLY;BYWEEKNO=0", "FREQ=MONTHLY;BYSETPOS=0"} {
		if parseRRULEValue(bad) != nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}
