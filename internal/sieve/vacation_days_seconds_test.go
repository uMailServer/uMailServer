package sieve_test

// Regression tests for the :days/:seconds mutual-exclusivity contract of the
// vacation action (RFC 5230 §4.1 + RFC 6131 §4): a script specifying BOTH
// arguments must produce a Sieve processing error, not a silent preference
// for :seconds.
//
// Scripts using :seconds must require the "vacation-seconds" capability
// (RFC 6131 §3), so the harness below does exactly that.

import (
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

// runVacationScript parses and executes a vacation script, returning the
// interpreter error (if any) and the number of VacationActions produced.
func runVacationScript(t *testing.T, script string) (error, int) {
	t.Helper()
	fullScript := "require [\"vacation\"];\n" + script
	p := sieve.NewParser(fullScript)
	parsed, err := p.Parse()
	if err != nil {
		return err, 0
	}

	interp := sieve.NewInterpreter(parsed)
	actions, err := interp.Execute(&sieve.MessageContext{
		From: "sender@example.com",
		To:   []string{"recipient@example.com"},
	})
	if err != nil {
		return err, 0
	}

	vacations := 0
	for _, a := range actions {
		if _, ok := a.(sieve.VacationAction); ok {
			vacations++
		}
	}
	return nil, vacations
}

// runVacationScriptSeconds is the same harness for scripts that use :seconds
// and therefore require the "vacation-seconds" capability (RFC 6131 §3).
func runVacationScriptSeconds(t *testing.T, script string) (error, int) {
	t.Helper()
	fullScript := "require [\"vacation\", \"vacation-seconds\"];\n" + script
	p := sieve.NewParser(fullScript)
	parsed, err := p.Parse()
	if err != nil {
		return err, 0
	}

	interp := sieve.NewInterpreter(parsed)
	actions, err := interp.Execute(&sieve.MessageContext{
		From: "sender@example.com",
		To:   []string{"recipient@example.com"},
	})
	if err != nil {
		return err, 0
	}

	vacations := 0
	for _, a := range actions {
		if _, ok := a.(sieve.VacationAction); ok {
			vacations++
		}
	}
	return nil, vacations
}

// TestVacationDaysAndSecondsMutuallyExclusive pins RFC 5230 §4.1 / RFC 6131
// §4: ":days" and ":seconds" cannot both be specified; the interpreter must
// produce a Sieve processing error for such a script (with the required
// capabilities present, so the error is about mutual exclusivity and not the
// missing vacation-seconds extension).
func TestVacationDaysAndSecondsMutuallyExclusive(t *testing.T) {
	err, vacations := runVacationScriptSeconds(t, `vacation :days 2 :seconds 60 "Body";`)
	if err == nil {
		t.Fatalf("FAIL: :days and :seconds specified together must be a Sieve processing error (RFC 5230 §4.1 / RFC 6131 §4); got nil error and %d vacation action(s)", vacations)
	}
}

// TestVacationDaysOnlyAccepted is the unaffected-path control: a script with
// :days alone must keep parsing and executing normally.
func TestVacationDaysOnlyAccepted(t *testing.T) {
	err, vacations := runVacationScript(t, `vacation :days 2 "Body";`)
	if err != nil {
		t.Fatalf("CONTROL FAILED (harness): :days-only script must be accepted, got error: %v", err)
	}
	if vacations != 1 {
		t.Fatalf("CONTROL FAILED (harness): :days-only script must produce one vacation action, got %d", vacations)
	}
}

// TestVacationSecondsOnlyAccepted is the second unaffected-path control: a
// script with :seconds alone (RFC 6131, properly requiring the capability)
// must keep parsing and executing normally.
func TestVacationSecondsOnlyAccepted(t *testing.T) {
	err, vacations := runVacationScriptSeconds(t, `vacation :seconds 60 "Body";`)
	if err != nil {
		t.Fatalf("CONTROL FAILED (harness): :seconds-only script must be accepted, got error: %v", err)
	}
	if vacations != 1 {
		t.Fatalf("CONTROL FAILED (harness): :seconds-only script must produce one vacation action, got %d", vacations)
	}
}
