package caldav

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// unfoldLines splits iCalendar text into logical content lines: CRLF/LF line
// endings are stripped and RFC 5545 §3.1 folded continuations (a leading space
// or tab) are joined onto the previous line. Blank lines are dropped.
func unfoldLines(s string) []string {
	var out []string
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	return out
}

// splitPropLine splits "NAME;PARAM=V:value" into its name, parameter string
// and value. Colons inside quoted parameter values are not separators.
func splitPropLine(line string) (name, params, value string, ok bool) {
	inQuote := false
	colon := -1
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon < 0 {
		return "", "", "", false
	}
	head := line[:colon]
	value = line[colon+1:]
	if semi := strings.Index(head, ";"); semi >= 0 {
		name, params = head[:semi], head[semi+1:]
	} else {
		name = head
	}
	return strings.ToUpper(strings.TrimSpace(name)), params, value, true
}

// paramValue returns the value of a named parameter from a parameter string.
func paramValue(params, name string) string {
	for _, p := range strings.Split(params, ";") {
		k, v, found := strings.Cut(p, "=")
		if found && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

type propLine struct {
	params string
	value  string
}

// ownPropLines returns every top-level (not inside a nested component such as
// VALARM) property named name in the component body, unfolded.
func ownPropLines(body, name string) []propLine {
	var out []propLine
	nested := 0
	for _, line := range unfoldLines(body) {
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "BEGIN:"):
			nested++
			continue
		case strings.HasPrefix(up, "END:"):
			if nested > 0 {
				nested--
			}
			continue
		}
		if nested > 0 {
			continue
		}
		n, params, value, ok := splitPropLine(line)
		if ok && n == name {
			out = append(out, propLine{params, value})
		}
	}
	return out
}

// ownPropValue returns the first top-level property value (F6076: a VALARM's
// DESCRIPTION/ACTION must not satisfy a prop-filter on the VEVENT).
func ownPropValue(body, name string) (string, bool) {
	l := ownPropLines(body, name)
	if len(l) == 0 {
		return "", false
	}
	return strings.TrimSpace(l[0].value), true
}

// parseICSTimeProp parses a DATE / DATE-TIME property value honoring a TZID
// parameter (F6073). Floating values and unknown zones are treated as UTC.
func parseICSTimeProp(params, value string) (t time.Time, isDate, ok bool) {
	value = strings.TrimSpace(value)
	if tz := paramValue(params, "TZID"); tz != "" && !strings.HasSuffix(value, "Z") && len(value) != 8 {
		if loc, err := time.LoadLocation(tz); err == nil {
			if parsed, err := time.ParseInLocation("20060102T150405", value, loc); err == nil {
				return parsed, false, true
			}
			return time.Time{}, false, false
		}
	}
	t, ok = parseICSTime(value)
	return t, len(value) == 8, ok
}

// validateCalendarData performs the RFC 4791 §5.3.2.1 structural checks for a
// PUT body: a single balanced VCALENDAR, at least one calendar component, and
// one consistent non-empty UID across all of them (F6070, F6071).
func validateCalendarData(ics string) bool {
	lines := unfoldLines(ics)
	if len(lines) < 2 || !strings.EqualFold(lines[0], "BEGIN:VCALENDAR") || !strings.EqualFold(lines[len(lines)-1], "END:VCALENDAR") {
		return false
	}
	var stack []string
	comps := 0
	uid := ""
	for i, line := range lines {
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "BEGIN:"):
			name := strings.TrimSpace(up[len("BEGIN:"):])
			if name == "" || (len(stack) == 0 && i != 0) {
				return false
			}
			stack = append(stack, name)
			if len(stack) == 2 {
				switch name {
				case "VEVENT", "VTODO", "VJOURNAL", "VFREEBUSY":
					comps++
				}
			}
		case strings.HasPrefix(up, "END:"):
			name := strings.TrimSpace(up[len("END:"):])
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 && i != len(lines)-1 {
				return false
			}
		default:
			if len(stack) == 2 && (stack[1] == "VEVENT" || stack[1] == "VTODO" || stack[1] == "VJOURNAL") {
				if n, _, v, ok := splitPropLine(line); ok && n == "UID" {
					v = strings.TrimSpace(v)
					if v == "" || (uid != "" && v != uid) {
						return false
					}
					uid = v
				}
			}
		}
	}
	return len(stack) == 0 && comps > 0 && uid != ""
}

// exclusionSet collects EXDATE values (comma lists, TZID-aware) of a component.
func exclusionSet(body string) map[int64]bool {
	set := map[int64]bool{}
	for _, pl := range ownPropLines(body, "EXDATE") {
		for _, v := range strings.Split(pl.value, ",") {
			if t, _, ok := parseICSTimeProp(pl.params, v); ok {
				set[t.Unix()] = true
			}
		}
	}
	return set
}

type occurrence struct{ start, end time.Time }

func intervalIntersects(s, e, ws, we time.Time) bool {
	if e.Equal(s) {
		return !s.Before(ws) && s.Before(we)
	}
	return s.Before(we) && e.After(ws)
}

// eventOccurrences returns the occurrences of the components sharing one UID
// that intersect [ws, we): masters expand RRULE/RDATE minus EXDATE, and a
// RECURRENCE-ID override replaces the master instance it names (F6075).
// busyOnly skips CANCELLED and TRANSPARENT components (free-busy) while still
// letting them suppress the instance they override.
func eventOccurrences(blocks []string, ws, we time.Time, busyOnly bool) []occurrence {
	overridden := overriddenSet(blocks)
	var out []occurrence
	for _, b := range blocks {
		if busyOnly {
			if v, _ := ownPropValue(b, "TRANSP"); strings.EqualFold(v, "TRANSPARENT") {
				continue
			}
			if v, _ := ownPropValue(b, "STATUS"); strings.EqualFold(v, "CANCELLED") {
				continue
			}
		}
		start, end, ok := componentTimeRange(b)
		if !ok {
			continue
		}
		if _, isOverride := ownPropValue(b, "RECURRENCE-ID"); isOverride {
			if intervalIntersects(start, end, ws, we) {
				out = append(out, occurrence{start, end})
			}
			continue
		}
		dur := end.Sub(start)
		for _, s := range recurrenceStarts(b, start, end, overridden, ws, we) {
			out = append(out, occurrence{s, s.Add(dur)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start.Before(out[j].start) })
	return out
}

// overriddenSet collects the RECURRENCE-ID instants of override components.
func overriddenSet(blocks []string) map[int64]bool {
	overridden := map[int64]bool{}
	for _, b := range blocks {
		for _, pl := range ownPropLines(b, "RECURRENCE-ID") {
			if t, _, ok := parseICSTimeProp(pl.params, pl.value); ok {
				overridden[t.Unix()] = true
			}
		}
	}
	return overridden
}

// recurrenceStarts returns the starts of a master component's instances
// intersecting [ws, we): RRULE (or the base) plus RDATE, minus EXDATE and
// overridden instances.
func recurrenceStarts(b string, start, end time.Time, overridden map[int64]bool, ws, we time.Time) []time.Time {
	dur := end.Sub(start)
	ex := exclusionSet(b)
	var starts []time.Time
	if rr := parseRRULEBlock(b); rr != nil {
		starts = rruleInstances(start, end, rr, ws, we)
	} else if intervalIntersects(start, end, ws, we) {
		starts = []time.Time{start}
	}
	for _, pl := range ownPropLines(b, "RDATE") {
		for _, v := range strings.Split(pl.value, ",") {
			if t, _, ok := parseICSTimeProp(pl.params, v); ok && intervalIntersects(t, t.Add(dur), ws, we) {
				starts = append(starts, t)
			}
		}
	}
	out := starts[:0]
	for _, s := range starts {
		if !ex[s.Unix()] && !overridden[s.Unix()] {
			out = append(out, s)
		}
	}
	return out
}

const maxRRULEPeriods = 100000

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

func daysInMonth(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// periodCandidates returns the ascending instance starts of recurrence period p.
func (r *rruleSpec) periodCandidates(base time.Time, p int) []time.Time {
	loc := base.Location()
	h, mi, sec := base.Clock()
	ns := base.Nanosecond()
	step := p * r.interval
	switch r.freq {
	case "DAILY":
		c := base.AddDate(0, 0, step)
		if len(r.byDay) > 0 && !r.hasWeekday(c.Weekday()) {
			return nil
		}
		return []time.Time{c}
	case "WEEKLY":
		anchor := base.AddDate(0, 0, 7*step)
		if len(r.byDay) == 0 {
			return []time.Time{anchor}
		}
		monday := anchor.AddDate(0, 0, -((int(anchor.Weekday()) + 6) % 7))
		var out []time.Time
		for off := 0; off < 7; off++ {
			c := monday.AddDate(0, 0, off)
			if r.hasWeekday(c.Weekday()) && !c.Before(base) {
				out = append(out, c)
			}
		}
		return out
	case "MONTHLY":
		total := int(base.Month()) - 1 + step
		y := base.Year() + total/12
		m := time.Month(total%12 + 1)
		days := r.byMonthDay
		if len(days) == 0 {
			days = []int{base.Day()}
		}
		var out []time.Time
		for _, d := range days {
			dim := daysInMonth(y, m)
			if d < 0 {
				d = dim + d + 1
			}
			if d < 1 || d > dim {
				continue // nonexistent dates are omitted
			}
			out = append(out, time.Date(y, m, d, h, mi, sec, ns, loc))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
		return out
	case "YEARLY":
		y := base.Year() + step
		if base.Month() == time.February && base.Day() == 29 && daysInMonth(y, time.February) < 29 {
			return nil
		}
		return []time.Time{time.Date(y, base.Month(), base.Day(), h, mi, sec, ns, loc)}
	}
	return nil
}

func (r *rruleSpec) hasWeekday(d time.Weekday) bool {
	for _, w := range r.byDay {
		if w == d {
			return true
		}
	}
	return false
}

// firstPeriod returns a safe period index to start from for an unbounded
// (COUNT-less) rule, so events that started long before the window are still
// expanded instead of exhausting the safety limit (F6077).
func (r *rruleSpec) firstPeriod(base, from time.Time) int {
	if r.count > 0 || !from.After(base) {
		return 0
	}
	var n int
	switch r.freq {
	case "DAILY":
		n = int(from.Sub(base).Hours() / 24)
	case "WEEKLY":
		n = int(from.Sub(base).Hours() / (24 * 7))
	case "MONTHLY":
		n = (from.Year()-base.Year())*12 + int(from.Month()) - int(base.Month())
	case "YEARLY":
		n = from.Year() - base.Year()
	}
	p := n/r.interval - 2
	if p < 0 {
		return 0
	}
	return p
}

func parseByDay(v string) ([]time.Weekday, bool) {
	var out []time.Weekday
	for _, d := range strings.Split(v, ",") {
		w, ok := weekdayCodes[strings.ToUpper(strings.TrimSpace(d))]
		if !ok { // ordinals (1MO, -1FR) are not supported
			return nil, false
		}
		out = append(out, w)
	}
	return out, len(out) > 0
}

func parseByMonthDay(v string) ([]int, bool) {
	var out []int
	for _, d := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(d))
		if err != nil || n == 0 || n < -31 || n > 31 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}
