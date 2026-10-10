package imap

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// F6170: mailbox names are modified UTF-7 on the wire (RFC 3501 §5.1.3).
func TestRound135MUTF7Vectors(t *testing.T) {
	cases := map[string]string{
		"Gönderilmiş":        "G&APY-nderilmi&AV8-",
		"A&B":                "A&-B",
		"~peter/mail/台北/日本語": "~peter/mail/&U,BTFw-/&ZeVnLIqe-",
		"INBOX":              "INBOX",
		"😀":                  "&2D3eAA-",
	}
	for utf, wire := range cases {
		if got := encodeMUTF7(utf); got != wire {
			t.Errorf("encode %q = %q want %q", utf, got, wire)
		}
		if got, err := decodeMUTF7(wire); err != nil || got != utf {
			t.Errorf("decode %q = %q,%v want %q", wire, got, err, utf)
		}
	}
	for _, bad := range []string{"&", "&AV8", "&AV8-x&", "&A-", "&ADs-", "&2D0-", "&3gA-", "&!!!-", "\xff"} {
		if _, err := decodeMUTF7(bad); err == nil {
			t.Errorf("decode %q: want error", bad)
		}
	}
}

func FuzzMUTF7RoundTrip(f *testing.F) {
	for _, s := range []string{"", "INBOX", "Gönderilmiş", "a&b", "台北", "😀x", "&-&", "\x01\x7f"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if utf8.ValidString(in) {
			w := encodeMUTF7(in)
			for i := 0; i < len(w); i++ {
				if w[i] >= 0x80 {
					t.Fatalf("non-ASCII in %q", w)
				}
			}
			got, err := decodeMUTF7(w)
			if err != nil || got != in {
				t.Fatalf("round trip %q -> %q -> %q (%v)", in, w, got, err)
			}
		}
		_, _ = decodeMUTF7(in) // never panics
	})
}

func TestRound135WireNames(t *testing.T) {
	c, ms, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	if l := r121Last(c.cmd(`a1 CREATE "G&APY-nderilmi&AV8-"`)); !strings.HasPrefix(l, "a1 OK") {
		t.Fatal(l)
	}
	list, _ := ms.ListMailboxes(r121User, "*")
	found := false
	for _, m := range list {
		if m == "Gönderilmiş" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stored names %q", list)
	}
	out := strings.Join(c.cmd(`a2 LIST "" "*"`), "\n")
	if !strings.Contains(out, `"G&APY-nderilmi&AV8-"`) {
		t.Fatalf("LIST: %s", out)
	}
	if l := r121Last(c.cmd(`a3 SUBSCRIBE "G&APY-nderilmi&AV8-"`)); !strings.HasPrefix(l, "a3 OK") {
		t.Fatal(l)
	}
	out = strings.Join(c.cmd(`a4 LSUB "" "G*"`), "\n")
	if !strings.Contains(out, `"G&APY-nderilmi&AV8-"`) {
		t.Fatalf("LSUB: %s", out)
	}
	out = strings.Join(c.cmd(`a5 STATUS "G&APY-nderilmi&AV8-" (MESSAGES)`), "\n")
	if !strings.Contains(out, `STATUS "G&APY-nderilmi&AV8-" (MESSAGES 0)`) {
		t.Fatalf("STATUS: %s", out)
	}
	if l := r121Last(c.cmd(`a6 SELECT "G&APY-nderilmi&AV8-"`)); !strings.HasPrefix(l, "a6 OK") {
		t.Fatal(l)
	}
	if l := r121Last(c.cmd(`a7 CREATE "Bad&AV8"`)); !strings.HasPrefix(l, "a7 BAD") {
		t.Fatalf("invalid: %s", l)
	}
	if l := r121Last(c.cmd(`a8 CREATE "R&-D"`)); !strings.HasPrefix(l, "a8 OK") {
		t.Fatal(l)
	}
	if out = strings.Join(c.cmd(`a9 LIST "" "R*"`), "\n"); !strings.Contains(out, `"R&-D"`) {
		t.Fatalf("amp: %s", out)
	}
	if l := r121Last(c.cmd(`b1 SELECT inbox`)); !strings.HasPrefix(l, "b1 OK") {
		t.Fatal(l)
	}
}

func r135Append(t *testing.T, c *round82Client, msg string) {
	t.Helper()
	out := c.cmd("ap APPEND INBOX {" + strconv.Itoa(len(msg)) + "+}\r\n" + msg)
	if l := r121Last(out); !strings.HasPrefix(l, "ap OK") {
		t.Fatalf("append: %q", out)
	}
}

// F6173/F6174: ENVELOPE keeps RFC 2047 names raw and renders groups with
// start/end markers.
func TestRound135EnvelopeRawNamesAndGroups(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	r135Append(t, c, "From: =?UTF-8?B?w5Z6Z8O8cg==?= <o@example.com>\r\nTo: Team: a@x.com, \"B, C\" <b@x.com>;, Undisclosed:;, d@y.com\r\nSubject: s\r\n\r\nb\r\n")
	c.cmd("s SELECT INBOX")
	out := strings.Join(c.cmd("e FETCH 1 (ENVELOPE)"), "\n")
	for _, want := range []string{
		`(("=?UTF-8?B?w5Z6Z8O8cg==?=" NIL "o" "example.com"))`,
		`((NIL NIL "Team" NIL)(NIL NIL "a" "x.com")("B, C" NIL "b" "x.com")(NIL NIL NIL NIL)(NIL NIL "Undisclosed" NIL)(NIL NIL NIL NIL)(NIL NIL "d" "y.com"))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
}

// F6175: SEARCH with non-ASCII keys sees encoded headers, folded headers and
// transfer/charset-encoded bodies.
func TestRound135SearchNonASCII(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	r135Append(t, c, "From: =?UTF-8?B?w5Z6Z8O8cg==?= <o@example.com>\r\nSubject: =?UTF-8?Q?I=C5=9F=C4=B1k_Ma=C4=9Fazas=C4=B1?=\r\n\r\nx\r\n")
	r135Append(t, c, "From: a@b.com\r\nSubject: first line\r\n second FOLDED part\r\n\r\nx\r\n")
	r135Append(t, c, "From: a@b.com\r\nSubject: q\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nZ8O2csO8xZ/DvHLDvHo=\r\n")
	r135Append(t, c, "From: a@b.com\r\nSubject: q\r\nContent-Type: text/plain; charset=iso-8859-9\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nal=FDk=FDn\r\n")
	c.cmd("s SELECT INBOX")
	for q, want := range map[string]string{
		`SUBJECT "işık"`:          "* SEARCH 1",
		`SUBJECT "MAĞAZA"`:        "* SEARCH 1",
		`FROM "özgür"`:            "* SEARCH 1",
		`SUBJECT "second folded"`: "* SEARCH 2",
		`BODY "görüşürüz"`:        "* SEARCH 3",
		`TEXT "GÖRÜŞÜRÜZ"`:        "* SEARCH 3",
		`BODY "alıkın"`:           "* SEARCH 4",
	} {
		out := c.cmd("q SEARCH CHARSET UTF-8 " + q)
		if out[0] != want {
			t.Errorf("%s: got %q want %q", q, out[0], want)
		}
	}
}

// F6171: ID is one untagged line, allowed before login, validated.
func TestRound135ID(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	out := c.cmd(`p ID ("name" "Thunderbird" "version" "1 2")`)
	if !strings.HasPrefix(out[0], `* ID (`) || strings.HasPrefix(out[0], "* *") || !strings.HasPrefix(r121Last(out), "p OK") {
		t.Fatalf("pre-login ID: %q", out)
	}
	for _, bad := range []string{`ID (name)`, `ID foo`, `ID ("a" (`} {
		if l := r121Last(c.cmd("p " + bad)); !strings.HasPrefix(l, "p BAD") {
			t.Errorf("%s: %s", bad, l)
		}
	}
	if l := r121Last(c.cmd("p ID NIL")); !strings.HasPrefix(l, "p OK") {
		t.Fatal(l)
	}
}

// F6172: APPEND / STORE validate flags.
func TestRound135FlagValidation(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	for _, f := range []string{`(\Seen \Bogus)`, `(\Recent)`, `(bad(x)`, `(\Seen`, `(a"b)`} {
		out := c.cmd("a APPEND INBOX " + f + " {1}")
		if l := out[0]; !strings.HasPrefix(l, "a BAD") {
			t.Errorf("APPEND %s: %q", f, out)
		}
	}
	r135Append(t, c, "Subject: x\r\n\r\nb\r\n")
	c.cmd("s SELECT INBOX")
	if l := r121Last(c.cmd(`t STORE 1 +FLAGS (\Bogus)`)); !strings.HasPrefix(l, "t BAD") {
		t.Errorf("STORE: %s", l)
	}
	if out := c.cmd(`t STORE 1 +FLAGS (\seen $Label)`); !strings.Contains(strings.Join(out, " "), `\Seen $Label`) {
		t.Errorf("STORE ok: %q", out)
	}
}

// F6176/F6177: LSUB "%" returns a \Noselect parent of a subscribed child, and
// RENAME carries the subscription to the new name.
func TestRound135LsubParentAndRename(t *testing.T) {
	c, _, _ := r121Server(t, nil, nil)
	r121Login(t, c)
	c.cmd("a CREATE Work/Proj")
	c.cmd("a SUBSCRIBE Work/Proj")
	if out := strings.Join(c.cmd(`a LSUB "" "%"`), "\n"); !strings.Contains(out, `LSUB (\Noselect \HasChildren) "/" "Work"`) {
		t.Fatalf("LSUB %%: %s", out)
	}
	c.cmd("a RENAME Work/Proj Work/New")
	out := strings.Join(c.cmd(`a LSUB "" "*"`), "\n")
	if !strings.Contains(out, `"Work/New"`) || strings.Contains(out, `"Work/Proj"`) {
		t.Fatalf("LSUB after rename: %s", out)
	}
}

func FuzzImapAddressList(f *testing.F) {
	for _, s := range []string{`a@b.c`, `Team: a@x.com, "B, C" <b@x.com>;`, `"unterminated <a@b>`, `((`, `:;`, `a: b: c;`, `<>`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) { _ = imapAddressList(in) })
}
