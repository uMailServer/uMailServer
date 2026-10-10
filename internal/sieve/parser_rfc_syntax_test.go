package sieve

import "testing"

// TestMissingSemicolonRejected covers F5340: RFC 5228 §8.2 command =
// identifier arguments (";" / block). A missing ';' made the next command an
// argument of the previous one (`fileinto "A" keep;` lost the keep).
func TestMissingSemicolonRejected(t *testing.T) {
	for _, script := range []string{
		`require "fileinto"; fileinto "A" keep;`,
		"keep\ndiscard;",
		`keep`,
		`if false { keep; } else true { discard; }`,
	} {
		if _, err := NewParser(script).Parse(); err == nil {
			t.Errorf("Parse(%q) succeeded, want missing ';' error", script)
		}
	}
	for _, script := range []string{
		``,
		`if true { keep; } discard;`,
		`if false { discard; } elsif not exists "X" { keep; } else { stop; }`,
		"require \"vacation\";\nvacation :days 3 text:\nAway\n.\n;",
	} {
		if _, err := NewParser(script).Parse(); err != nil {
			t.Errorf("Parse(%q): %v", script, err)
		}
	}
}

// TestNumberOverflowRejected covers F5341: an out-of-range number became 0
// (or wrapped after a K/M/G shift), so `size :over <huge>` discarded every
// message instead of failing the script (RFC 5228 §2.4.1, §2.10.6).
func TestNumberOverflowRejected(t *testing.T) {
	for _, num := range []string{"99999999999999999999", "99999999999G", "8589934592G"} {
		acts, err := ExecuteScript(`if size :over `+num+` { discard; }`, &MessageContext{Size: 100})
		if err == nil {
			t.Errorf("size :over %s: no error, actions %v", num, acts)
		}
	}
	acts, err := ExecuteScript(`if size :over 8589934591G { discard; }`, &MessageContext{Size: 100})
	if err != nil || len(acts) != 1 {
		t.Errorf("largest in-range number: actions %v err %v, want implicit keep", acts, err)
	} else if _, ok := acts[0].(KeepAction); !ok {
		t.Errorf("largest in-range number: got %v, want keep", acts)
	}
}

// TestHeaderIdentifierCaseInsensitive covers F5342: RFC 5228 §2.1
// identifiers are case-insensitive, but `HEADER :is ...` tested a header
// named "HEADER".
func TestHeaderIdentifierCaseInsensitive(t *testing.T) {
	acts, err := ExecuteScript(`require "fileinto"; if HEADER :is "subject" "hello" { fileinto "Hit"; }`,
		&MessageContext{Headers: map[string][]string{"Subject": {"hello"}}, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0] != (FileintoAction{Folder: "Hit"}) {
		t.Errorf("got %v, want fileinto Hit", acts)
	}
}
