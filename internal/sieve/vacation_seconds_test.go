package sieve

import (
	"testing"
)

// TestVacation_SecondsTagSetsInterval verifies that RFC 5230 §4.1's :seconds tag
// is parsed and its value stored in VacationAction.Seconds.
//
// Expected:  vacation :seconds 3600 "away"
//           VacationAction.Seconds = 3600
//
// Bug: VacationAction had no Seconds field; the :seconds TagValue fell through
// the switch in executeVacation with no handler, silently discarding the value.
// handleSieveVacation hardcoded sendInterval = 24h, so :seconds 3600 was
// silently treated as 24 hours.
//
// Fix: added Seconds int to VacationAction; added :seconds case in
// executeVacation; plumbed Seconds into handleSieveVacation with 24h floor.
func TestVacation_SecondsTagSetsInterval(t *testing.T) {
	script := `require "vacation"; vacation :seconds 3600 "I am away";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	interp := NewInterpreter(s)
	actions, err := interp.Execute(&MessageContext{
		From: "sender@example.com",
		To:   []string{"me@example.com"},
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var vac VacationAction
	for _, a := range actions {
		if va, ok := a.(VacationAction); ok {
			vac = va
			break
		}
	}

	// Verify Seconds field was populated from the :seconds tag value.
	if vac.Seconds != 3600 {
		t.Errorf("VacationAction.Seconds = %d; want 3600", vac.Seconds)
	}
}

// TestVacation_SecondsTagDefaultZero verifies that when :seconds is absent,
// Seconds defaults to 0 and the 24h floor in handleSieveVacation applies.
func TestVacation_SecondsTagDefaultZero(t *testing.T) {
	script := `require "vacation"; vacation "I am away";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	interp := NewInterpreter(s)
	actions, err := interp.Execute(&MessageContext{
		From: "sender@example.com",
		To:   []string{"me@example.com"},
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var vac VacationAction
	for _, a := range actions {
		if va, ok := a.(VacationAction); ok {
			vac = va
			break
		}
	}

	if vac.Seconds != 0 {
		t.Errorf("VacationAction.Seconds = %d; want 0 when :seconds is absent", vac.Seconds)
	}
}

// TestVacation_SecondsTagWithDays verifies that :days and :seconds can coexist
// without the :seconds value corrupting Days.
func TestVacation_SecondsTagWithDays(t *testing.T) {
	script := `require "vacation"; vacation :days 7 :seconds 60 "away";`

	p := NewParser(script)
	s, err := p.Parse()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	interp := NewInterpreter(s)
	actions, err := interp.Execute(&MessageContext{
		From: "sender@example.com",
		To:   []string{"me@example.com"},
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var vac VacationAction
	for _, a := range actions {
		if va, ok := a.(VacationAction); ok {
			vac = va
			break
		}
	}

	if vac.Days != 7 {
		t.Errorf("VacationAction.Days = %d; want 7", vac.Days)
	}
	if vac.Seconds != 60 {
		t.Errorf("VacationAction.Seconds = %d; want 60", vac.Seconds)
	}
}
