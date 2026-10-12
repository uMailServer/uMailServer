package sieve

import "testing"

func r240Run(t *testing.T, script string) []Action {
	t.Helper()
	acts, err := ExecuteScript(script, &MessageContext{From: "a@b.com", To: []string{"u@x.com"},
		Headers: map[string][]string{"Subject": {"hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	return acts
}

func r240Vacations(acts []Action) (v []VacationAction) {
	for _, a := range acts {
		if va, ok := a.(VacationAction); ok {
			v = append(v, va)
		}
	}
	return
}

// F7171: a huge :days / :seconds must be bounded: time.Duration(days)*24h
// overflows and a negative window turned off reply suppression entirely.
func TestR240VacationIntervalBounded(t *testing.T) {
	acts := r240Run(t, `require "vacation"; vacation :days 9223372036854775807 "away";`)
	va := r240Vacations(acts)
	if len(va) != 1 || va[0].Days > MaxVacationDays || va[0].Days < 1 {
		t.Fatalf("days not bounded: %+v", va)
	}
	acts = r240Run(t, `require ["vacation","vacation-seconds"]; vacation :seconds 9223372036854775807 "away";`)
	va = r240Vacations(acts)
	if len(va) != 1 || va[0].Seconds > MaxVacationDays*86400 || va[0].Seconds < 0 {
		t.Fatalf("seconds not bounded: %+v", va)
	}
}

// F7172: two vacation actions sharing a handle (explicit, or implied by
// identical subject/reason) are one reply (RFC 5230 §4.6).
func TestR240VacationDuplicateHandleOnce(t *testing.T) {
	acts := r240Run(t, `require "vacation";
vacation :handle "h" "one";
vacation :handle "h" "two";
vacation :subject "s" "same";
vacation :subject "s" "same";
vacation :handle "other" "three";`)
	va := r240Vacations(acts)
	if len(va) != 3 {
		t.Fatalf("want 3 distinct vacation actions, got %d: %+v", len(va), va)
	}
	if va[0].Body != "one" {
		t.Fatalf("first occurrence must win: %+v", va[0])
	}
}

func TestR240ManagerVacationIntervalNoOverflow(t *testing.T) {
	m := NewManager()
	if d := m.GetVacationInterval(1 << 62); d <= 0 {
		t.Fatalf("interval overflowed: %v", d)
	}
	if !m.CheckAndRecordVacation("s", 1<<62) {
		t.Fatal("first reply must pass")
	}
	if m.CheckAndRecordVacation("s", 1<<62) {
		t.Fatal("second reply within the window must be suppressed")
	}
}
