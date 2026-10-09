package sieve

// Regression tests for audit finding F5035 (see .temp_files/ledger_internal_sieve.md, Round 22).

import (
	"fmt"
	"testing"
)

func regF5035Run(t *testing.T, script string) []Action {
	t.Helper()
	acts, err := ExecuteScript(script, &MessageContext{Headers: map[string][]string{"Subject": {"hi"}}, Size: 10})
	if err != nil {
		t.Fatalf("INVALID execute error: %v", err)
	}
	return acts
}

func regF5035Folders(acts []Action) []string {
	var out []string
	for _, a := range acts {
		switch v := a.(type) {
		case FileintoAction:
			out = append(out, "fileinto:"+v.Folder)
		case KeepAction:
			out = append(out, "keep")
		case VacationAction:
			out = append(out, "vacation")
		case StopAction:
			out = append(out, "stop")
		case RedirectAction:
			out = append(out, "redirect:"+v.Address)
		case DiscardAction:
			out = append(out, "discard")
		}
	}
	return out
}

func TestF5035_Control(t *testing.T) {
	got := regF5035Folders(regF5035Run(t, `require "fileinto"; fileinto "A";`))
	t.Logf("CONTROL EXPECTED: [fileinto:A] | ACTUAL: %v\n", got)
	if fmt.Sprint(got) != "[fileinto:A]" {
		t.Fatal("INVALID control")
	}
}

func TestF5035_Regression(t *testing.T) {
	got := regF5035Folders(regF5035Run(t, `require ["fileinto","vacation"]; fileinto "A"; fileinto "B"; vacation "away";`))
	t.Logf("EXPECTED: [fileinto:A fileinto:B vacation] | ACTUAL: %v\n", got)
	if fmt.Sprint(got) != "[fileinto:A fileinto:B vacation]" {
		t.Fatal("DEFECT F5035: interpreter stops after the first action (RFC 5228 §2.10)")
	}
}

func TestF5035_Edges(t *testing.T) {
	cases := []struct{ script, want string }{
		// stop ends execution; earlier actions survive, later ones do not.
		{`require "fileinto"; fileinto "A"; stop; fileinto "B";`, "[fileinto:A stop]"},
		// actions inside a taken if block plus actions after it.
		{`require "fileinto"; if true { fileinto "A"; } keep;`, "[fileinto:A keep]"},
		// stop inside a nested block terminates the whole script.
		{`require "fileinto"; if true { if true { stop; } fileinto "X"; } fileinto "Y";`, "[stop]"},
		// no actions → implicit keep.
		{`if false { discard; }`, "[keep]"},
		// redirect then keep both retained.
		{`redirect "a@example.com"; keep;`, "[redirect:a@example.com keep]"},
		// discard only cancels the implicit keep; an explicit keep wins.
		{`keep; discard;`, "[keep]"},
		{`discard;`, "[discard]"},
	}
	for _, c := range cases {
		got := fmt.Sprint(regF5035Folders(regF5035Run(t, c.script)))
		t.Logf("EDGE %q EXPECTED %s | ACTUAL %s\n", c.script, c.want, got)
		if got != c.want {
			t.Fatalf("edge %q", c.script)
		}
	}
}
