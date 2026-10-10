package imap

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// Regression tests for RFC 3501 §6.4.4 SEARCH and RFC 5256 SORT semantics
// (F5490–F5493).

// rfcSearchSession selects INBOX holding:
//  1. From b@x, "Re: zebra", arrival 1 Jan
//  2. From a@x, "apple", arrival 2 Jan
//  3. From a@x, "outer" (multipart), arrival 3 Jan
func rfcSearchSession(t *testing.T) func(string) []string {
	t.Helper()
	ms, user, run := persistentSession(t, 0)
	for i, m := range []string{
		"From: b@x\r\nSubject: Re: zebra\r\n\r\nshort\r\n",
		"From: a@x\r\nSubject: apple\r\n\r\nlonger body here\r\n",
		rfcNestedMessage,
	} {
		if err := ms.AppendMessage(user, "INBOX", nil, time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC), []byte(m)); err != nil {
			t.Fatal(err)
		}
	}
	run("a0 SELECT INBOX")
	return run
}

func untaggedLine(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return strings.Join(lines, " | ")
}

// F5493: NOT / OR / parenthesised keys and quoted strings.
func TestSearch_LogicalKeysAndQuotedStrings(t *testing.T) {
	run := rfcSearchSession(t)
	for cmd, want := range map[string]string{
		"x1 SEARCH NOT FROM b@x":                         "* SEARCH 2 3",
		"x2 SEARCH OR FROM b@x SUBJECT apple":            "* SEARCH 1 2",
		"x3 SEARCH (FROM a@x SUBJECT apple)":             "* SEARCH 2",
		"x4 SEARCH NOT (OR FROM b@x SUBJECT apple)":      "* SEARCH 3",
		`x5 SEARCH SUBJECT "Re: zebra"`:                  "* SEARCH 1",
		`x6 SEARCH CHARSET UTF-8 SUBJECT "apple"`:        "* SEARCH 2",
		"x7 UID SEARCH NOT UID 1":                        "* SEARCH 2 3",
		"x8 SEARCH OR (FROM a@x SUBJECT apple) FROM b@x": "* SEARCH 1 2",
	} {
		if got := untaggedLine(run(cmd), "* SEARCH"); got != want {
			t.Errorf("%s => %q, want %q", cmd, got, want)
		}
	}
	for _, cmd := range []string{"z1 SEARCH (FROM a@x", "z2 SEARCH OR FROM a@x"} {
		tag := strings.Fields(cmd)[0]
		if got := untaggedLine(run(cmd), tag+" "); !strings.HasPrefix(got, tag+" BAD") {
			t.Errorf("%s => %q, want BAD", cmd, got)
		}
	}
}

// F5490: RFC 5256 SORT syntax, search keys honoured, UID SORT returns UIDs.
func TestSort_RFC5256Syntax(t *testing.T) {
	run := rfcSearchSession(t)
	for cmd, want := range map[string]string{
		"s1 SORT (ARRIVAL) UTF-8 ALL":             "* SORT 1 2 3",
		"s2 SORT (ARRIVAL) UTF-8 FROM a@x":        "* SORT 2 3",
		"s3 UID SORT (REVERSE ARRIVAL) UTF-8 ALL": "* SORT 3 2 1",
		"s4 SORT (SUBJECT) US-ASCII ALL":          "* SORT 2 3 1",
	} {
		if got := untaggedLine(run(cmd), "* SORT"); got != want {
			t.Errorf("%s => %q, want %q", cmd, got, want)
		}
	}
	if got := untaggedLine(run("s5 SORT (DATE) ISO-8859-9 ALL"), "s5 "); !strings.HasPrefix(got, "s5 NO [BADCHARSET") {
		t.Errorf("unsupported charset => %q, want NO [BADCHARSET", got)
	}
}

// F5490: a SORT that reached the mailstore dereferenced the nil Envelope
// and dropped the connection.
func TestSort_DoesNotPanic(t *testing.T) {
	ms, _, _ := persistentSession(t, 2)
	for _, cmd := range []string{"p1 SORT ARRIVAL", "p2 SORT (ARRIVAL) UTF-8 ALL", "p3 UID SORT (SUBJECT) UTF-8 ALL"} {
		client, srv := net.Pipe()
		s := NewSession(srv, NewServer(&Config{}, ms))
		s.state, s.user = StateSelected, "user@example.com"
		s.selected = &Mailbox{Name: "INBOX"}
		go func() { _, _ = io.Copy(io.Discard, client) }()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", cmd, r)
				}
			}()
			_ = s.handleCommand(cmd)
		}()
		_ = srv.Close()
		_ = client.Close()
	}
}

// F5491 / F5492: ascending by default, REVERSE flips only the next key,
// SUBJECT compares RFC 5256 base subjects.
func TestSort_DirectionAndBaseSubject(t *testing.T) {
	msgs := round005Messages(4)
	for i := range msgs {
		msgs[i].InternalDate = round005Arrival.Add(time.Duration(i) * time.Hour)
	}
	for spec, want := range map[string]string{
		"ARRIVAL":         "[1 2 3 4]",
		"REVERSE ARRIVAL": "[4 3 2 1]",
	} {
		c, err := parseSortCriteria(strings.Fields(spec))
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(sortMessagesByCriteria(msgs, c, round005SeqNums(4))); got != want {
			t.Errorf("SORT (%s) => %s, want %s", spec, got, want)
		}
	}
	for i, subj := range []string{"Re: apple", "banana", "Fwd: cherry", "[list] RE: [Fwd: date]"} {
		msgs[i].Subject = subj
	}
	got := fmt.Sprint(sortMessagesByCriteria(msgs, []SortCriterion{{Field: "SUBJECT"}}, round005SeqNums(4)))
	if got != "[1 2 3 4]" {
		t.Errorf("SORT (SUBJECT) => %s, want [1 2 3 4] (base subjects apple, banana, cherry, date)", got)
	}
}
