package sieve

import (
	"testing"
	"time"
)

// TestParser_UnexpectedCharacterTerminates guards F5005: a character that
// cannot start an argument used to leave the parser position unchanged, so
// Parse spun forever (reachable pre-auth through ManageSieve CHECKSCRIPT).
func TestParser_UnexpectedCharacterTerminates(t *testing.T) {
	for _, src := range []string{
		`if anyof (header :is "subject" "a", true) { discard; }`,
		`keep, ;`,
		`keep )`,
		`fileinto "A" }`,
		`redirect !`,
		"keep \x00;",
	} {
		done := make(chan error, 1)
		go func() {
			_, err := NewParser(src).Parse()
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("Parse(%q): expected error for unexpected character", src)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Parse(%q) did not return", src)
		}
	}
}
