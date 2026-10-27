package sieve

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// F7410: a non-ASCII literal in a :matches key was split into Latin-1 bytes
// and never matched.
func TestR267MatchesNonASCIIKey(t *testing.T) {
	ctx := &MessageContext{Headers: map[string][]string{"Subject": {"Café menu"}}}
	for _, key := range []string{"café*", "*é menu", "café menu", "caf?*"} {
		a, err := ExecuteScript(fmt.Sprintf(`if header :matches "subject" %q { discard; }`, key), ctx)
		if err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		if _, ok := a[0].(DiscardAction); !ok {
			t.Errorf("key %q did not match: %v", key, a)
		}
	}
}

func TestR267GlobMatchSemantics(t *testing.T) {
	for _, c := range []struct {
		glob, val string
		want      bool
	}{
		{"a*a", "a", false}, {"a*b*c", "aXbYc", true}, {"a*b*c", "acb", false}, {"*", "", true},
		{"", "", true}, {"a\\*b", "a*b", true}, {"a\\*b", "axb", false}, {"caf??", "café", false},
		{"?本語", "日本語", true},
	} {
		got, err := globMatch(c.glob, c.val, 0)
		if err != nil || got != c.want {
			t.Errorf("globMatch(%q,%q)=%v,%v want %v", c.glob, c.val, got, err, c.want)
		}
	}
}

// F7411: a many-"*" :matches key on a ~1 MiB body timed out and left an
// uncancellable RE2 goroutine burning CPU for ~12s.
func TestR267MatchesManyStarsLargeBody(t *testing.T) {
	body := strings.Repeat("abcdefghij klmnop\r\n", 55000)
	before := runtime.NumGoroutine()
	src := fmt.Sprintf(`require "body"; if body :raw :matches %q { discard; }`, strings.Repeat("*a", 500)+"*zzz")
	a, err := ExecuteScript(src, &MessageContext{Headers: map[string][]string{}, Body: []byte(body), Size: int64(len(body))})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := a[0].(KeepAction); !ok {
		t.Fatalf("want implicit keep, got %v", a)
	}
	// Only a rise is a leak: the count is process-wide, so a goroutine of an
	// earlier test finishing meanwhile lowers it (the test failed with -1).
	if extra := runtime.NumGoroutine() - before; extra > 0 {
		t.Fatalf("%d goroutine(s) left running", extra)
	}
}
