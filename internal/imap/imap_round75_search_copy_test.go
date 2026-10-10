package imap

import (
	"strings"
	"testing"
)

func round75Expect(t *testing.T, run func(string) []string, cmd, prefix, want string) {
	t.Helper()
	out := run(cmd)
	if got := round75Line(out, prefix); got != want {
		t.Errorf("%s: got %q (tagged %q), want %q", cmd, got, round75Tagged(out), want)
	}
}

func round75Status(t *testing.T, run func(string) []string, cmd, want string) {
	t.Helper()
	if got := round75Tagged(run(cmd)); !strings.HasPrefix(got, want) {
		t.Errorf("%s: got %q, want prefix %q", cmd, got, want)
	}
}

// F5561: SORT CC / TO compared nothing, FROM compared the raw header, and a
// message without a Date sorted as the zero time (RFC 5256 §2.2, §3).
func TestSort_AddressKeysAndDateFallback(t *testing.T) {
	run := round75Session(t,
		round75Msg("From: Zed <a@x>", "To: Zoe <z@x>", "Cc: z@x", "Subject: one"),
		round75Msg("From: Alice <z@x>", "To: Adam <a@x>", "Cc: =?UTF-8?Q?B=C3=A9a?= <a@x>, z@x", "Subject: two", "Date: Tue, 2 Jan 2024 10:00:00 +0000"),
		round75Msg("Subject: three", "Date: Fri, 19 Jan 2024 10:00:00 +0000"))
	run("a0 SELECT INBOX")
	for cmd, want := range map[string]string{
		"s1 SORT (FROM) UTF-8 ALL":             "* SORT 3 1 2", // "" < "a" < "z"
		"s2 SORT (CC) UTF-8 ALL":               "* SORT 3 2 1",
		"s3 SORT (TO) UTF-8 ALL":               "* SORT 3 2 1",
		"s4 SORT (REVERSE CC) UTF-8 ALL":       "* SORT 1 2 3",
		"s5 SORT (DATE) UTF-8 ALL":             "* SORT 2 1 3", // 1 has no Date: INTERNALDATE 10-Jan
		"s6 UID SORT (REVERSE DATE) UTF-8 1:2": "* SORT 1 2",
	} {
		round75Expect(t, run, cmd, "* SORT", want)
	}
}

// F5562: SENT* keys matched messages with a missing or unparsable Date and
// compared instants in UTC instead of the Date header's own calendar day.
func TestSearch_SentDateKeys(t *testing.T) {
	run := round75Session(t,
		round75Msg("Subject: no date"),
		round75Msg("Subject: one digit day", "Date: Tue, 2 Jan 2024 10:00:00 +0000"),
		round75Msg("Subject: late west", "Date: Tue, 02 Jan 2024 23:00:00 -0500"),
		round75Msg("Subject: early east", "Date: Wed, 03 Jan 2024 01:00:00 +0300"),
		round75Msg("Subject: comment zone", "Date: 4 Jan 2024 08:00:00 +0000 (UTC)"),
		round75Msg("Subject: garbage", "Date: yesterday"))
	run("a0 SELECT INBOX")
	for cmd, want := range map[string]string{
		"x1 SEARCH SENTBEFORE 1-Jan-2030":                      "* SEARCH 2 3 4 5",
		"x2 SEARCH SENTSINCE 03-Jan-2024":                      "* SEARCH 4 5",
		"x3 SEARCH SENTON 02-Jan-2024":                         "* SEARCH 2 3",
		"x4 SEARCH SENTBEFORE 03-Jan-2024":                     "* SEARCH 2 3",
		"x5 SEARCH SENTON 4-Jan-2024":                          "* SEARCH 5",
		"x6 SEARCH NOT SENTBEFORE 1-Jan-2030":                  "* SEARCH 1 6",
		"x7 SEARCH SENTSINCE 2-Jan-2024 SENTBEFORE 3-Jan-2024": "* SEARCH 2 3",
	} {
		round75Expect(t, run, cmd, "* SEARCH", want)
	}
}

// F5563: an unsupported SEARCH CHARSET answered OK instead of NO [BADCHARSET].
func TestSearch_UnsupportedCharset(t *testing.T) {
	run := round75Session(t, round75Msg("Subject: apple"))
	run("a0 SELECT INBOX")
	round75Status(t, run, "b1 SEARCH CHARSET KOI8-R SUBJECT apple", "b1 NO [BADCHARSET (US-ASCII UTF-8)]")
	round75Status(t, run, "b2 UID SEARCH CHARSET X-UNKNOWN ALL", "b2 NO [BADCHARSET")
	round75Status(t, run, "b3 SORT (ARRIVAL) UTF-8 CHARSET KOI8-R ALL", "b3 NO [BADCHARSET")
	round75Expect(t, run, "b4 SEARCH CHARSET utf-8 SUBJECT apple", "* SEARCH", "* SEARCH 1")
	round75Expect(t, run, `b5 SEARCH CHARSET "US-ASCII" SUBJECT apple`, "* SEARCH", "* SEARCH 1")
}

// F5566: a date key with a one-digit day (valid RFC 3501 date-day) or an
// invalid date was dropped, so the search matched every message.
func TestSearch_DateArguments(t *testing.T) {
	// INTERNALDATEs 10-Jan-2024 and 11-Jan-2024.
	run := round75Session(t, round75Msg("Subject: a"), round75Msg("Subject: b"))
	run("a0 SELECT INBOX")
	for cmd, want := range map[string]string{
		"d1 SEARCH BEFORE 1-Feb-2000":                   "* SEARCH",
		"d2 SEARCH NOT BEFORE 1-Feb-2000":               "* SEARCH 1 2",
		"d3 SEARCH ON 10-jan-2024":                      "* SEARCH 1",
		"d4 SEARCH SINCE 11-Jan-2024":                   "* SEARCH 2",
		`d5 SEARCH SINCE "9-Jan-2024"`:                  "* SEARCH 1 2",
		"d6 UID SEARCH OR ON 10-Jan-2024 ON 1-Jan-2024": "* SEARCH 1",
	} {
		round75Expect(t, run, cmd, "* SEARCH", want)
	}
	for _, cmd := range []string{
		"e1 SEARCH SINCE not-a-date",
		"e2 SEARCH SINCE",
		"e3 SEARCH OR SENTON 32-Jan-2024 ALL",
		"e4 SEARCH (BEFORE 2024-01-01)",
		"e5 SORT (ARRIVAL) UTF-8 BEFORE x",
	} {
		round75Status(t, run, cmd, strings.Fields(cmd)[0]+" BAD")
	}
}

// F5564: COPY / UID COPY sent no COPYUID although UIDPLUS is advertised.
func TestCopy_COPYUID(t *testing.T) {
	run := round75Session(t, round75Msg("Subject: a"), round75Msg("Subject: b"), round75Msg("Subject: c"))
	sel := strings.Join(run("a0 SELECT Archive"), "\n")
	validity := ""
	if i := strings.Index(sel, "[UIDVALIDITY "); i >= 0 {
		validity = strings.Fields(sel[i+len("[UIDVALIDITY "):])[0]
		validity = strings.TrimSuffix(validity, "]")
	}
	if validity == "" {
		t.Fatalf("no UIDVALIDITY in %q", sel)
	}
	run("a1 SELECT INBOX")
	round75Status(t, run, "k1 COPY 2:3 Archive", "k1 OK [COPYUID "+validity+" 2:3 1:2]")
	round75Status(t, run, "k2 UID COPY 1 Archive", "k2 OK [COPYUID "+validity+" 1 3]")
	round75Status(t, run, "k3 COPY 1,3 Archive", "k3 OK [COPYUID "+validity+" 1,3 4:5]")
	// Nothing copied: no COPYUID (an empty uid-set is not valid).
	round75Status(t, run, "k4 UID COPY 99 Archive", "k4 OK COPY completed")
	run("a2 SELECT Archive")
	round75Expect(t, run, "k5 UID SEARCH ALL", "* SEARCH", "* SEARCH 1 2 3 4 5")
}

// F5565: CONDSTORE / QRESYNC were advertised and ENABLE-able, but STORE
// (UNCHANGEDSINCE), FETCH MODSEQ / CHANGEDSINCE, SEARCH MODSEQ and the
// QRESYNC SELECT parameters are not implemented (RFC 7162 §3, §5).
func TestCapability_NoUnimplementedCondstore(t *testing.T) {
	for _, c := range defaultCapabilities() {
		if c == "CONDSTORE" || c == "QRESYNC" {
			t.Errorf("default capabilities advertise %s", c)
		}
	}
	run := round75Session(t, round75Msg("Subject: a"))
	if caps := round75Line(run("q1 CAPABILITY"), "* CAPABILITY"); strings.Contains(caps, "CONDSTORE") || strings.Contains(caps, "QRESYNC") || !strings.Contains(caps, " UIDPLUS") {
		t.Errorf("CAPABILITY: %q", caps)
	}
	out := run("q2 ENABLE CONDSTORE QRESYNC")
	if got := round75Line(out, "* ENABLED"); got != "* ENABLED" || !strings.HasPrefix(round75Tagged(out), "q2 OK") {
		t.Errorf("ENABLE: %q", out)
	}
	if sel := strings.Join(run("q3 SELECT INBOX"), "\n"); strings.Contains(sel, "HIGHESTMODSEQ") {
		t.Errorf("SELECT after ENABLE reports HIGHESTMODSEQ: %q", sel)
	}
}

// F5567: COPY / MOVE to a missing mailbox created it instead of answering
// NO [TRYCREATE] (RFC 3501 §6.4.7).
func TestCopy_MissingDestinationTryCreate(t *testing.T) {
	run := round75Session(t, round75Msg("Subject: a"), round75Msg("Subject: b"))
	run("a0 SELECT INBOX")
	round75Status(t, run, "n1 COPY 1 NoSuchBox", "n1 NO [TRYCREATE]")
	round75Status(t, run, "n2 UID MOVE 2 NoSuchBox", "n2 NO [TRYCREATE]")
	round75Status(t, run, "n3 MOVE 1 Archiv", "n3 NO [TRYCREATE]")
	if list := strings.Join(run(`n4 LIST "" "*"`), "\n"); strings.Contains(list, "NoSuchBox") || strings.Contains(list, `"Archiv"`) {
		t.Errorf("mailbox created: %q", list)
	}
	round75Expect(t, run, "n5 SEARCH UNDELETED", "* SEARCH", "* SEARCH 1 2")
	// INBOX is case-insensitive and existing mailboxes still work.
	round75Status(t, run, "n6 COPY 1 inbox", "n6 OK")
	round75Status(t, run, `n7 COPY 2 "Archive"`, "n7 OK")
	round75Expect(t, run, "n8 SEARCH ALL", "* SEARCH", "* SEARCH 1 2 3")
	if list := strings.Join(run(`n9 LIST "" "*"`), "\n"); strings.Contains(list, `"inbox"`) {
		t.Errorf("lower-case inbox created: %q", list)
	}
}
