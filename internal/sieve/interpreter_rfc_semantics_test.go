package sieve

import (
	"strings"
	"testing"
)

func regF5240Folder(t *testing.T, script string) string {
	t.Helper()
	acts, err := ExecuteScript(`require ["fileinto", "vacation"]; `+script, &MessageContext{
		Headers: map[string][]string{"Subject": {"a1 b2 c3 d4 e5"}},
		Size:    10,
	})
	if err != nil {
		t.Fatalf("execute %q: %v", script, err)
	}
	for _, a := range acts {
		if f, ok := a.(FileintoAction); ok {
			return f.Folder
		}
	}
	return "INBOX"
}

// TestNotAndExistsTests covers F5240: "not" and "exists" (RFC 5228 §5.5,
// §5.8) were parsed as header tests on headers named "not"/"exists", and
// unimplemented tests were evaluated the same way.
func TestNotAndExistsTests(t *testing.T) {
	if got := regF5240Folder(t, `if not header :contains "subject" "zz" { fileinto "N"; }`); got != "N" {
		t.Errorf("not: got %s, want N", got)
	}
	if got := regF5240Folder(t, `if exists "subject" { fileinto "E"; }`); got != "E" {
		t.Errorf("exists: got %s, want E", got)
	}
	_, err := ExecuteScript(`if address :is "from" "a@x" { keep; }`, &MessageContext{})
	if err == nil || !strings.Contains(err.Error(), "unsupported test") {
		t.Errorf("address: want unsupported test error, got %v", err)
	}
}

// TestMatchesManyWildcards covers F5241: keys with four or more "*" were
// rejected as ReDoS candidates and silently evaluated false.
func TestMatchesManyWildcards(t *testing.T) {
	if got := regF5240Folder(t, `if header :matches "subject" "*a*b*c*d*" { fileinto "M"; }`); got != "M" {
		t.Errorf("got %s, want M", got)
	}
}

// TestHeaderDefaultMatchTypeIs covers F5243: without a match-type tag the
// test must use :is (RFC 5228 §2.7.1), not never match.
func TestHeaderDefaultMatchTypeIs(t *testing.T) {
	if got := regF5240Folder(t, `if header "subject" "A1 B2 C3 D4 E5" { fileinto "D"; }`); got != "D" {
		t.Errorf("got %s, want D", got)
	}
}

// TestVacationReasonIsBody covers F5242: RFC 5230 §4 the positional reason is
// the reply body, and :from/:handle/:addresses take their own values in any
// position.
func TestVacationReasonIsBody(t *testing.T) {
	acts, err := ExecuteScript(`require "vacation"; vacation :handle "h" :from "me@x" :addresses ["alt@x"] "I am away";`, &MessageContext{})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := acts[0].(VacationAction)
	if !ok || v.Subject != "" || v.Body != "I am away" || v.Handle != "h" || v.From != "me@x" || len(v.Addresses) != 1 {
		t.Fatalf("got %+v", acts[0])
	}
}
