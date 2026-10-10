package sieve

import (
	"strings"
	"testing"
)

func FuzzManageSieveLine(f *testing.F) {
	for _, s := range []string{
		``, `LISTSCRIPTS`, `PUTSCRIPT "a" {10+}`, `PUTSCRIPT "a b" "x\"y"`,
		`AUTHENTICATE "PLAIN" "AGEAYg=="`, `"\`, `"unterminated`, "\t \x00 \xff",
		`SETACTIVE "\\"`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, line string) {
		fuzzTimed(t, "line", func() {
			parts := parseManageSieveLine(line)
			for _, p := range parts {
				if p == "" {
					t.Fatalf("empty token from %q", line)
				}
				if strings.HasPrefix(p, `"`) && quotedStringComplete(p) {
					v := unquoteManageSieveArg(p)
					// round trip
					if got := unquoteManageSieveArg(quoteManageSieveString(v)); got != v {
						t.Fatalf("quote round trip %q != %q", got, v)
					}
				}
			}
			_ = validScriptName(line)
		})
	})
}
