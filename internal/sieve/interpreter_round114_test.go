package sieve

import "testing"

// F5960: fileinto names that are not usable mailbox names fall back to keep.
func TestF5960_FileintoUnsafeFolderFallsBackToKeep(t *testing.T) {
	msg := &MessageContext{Headers: map[string][]string{}}
	for _, folder := range []string{"../../etc/x", "a/../b", "/abs", "a\\..\\b", "bad\x00name", "bad\r\nname"} {
		acts, err := ExecuteScript(`require "fileinto"; fileinto "`+escapeForScript(folder)+`";`, msg)
		if err != nil {
			t.Fatalf("%q: %v", folder, err)
		}
		for _, a := range acts {
			if f, ok := a.(FileintoAction); ok {
				t.Fatalf("FAIL: unsafe folder %q reached FileintoAction %q", folder, f.Folder)
			}
		}
		if len(acts) != 1 {
			t.Fatalf("%q: want single keep, got %#v", folder, acts)
		}
	}
	acts, _ := ExecuteScript(`require "fileinto"; fileinto "Work/Reports.2024";`, msg)
	if len(acts) != 1 || acts[0] != (FileintoAction{Folder: "Work/Reports.2024"}) {
		t.Fatalf("valid folder altered: %#v", acts)
	}
}

func escapeForScript(s string) string {
	out := ""
	for _, r := range s {
		if r == '\\' || r == '"' {
			out += "\\"
		}
		out += string(r)
	}
	return out
}

// F5961: duplicate redirects collapse; display names are stripped.
func TestF5961_RedirectDedupAndBareAddress(t *testing.T) {
	msg := &MessageContext{Headers: map[string][]string{}}
	acts, err := ExecuteScript(`redirect "a@b.c"; redirect "A@B.C"; redirect "Bob <bob@x.org>";`, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 || acts[0] != (RedirectAction{Address: "a@b.c"}) || acts[1] != (RedirectAction{Address: "bob@x.org"}) {
		t.Fatalf("FAIL: got %#v", acts)
	}
}

// F5962: :days 0 must not disable reply suppression.
func TestF5962_VacationDaysFloor(t *testing.T) {
	msg := &MessageContext{Headers: map[string][]string{}}
	acts, err := ExecuteScript(`require ["vacation"]; vacation :days 0 "away";`, msg)
	if err != nil || len(acts) != 1 {
		t.Fatalf("%v %#v", err, acts)
	}
	if d := acts[0].(VacationAction).Days; d != 1 {
		t.Fatalf("FAIL: Days = %d, want 1", d)
	}
	acts, _ = ExecuteScript(`require ["vacation", "vacation-seconds"]; vacation :seconds 0 "away";`, msg)
	if acts[0].(VacationAction).SecondsSet != true {
		t.Fatal("seconds lost")
	}
}
