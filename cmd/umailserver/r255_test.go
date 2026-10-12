package main

import (
	"bytes"
	"strings"
	"testing"
)

// F7323: destructive commands accept --yes and fail closed on EOF/other input.
func TestR255ConfirmDestructive(t *testing.T) {
	var out bytes.Buffer
	if !confirmDestructive("delete?", true, strings.NewReader(""), &out) {
		t.Error("--yes must skip the prompt")
	}
	for in, want := range map[string]bool{"y\n": true, "YES\n": true, "yes\n": true, "n\n": false, "\n": false, "": false, "maybe\n": false} {
		var o bytes.Buffer
		if got := confirmDestructive("delete?", false, strings.NewReader(in), &o); got != want {
			t.Errorf("input %q: got %v want %v", in, got, want)
		}
	}
}

// F7324: a --password on argv warns; env fallbacks work without a warning.
func TestR255ResolveSecret(t *testing.T) {
	t.Setenv("UMAILSERVER_PASSWORD", "")
	t.Setenv("UMAILSERVER_X", "fromenv")
	var errb bytes.Buffer
	if got := resolveSecret("argv", "--password", &errb, "UMAILSERVER_X"); got != "argv" || !strings.Contains(errb.String(), "process list") {
		t.Errorf("argv: got %q warn=%q", got, errb.String())
	}
	errb.Reset()
	if got := resolveSecret("", "--password", &errb, "UMAILSERVER_X"); got != "fromenv" || errb.Len() != 0 {
		t.Errorf("env: got %q warn=%q", got, errb.String())
	}
	t.Setenv("UMAILSERVER_X", "")
	t.Setenv("UMAILSERVER_PASSWORD", "generic")
	if got := resolveSecret("", "--password", &errb, "UMAILSERVER_X"); got != "generic" {
		t.Errorf("generic env: got %q", got)
	}
}
