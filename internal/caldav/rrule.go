package caldav

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxRRULEPeriods bounds the number of recurrence periods (days, weeks,
// months or years) examined per expansion, in addition to maxRRULEInstances.
const maxRRULEPeriods = 100000

// byDayRule is one BYDAY entry: ord 0 means every such weekday; otherwise the
// ord-th (negative: from the end) weekday of the month or year.
type byDayRule struct {
	ord int
	wd  time.Weekday
}

// rruleSpec is the supported RRULE subset (RFC 5545 §3.3.10): FREQ, INTERVAL,
// COUNT, UNTIL, WKST, BYMONTH, BYWEEKNO, BYYEARDAY, BYMONTHDAY, BYDAY
// (including ordinals) and BYSETPOS. Time-of-day parts (BYHOUR, BYMINUTE,
// BYSECOND) and unknown parts are unsupported: parseRRULEValue returns nil and
// the event degrades to base-only matching.
type rruleSpec struct {
	freq     string
	interval int
	count    int
	until    time.Time
	wkst     time.Weekday

	byDay      []byDayRule
	byMonthDay []int
	byMonth    []int
	byYearDay  []int
	byWeekNo   []int
	bySetPos   []int
}

func parseIntList(v string, max int) ([]int, bool) {
	var out []int
	for _, d := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(d))
		if err != nil || n == 0 || n < -max || n > max {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) > 0
}

func parseByDay(v string) ([]byDayRule, bool) {
	var out []byDayRule
	for _, d := range strings.Split(v, ",") {
		d = strings.ToUpper(strings.TrimSpace(d))
		if len(d) < 2 {
			return nil, false
		}
		wd, ok := weekdayCodes[d[len(d)-2:]]
		if !ok {
			return nil, false
		}
		ord := 0
		if p := d[:len(d)-2]; p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n == 0 || n < -53 || n > 53 {
				return nil, false
			}
			ord = n
		}
		out = append(out, byDayRule{ord, wd})
	}
	return out, len(out) > 0
}

// parseRRULEValue parses an RRULE value ("FREQ=DAILY;COUNT=10"). It returns
// nil when the rule is absent, invalid or uses unsupported parts.
func parseRRULEValue(value string) *rruleSpec {
	var r rruleSpec
	r.wkst = time.Monday
	seenFreq := false
	for _, part := range strings.Split(strings.TrimSpace(value), ";") {
		k, v, found := strings.Cut(part, "=")
		if !found {
			return nil
		}
		var ok bool
		switch strings.ToUpper(strings.TrimSpace(k)) {
		case "FREQ":
			freq := strings.ToUpper(strings.TrimSpace(v))
			if freq != "DAILY" && freq != "WEEKLY" && freq != "MONTHLY" && freq != "YEARLY" {
				return nil
			}
			r.freq = freq
			seenFreq = true
		case "INTERVAL":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 {
				return nil
			}
			r.interval = n
		case "COUNT":
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 1 {
				return nil
			}
			r.count = n
		case "UNTIL":
			t, tok := parseICSTime(strings.TrimSpace(v))
			if !tok {
				return nil
			}
			if len(strings.TrimSpace(v)) == 8 {
				// A DATE UNTIL is inclusive of the whole day (F6078).
				t = t.AddDate(0, 0, 1).Add(-time.Nanosecond)
			}
			r.until = t
		case "BYDAY":
			if r.byDay, ok = parseByDay(v); !ok {
				return nil
			}
		case "BYMONTHDAY":
			if r.byMonthDay, ok = parseIntList(v, 31); !ok {
				return nil
			}
		case "BYMONTH":
			if r.byMonth, ok = parseIntList(v, 12); !ok {
				return nil
			}
			for _, m := range r.byMonth {
				if m < 1 {
					return nil
				}
			}
		case "BYYEARDAY":
			if r.byYearDay, ok = parseIntList(v, 366); !ok {
				return nil
			}
		case "BYWEEKNO":
			if r.byWeekNo, ok = parseIntList(v, 53); !ok {
				return nil
			}
		case "BYSETPOS":
			if r.bySetPos, ok = parseIntList(v, 366); !ok {
				return nil
			}
		case "WKST":
			wd, wok := weekdayCodes[strings.ToUpper(strings.TrimSpace(v))]
			if !wok {
				return nil
			}
			r.wkst = wd
		default:
			return nil
		}
	}
	if !seenFreq {
		return nil
	}
	if r.interval == 0 {
		r.interval = 1
	}
	// RFC 5545 §3.3.10 combination table.
	if len(r.byWeekNo) > 0 && r.freq != "YEARLY" {
		return nil
	}
	if len(r.byYearDay) > 0 && (r.freq == "DAILY" || r.freq == "WEEKLY" || r.freq == "MONTHLY") {
		return nil
	}
	if len(r.byMonthDay) > 0 && r.freq == "WEEKLY" {
		return nil
	}
	for _, d := range r.byDay {
		if d.ord != 0 && (r.freq == "DAILY" || r.freq == "WEEKLY" || len(r.byWeekNo) > 0) {
			return nil
		}
	}
	return &r
}

// parseRRULEBlock extracts the component's own RRULE property and parses it;
// nil when the block has no supported RRULE.
func parseRRULEBlock(body string) *rruleSpec {
	if l := ownPropLines(body, "RRULE"); len(l) > 0 {
		return parseRRULEValue(l[0].value)
	}
	return nil
}

func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func weekStart(d time.Time, wkst time.Weekday) time.Time {
	return d.AddDate(0, 0, -((int(d.Weekday()) - int(wkst) + 7) % 7))
}

// weekYearStart returns the first day of week 1 of year y: the week (starting
// at wkst) that contains at least four days of y, i.e. contains January 4.
func weekYearStart(y int, wkst time.Weekday) time.Time {
	return weekStart(time.Date(y, time.January, 4, 0, 0, 0, 0, time.UTC), wkst)
}

// effective returns a copy of r with the RFC 5545 implicit BY-rules derived
// from the DTSTART base filled in.
func (r *rruleSpec) effective(base time.Time) rruleSpec {
	e := *r
	switch r.freq {
	case "YEARLY":
		switch {
		case len(r.byWeekNo) > 0 && len(r.byYearDay) == 0 && len(r.byMonthDay) == 0 && len(r.byDay) == 0:
			e.byDay = []byDayRule{{0, base.Weekday()}}
		case len(r.byWeekNo) == 0 && len(r.byYearDay) == 0 && len(r.byMonthDay) == 0 && len(r.byDay) == 0:
			e.byMonthDay = []int{base.Day()}
			if len(r.byMonth) == 0 {
				e.byMonth = []int{int(base.Month())}
			}
		}
	case "MONTHLY":
		if len(r.byMonthDay) == 0 && len(r.byDay) == 0 {
			e.byMonthDay = []int{base.Day()}
		}
	case "WEEKLY":
		if len(r.byDay) == 0 {
			e.byDay = []byDayRule{{0, base.Weekday()}}
		}
	}
	return e
}

// periodDays returns the first day of recurrence period p and every calendar
// day (UTC midnight) the BY-rules are evaluated over.
func (r *rruleSpec) periodDays(base time.Time, p int) (time.Time, []time.Time) {
	step := p * r.interval
	b := civilDate(base)
	switch r.freq {
	case "DAILY":
		d := b.AddDate(0, 0, step)
		return d, []time.Time{d}
	case "WEEKLY":
		ws := weekStart(b, r.wkst).AddDate(0, 0, 7*step)
		days := make([]time.Time, 7)
		for i := range days {
			days[i] = ws.AddDate(0, 0, i)
		}
		return ws, days
	case "MONTHLY":
		first := time.Date(b.Year(), b.Month()+time.Month(step), 1, 0, 0, 0, 0, time.UTC)
		n := daysInMonth(first.Year(), first.Month())
		days := make([]time.Time, n)
		for i := range days {
			days[i] = first.AddDate(0, 0, i)
		}
		return first, days
	default: // YEARLY
		y := b.Year() + step
		first := time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
		if len(r.byWeekNo) > 0 {
			start := weekYearStart(y, r.wkst)
			total := int(weekYearStart(y+1, r.wkst).Sub(start).Hours()/24) / 7
			var days []time.Time
			for _, n := range r.byWeekNo {
				if n < 0 {
					n = total + n + 1
				}
				if n < 1 || n > total {
					continue
				}
				ws := start.AddDate(0, 0, 7*(n-1))
				for i := 0; i < 7; i++ {
					days = append(days, ws.AddDate(0, 0, i))
				}
			}
			return first, days
		}
		n := 365
		if daysInMonth(y, time.February) == 29 {
			n = 366
		}
		days := make([]time.Time, n)
		for i := range days {
			days[i] = first.AddDate(0, 0, i)
		}
		return first, days
	}
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// dayMatches applies the (effective) BY limits to one calendar day.
func (r *rruleSpec) dayMatches(d time.Time) bool {
	if len(r.byMonth) > 0 && !containsInt(r.byMonth, int(d.Month())) {
		return false
	}
	dim := daysInMonth(d.Year(), d.Month())
	if len(r.byMonthDay) > 0 && !containsInt(r.byMonthDay, d.Day()) && !containsInt(r.byMonthDay, d.Day()-dim-1) {
		return false
	}
	if len(r.byYearDay) > 0 {
		diy := 365
		if dim := daysInMonth(d.Year(), time.February); dim == 29 {
			diy = 366
		}
		doy := d.YearDay()
		if !containsInt(r.byYearDay, doy) && !containsInt(r.byYearDay, doy-diy-1) {
			return false
		}
	}
	if len(r.byDay) > 0 {
		monthScope := r.freq == "MONTHLY" || (r.freq == "YEARLY" && len(r.byMonth) > 0)
		ok := false
		for _, rule := range r.byDay {
			if rule.wd != d.Weekday() {
				continue
			}
			if rule.ord == 0 {
				ok = true
				break
			}
			var pos, fromEnd int
			if monthScope {
				pos = (d.Day()-1)/7 + 1
				fromEnd = -((dim-d.Day())/7 + 1)
			} else {
				diy := 365
				if daysInMonth(d.Year(), time.February) == 29 {
					diy = 366
				}
				pos = (d.YearDay()-1)/7 + 1
				fromEnd = -((diy-d.YearDay())/7 + 1)
			}
			if rule.ord == pos || rule.ord == fromEnd {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// periodCandidates returns the ascending instance starts of recurrence period
// p, honoring BYSETPOS and dropping anything before the base DTSTART.
func (r *rruleSpec) periodCandidates(eff *rruleSpec, base time.Time, p int) (time.Time, []time.Time) {
	loc := base.Location()
	h, mi, sec := base.Clock()
	ns := base.Nanosecond()
	start, days := eff.periodDays(base, p)
	var sel []time.Time
	for _, d := range days {
		if eff.dayMatches(d) {
			sel = append(sel, d)
		}
	}
	sort.Slice(sel, func(i, j int) bool { return sel[i].Before(sel[j]) })
	uniq := sel[:0]
	for i, d := range sel {
		if i == 0 || !d.Equal(sel[i-1]) {
			uniq = append(uniq, d)
		}
	}
	sel = uniq
	if len(r.bySetPos) > 0 {
		var picked []time.Time
		used := map[int]bool{}
		for _, sp := range r.bySetPos {
			i := sp - 1
			if sp < 0 {
				i = len(sel) + sp
			}
			if i >= 0 && i < len(sel) && !used[i] {
				used[i] = true
				picked = append(picked, sel[i])
			}
		}
		sort.Slice(picked, func(i, j int) bool { return picked[i].Before(picked[j]) })
		sel = picked
	}
	out := make([]time.Time, 0, len(sel))
	for _, d := range sel {
		t := time.Date(d.Year(), d.Month(), d.Day(), h, mi, sec, ns, loc)
		if !t.Before(base) {
			out = append(out, t)
		}
	}
	return start, out
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

// rruleInstances generates the recurrence instances of a component whose
// base interval is [baseStart, baseEnd) and returns those intersecting
// [windowStart, windowEnd). Zero-length instances match points inside the
// window (RFC 4791 §9.9.1). Expansion is bounded by maxRRULEPeriods periods,
// maxRRULEInstances results and the window end.
func rruleInstances(baseStart, baseEnd time.Time, r *rruleSpec, windowStart, windowEnd time.Time) []time.Time {
	dur := baseEnd.Sub(baseStart)
	eff := r.effective(baseStart)
	var out []time.Time
	count := 0
	first := r.firstPeriod(baseStart, windowStart.Add(-dur))
	for p := first; p < first+maxRRULEPeriods && len(out) < maxRRULEInstances; p++ {
		pstart, cands := r.periodCandidates(&eff, baseStart, p)
		if pstart.Year() > 9999 || pstart.After(windowEnd.AddDate(0, 0, 8)) {
			return out
		}
		for _, inst := range cands {
			if !r.until.IsZero() && inst.After(r.until) {
				return out
			}
			if r.count > 0 && count >= r.count {
				return out
			}
			count++
			if intervalIntersects(inst, inst.Add(dur), windowStart, windowEnd) {
				out = append(out, inst)
			}
		}
	}
	return out
}
