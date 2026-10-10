package sieve

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Round 142 regression tests (F6240-F6249): RFC 5228 compile-time strictness,
// the missing standard tests/extensions and the run-time safety limits. They
// only use API that exists before the round (ValidateScript, ExecuteScript,
// ProcessMessage) so they can be run against the old tree.

func r142Msg() *MessageContext {
	return &MessageContext{
		From: "<alice@example.com>",
		To:   []string{"bob@example.org"},
		Headers: map[string][]string{
			"From":    {`"Alice A" <alice@example.com>`},
			"To":      {"bob@example.org, Carol <carol@Example.NET>"},
			"Subject": {"Hello World"},
			"X-Num":   {"42"},
		},
		Body: []byte("Hi there\r\nsecret word\r\n"),
		Size: 1000,
	}
}

func r142Folder(t *testing.T, script string) string {
	t.Helper()
	acts, err := ExecuteScript(script, r142Msg())
	if err != nil {
		t.Fatalf("ExecuteScript(%q): %v", script, err)
	}
	for _, a := range acts {
		if f, ok := a.(FileintoAction); ok {
			return f.Folder
		}
	}
	return ""
}

// F6240: any unknown command/test/tag, wrong argument shape or missing
// require is a compile error (RFC 5228 §2.10.1), never silently ignored.
func TestF6240_CompileRejectsInvalidScripts(t *testing.T) {
	m := NewManager()
	bad := []string{
		`frobnicate;`,
		`set "a" "b";`,
		`addheader "X" "y";`,
		`keep :bogus;`,
		`require "fileinto"; fileinto :bogus "A";`,
		`require "fileinto"; fileinto;`,
		`require "fileinto"; fileinto "A" "B";`,
		`fileinto "A";`,                                // no require
		`reject "no";`,                                 // no require
		`vacation "x";`,                                // no require
		`require "vacation"; vacation :seconds 5 "x";`, // needs vacation-seconds
		`redirect;`,
		`redirect :copy "a@b.c";`, // needs copy
		`if frob "a" { keep; }`,
		`if header :contains "s" { keep; }`, // missing key list
		`if header :bogus "s" "k" { keep; }`,
		`if header :is :contains "s" "k" { keep; }`,
		`if size :over { keep; }`,
		`if size :over 1 :under 2 { keep; }`,
		`if exists { keep; }`,
		`if true false { keep; }`,
		`if true;`,
		`else { keep; }`,
		`if true { keep; } elsif { keep; }`,
		`keep; require "fileinto";`,
		`require "variables";`,
		`require "nonexistent";`,
		`if header :count "ge" "to" "1" { keep; }`, // needs relational
		`if header :regex "s" "x" { keep; }`,       // needs regex
		`if body :contains "x" { keep; }`,          // needs body
		`if envelope :is "from" "x" { keep; }`,     // needs envelope
		`if header :comparator "i;bogus" :is "s" "k" { keep; }`,
		`if header :comparator "i;ascii-numeric" :is "s" "1" { keep; }`, // needs comparator ext
		`require "regex"; if header :regex "s" "(" { keep; }`,
		`require "relational"; if header :count "zz" "to" "1" { keep; }`,
		`require "envelope"; if envelope :is "bogus" "x" { keep; }`,
		`stop 1;`,
		`keep { keep; }`,
	}
	for _, src := range bad {
		if err := m.ValidateScript(src); err == nil {
			t.Errorf("ValidateScript(%q) accepted an invalid script", src)
		}
	}
	good := []string{
		`keep;`,
		`require ["fileinto", "reject", "vacation", "vacation-seconds", "envelope", "body", "relational", "regex", "imap4flags", "copy", "comparator-i;ascii-numeric"]; keep;`,
		`require "fileinto"; if header :contains "subject" "x" { fileinto "A"; } elsif true { stop; } else { discard; }`,
		`if allof (true, anyof (false, not true), size :over 5K) { keep; }`,
	}
	for _, src := range good {
		if err := m.ValidateScript(src); err != nil {
			t.Errorf("ValidateScript(%q): %v", src, err)
		}
	}
}

// F6241: allof/anyof with a test list must parse and evaluate.
func TestF6241_AllofAnyof(t *testing.T) {
	cases := map[string]string{
		`require "fileinto"; if allof (header :contains "subject" "hello", size :under 5000) { fileinto "ALL"; }`:                           "ALL",
		`require "fileinto"; if allof (header :contains "subject" "hello", size :over 5000) { fileinto "ALL"; }`:                            "",
		`require "fileinto"; if anyof (false, header :is "subject" "hello world") { fileinto "ANY"; }`:                                      "ANY",
		`require "fileinto"; if not anyof (false, false) { fileinto "NOT"; }`:                                                               "NOT",
		`require "fileinto"; if allof (anyof (false, true), not allof (true, false)) { fileinto "NEST"; }`:                                  "NEST",
		"require \"fileinto\"; if anyof(header :is \"subject\" \"x\",\n  # c\n header :is \"subject\" \"Hello World\") { fileinto \"C\"; }": "C",
	}
	for src, want := range cases {
		if got := r142Folder(t, src); got != want {
			t.Errorf("%q: folder %q, want %q", src, got, want)
		}
	}
	for _, src := range []string{`if allof () { keep; }`, `if anyof (true,) { keep; }`, `if allof (true { keep; }`, `if allof true { keep; }`} {
		if err := NewManager().ValidateScript(src); err == nil {
			t.Errorf("%q should not compile", src)
		}
	}
}

// F6242: address test (RFC 5228 §5.1).
func TestF6242_AddressTest(t *testing.T) {
	cases := map[string]string{
		`if address :is "from" "alice@example.com"`:                       "Y",
		`if address :localpart :is "from" "ALICE"`:                        "Y",
		`if address :domain :is "from" "example.com"`:                     "Y",
		`if address :domain :contains ["to", "cc"] "example.net"`:         "Y", // second address of To, case-folded
		`if address :all :matches "to" "*@example.org"`:                   "Y",
		`if address :is "from" ["x@y", "alice@example.com"]`:              "Y",
		`if address :localpart :is "from" "alice@example.com"`:            "",
		`if address :comparator "i;octet" :domain :is "to" "example.net"`: "", // octet: Example.NET != example.net
		`if address :comparator "i;octet" :domain :is "to" "Example.NET"`: "Y",
		`if not address :is "from" "nobody@example.com"`:                  "Y",
		`if address :is "x-missing" "a@b"`:                                "",
	}
	for cond, want := range cases {
		got := r142Folder(t, `require "fileinto"; `+cond+` { fileinto "Y"; }`)
		if got != want {
			t.Errorf("%s: got %q want %q", cond, got, want)
		}
	}
}

// F6243: envelope test (RFC 5228 §5.4), needs require "envelope".
func TestF6243_EnvelopeTest(t *testing.T) {
	cases := map[string]string{
		`if envelope :is "from" "alice@example.com"`:            "Y",
		`if envelope :domain :is "to" "example.org"`:            "Y",
		`if envelope :localpart :matches "to" "b*"`:             "Y",
		`if envelope :all :is ["from", "to"] "bob@example.org"`: "Y",
		`if envelope :is "from" "<>"`:                           "",
		`if envelope :is "from" "mallory@example.com"`:          "",
	}
	for cond, want := range cases {
		got := r142Folder(t, `require ["fileinto", "envelope"]; `+cond+` { fileinto "Y"; }`)
		if got != want {
			t.Errorf("%s: got %q want %q", cond, got, want)
		}
	}
	// null sender
	msg := r142Msg()
	msg.From = "<>"
	acts, err := ExecuteScript(`require ["fileinto","envelope"]; if envelope :is "from" "" { fileinto "NULL"; }`, msg)
	if err != nil || len(acts) != 1 || acts[0] != (FileintoAction{Folder: "NULL"}) {
		t.Errorf("null sender: %v %#v", err, acts)
	}
}

// F6244: body extension (RFC 5173).
func TestF6244_BodyTest(t *testing.T) {
	cases := map[string]string{
		`if body :contains "SECRET"`:                       "Y",
		`if body :raw :contains "there"`:                   "Y",
		`if body :text :matches "*secret*"`:                "Y",
		`if body :content "text/plain" :contains "hi"`:     "Y",
		`if body :content "text" :contains "hi"`:           "Y",
		`if body :content "image" :contains "hi"`:          "",
		`if body :contains "Subject"`:                      "", // headers are not body
		`if body :comparator "i;octet" :contains "SECRET"`: "",
	}
	for cond, want := range cases {
		got := r142Folder(t, `require ["fileinto", "body"]; `+cond+` { fileinto "Y"; }`)
		if got != want {
			t.Errorf("%s: got %q want %q", cond, got, want)
		}
	}
	// A Body that is the whole message must not match header text.
	full := r142Msg()
	full.Headers = map[string][]string{"Subject": {"Invoice"}}
	full.Body = []byte("Subject: Invoice\r\n\r\nplain body\r\n")
	acts, err := ExecuteScript(`require ["fileinto","body"]; if body :contains "invoice" { fileinto "H"; }`, full)
	if err != nil || len(acts) != 1 {
		t.Fatalf("%v %#v", err, acts)
	}
	if _, ok := acts[0].(KeepAction); !ok {
		t.Errorf("header text leaked into body test: %#v", acts)
	}
	// multipart + base64 part
	mp := r142Msg()
	mp.Headers = map[string][]string{"Content-Type": {`multipart/mixed; boundary="B"`}}
	mp.Body = []byte("--B\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8gYmFzZTY0\r\n--B\r\nContent-Type: application/octet-stream\r\n\r\nbinaryhello\r\n--B--\r\n")
	acts, _ = ExecuteScript(`require ["fileinto","body"]; if body :text :contains "hello base64" { fileinto "B64"; }`, mp)
	if len(acts) != 1 || acts[0] != (FileintoAction{Folder: "B64"}) {
		t.Errorf("base64 text part not decoded: %#v", acts)
	}
	acts, _ = ExecuteScript(`require ["fileinto","body"]; if body :text :contains "binaryhello" { fileinto "BIN"; }`, mp)
	if len(acts) != 1 {
		t.Fatalf("%#v", acts)
	}
	if _, ok := acts[0].(KeepAction); !ok {
		t.Errorf(":text matched a non-text part: %#v", acts)
	}
}

// F6245: relational, regex and the ascii-numeric comparator.
func TestF6245_RelationalRegexNumeric(t *testing.T) {
	cases := map[string]string{
		`if header :count "ge" "to" "1"`:                                    "Y",
		`if address :count "eq" "to" "2"`:                                   "Y",
		`if address :count "gt" "to" "2"`:                                   "",
		`if header :count "eq" "x-missing" "0"`:                             "Y",
		`if header :value "gt" :comparator "i;ascii-numeric" "x-num" "5"`:   "Y",
		`if header :value "lt" :comparator "i;ascii-numeric" "x-num" "5"`:   "",
		`if header :value "eq" :comparator "i;ascii-numeric" "x-num" "042"`: "Y",
		`if header :is :comparator "i;ascii-numeric" "x-num" "42"`:          "Y",
		`if header :value "gt" :comparator "i;ascii-numeric" "subject" "5"`: "Y", // non-numeric sorts last
		`if header :value "ge" "subject" "hello"`:                           "Y", // casemap ordering
		`if header :value "lt" "subject" "a"`:                               "",
		`if header :regex "subject" "^hel+o\\s+W.*d$"`:                      "Y",
		`if header :regex :comparator "i;octet" "subject" "^hello"`:         "",
		`if header :regex "subject" ["^zzz", "WORLD$"]`:                     "Y",
		`if size :over 500`: "Y",
	}
	for cond, want := range cases {
		got := r142Folder(t, `require ["fileinto", "relational", "regex", "comparator-i;ascii-numeric"]; `+cond+` { fileinto "Y"; }`)
		if got != want {
			t.Errorf("%s: got %q want %q", cond, got, want)
		}
	}
}

// F6246: imap4flags and :copy.
func TestF6246_FlagsAndCopy(t *testing.T) {
	acts, err := ExecuteScript(`require ["imap4flags","fileinto"]; addflag "\\Seen"; addflag ["$Work", "\\seen"]; removeflag "$Work"; fileinto :flags "\\Flagged" "A"; keep;`, r142Msg())
	if err != nil || len(acts) != 2 {
		t.Fatalf("%v %#v", err, acts)
	}
	if f := acts[0].(FileintoAction); f.Folder != "A" || f.Flags != `\Flagged` {
		t.Errorf("fileinto: %#v", f)
	}
	if k := acts[1].(KeepAction); k.Flags != `\Seen` {
		t.Errorf("keep flags: %#v", k)
	}
	if got := r142Folder(t, `require ["imap4flags","fileinto"]; setflag ["\\Seen \\Flagged"]; if hasflag "\\flagged" { fileinto "HF"; }`); got != "HF" {
		t.Errorf("hasflag: %q", got)
	}
	if got := r142Folder(t, `require ["imap4flags","fileinto"]; addflag "\\Seen"; if not hasflag :contains "\\Se" { fileinto "HF"; }`); got != "" {
		t.Errorf("hasflag contains: %q", got)
	}
	// :copy keeps the implicit keep.
	acts, err = ExecuteScript(`require ["copy","fileinto"]; fileinto :copy "Archive"; redirect :copy "x@y.org";`, r142Msg())
	if err != nil {
		t.Fatal(err)
	}
	var kept, copies int
	for _, a := range acts {
		switch v := a.(type) {
		case KeepAction:
			kept++
		case FileintoAction:
			if v.Copy {
				copies++
			}
		case RedirectAction:
			if v.Copy {
				copies++
			}
		}
	}
	if kept != 1 || copies != 2 {
		t.Errorf("copy semantics: kept=%d copies=%d %#v", kept, copies, acts)
	}
	// Without :copy the implicit keep is cancelled.
	acts, _ = ExecuteScript(`require "fileinto"; fileinto "Archive";`, r142Msg())
	if len(acts) != 1 {
		t.Errorf("plain fileinto: %#v", acts)
	}
}

// F6247: every supported extension can be required and an unsupported one
// cannot.
func TestF6247_ExtensionList(t *testing.T) {
	m := NewManager()
	for _, e := range []string{"fileinto", "reject", "vacation", "vacation-seconds", "envelope", "body", "relational", "regex", "imap4flags", "copy", "mailbox", "comparator-i;octet", "comparator-i;ascii-casemap", "comparator-i;ascii-numeric"} {
		if err := m.ValidateScript(fmt.Sprintf("require %q;", e)); err != nil {
			t.Errorf("require %q: %v", e, err)
		}
	}
	for _, e := range []string{"variables", "include", "editheader", "date", "subaddress", "foo", ""} {
		if err := m.ValidateScript(fmt.Sprintf("require %q;", e)); err == nil {
			t.Errorf("require %q accepted", e)
		}
	}
}

// F6248: nesting depth and run-time operation limits.
func TestF6248_Limits(t *testing.T) {
	m := NewManager()
	deep := strings.Repeat("if true { ", 40) + "keep;" + strings.Repeat(" }", 40)
	if err := m.ValidateScript(deep); err == nil {
		t.Error("40 nested blocks accepted")
	}
	ok := strings.Repeat("if true { ", 30) + "keep;" + strings.Repeat(" }", 30)
	if err := m.ValidateScript(ok); err != nil {
		t.Errorf("30 nested blocks rejected: %v", err)
	}
	nots := "if " + strings.Repeat("not ", 100000) + "true { keep; }"
	if err := m.ValidateScript(nots); err == nil {
		t.Error("100000 nested nots accepted")
	}
	lists := "if " + strings.Repeat("allof (", 1000) + "true" + strings.Repeat(")", 1000) + " { keep; }"
	if err := m.ValidateScript(lists); err == nil {
		t.Error("1000 nested allof accepted")
	}
	if err := m.ValidateScript(strings.Repeat("keep;", 300000)); err == nil {
		t.Error("script over 1 MiB accepted")
	}
	// Per-run operation budget: many tests over many headers cannot hang.
	var sb strings.Builder
	sb.WriteString(`require "regex"; if anyof (`)
	for i := 0; i < 900; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`header :contains ["a","b","c","d","e","f","g","h","i","j"] ["1","2","3","4","5","6","7","8","9","0","11","12","13","14","15","16","17","18","19","20"]`)
	}
	sb.WriteString(`) { keep; }`)
	msg := r142Msg()
	msg.Headers = map[string][]string{}
	for _, h := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		for i := 0; i < 50; i++ {
			msg.Headers[h] = append(msg.Headers[h], "zzzzzzzzzzzz")
		}
	}
	start := time.Now()
	acts, err := ExecuteScript(sb.String(), msg)
	if err == nil {
		t.Error("expected operation-limit error")
	}
	if len(acts) != 1 {
		t.Errorf("limit error must yield implicit keep, got %#v", acts)
	} else if _, ok := acts[0].(KeepAction); !ok {
		t.Errorf("limit error must yield implicit keep, got %#v", acts)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("limit took %v", time.Since(start))
	}
	// Too many redirects.
	var rb strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&rb, "redirect \"u%d@x.org\";", i)
	}
	if _, err := ExecuteScript(rb.String(), r142Msg()); err == nil {
		t.Error("40 redirects accepted")
	}
}

// F6249: run-time errors and conflicting actions end in the implicit keep and
// are logged, never a lost message or a partial action set.
func TestF6249_RuntimeErrorKeeps(t *testing.T) {
	m := NewManager()
	var logged []string
	m.warn = func(msg string, args ...any) { logged = append(logged, msg) }

	// reject conflicts with keep (RFC 5429 §2.2).
	if err := m.StoreScript("u", "s", `require ["reject","fileinto"]; fileinto "A"; reject "no";`); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveScriptByName("u", "s"); err != nil {
		t.Fatal(err)
	}
	acts, err := m.ProcessMessage("u", r142Msg())
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("want only the implicit keep, got %#v", acts)
	}
	if _, ok := acts[0].(KeepAction); !ok {
		t.Fatalf("want implicit keep, got %#v", acts)
	}
	if len(logged) == 0 {
		t.Error("run-time error was not logged")
	}

	// An unvalidated hostile Script (built without Compile) neither panics nor
	// loses the message.
	bad := &Script{Commands: []Command{{Name: "if", Arguments: []Value{&StringValue{Value: "header", Bare: true}}, Block: nil}}}
	acts, err = NewInterpreter(bad).Execute(r142Msg())
	if err == nil || len(acts) != 1 {
		t.Errorf("want error + keep, got %v %#v", err, acts)
	}
}
