package sieve_test

import (
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/sieve"
)

// TestExecuteVacation_TagParsing verifies that vacation command tags (:subject, :days)
// are properly parsed and advance the argument index.
//
// RFC 5228 §2.10: tagged arguments like ":subject" are followed by a string value.
// The interpreter must skip the tagged value after seeing a tag — otherwise the tag
// itself is re-consumed as a string on the next iteration.
func TestExecuteVacation_TagParsing(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		wantDays    int
		wantSubject string
		wantBody    string
		wantFail    bool
	}{
		{
			// This is the canonical failing case: :days 5 is a tag+value pair.
			// executeVacation must advance past the "5" after seeing ":days",
			// but the current loop does NOT advance for tag arguments, so "5"
			// is consumed again as a NumberValue and Days is NOT set.
			name:        "days tag sets Days field",
			script:      `vacation :days 5 "Body only";`,
			wantDays:    5,
			wantSubject: "",
			wantBody:    "Body only",
		},
		{
			name:        "subject tag sets Subject field",
			script:      `vacation :subject "My Subject" "Body text";`,
			wantDays:    7, // default
			wantSubject: "My Subject",
			wantBody:    "Body text",
		},
		{
			name:        "both subject and days tags",
			script:      `vacation :subject "OOO" :days 3 "Back soon";`,
			wantDays:    3,
			wantSubject: "OOO",
			wantBody:    "Back soon",
		},
		{
			name:     "days tag with mime flag",
			script:   `vacation :mime :days 1 "On vacation";`,
			wantDays: 1,
			wantBody: "On vacation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fullScript := "require [\"vacation\"];\n" + tc.script
			p := sieve.NewParser(fullScript)
			script, err := p.Parse()
			if err != nil {
				if tc.wantFail {
					t.Logf("parse error (expected): %v", err)
					return
				}
				t.Fatalf("parse error: %v", err)
			}

			interp := sieve.NewInterpreter(script)
			actions, err := interp.Execute(&sieve.MessageContext{
				From: "sender@example.com",
				To:   []string{"recipient@example.com"},
			})
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}

			// Find the vacation action
			var found bool
			for _, a := range actions {
				va, ok := a.(sieve.VacationAction)
				if !ok {
					continue
				}
				found = true
				// Debug: log all fields so we can see what's actually returned
				t.Logf("  got: Days=%d Subject=%q Body=%q MIME=%v", va.Days, va.Subject, va.Body, va.Mime)

				if va.Days != tc.wantDays {
					t.Errorf("Days = %d, want %d", va.Days, tc.wantDays)
				}
				if !strings.EqualFold(va.Subject, tc.wantSubject) {
					t.Errorf("Subject = %q, want %q", va.Subject, tc.wantSubject)
				}
				if !strings.EqualFold(va.Body, tc.wantBody) {
					t.Errorf("Body = %q, want %q", va.Body, tc.wantBody)
				}
			}

			if !found && !tc.wantFail {
				t.Errorf("no VacationAction found in actions")
			}
		})
	}
}
