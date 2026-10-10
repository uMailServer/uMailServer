package imap

import (
	"strings"
	"testing"
)

// F5560: THREAD / UID THREAD dereferenced msg.Envelope, which the bbolt
// mailstore never fills, so every THREAD panicked and the connection was
// dropped; charset and search keys were ignored and the response was not
// the RFC 5256 "* THREAD (..)(..)" form.

func threadCase(t *testing.T, run func(string) []string, cmd, want string) {
	t.Helper()
	out := run(cmd)
	tag := strings.Fields(cmd)[0]
	if got := round75Line(out, "* THREAD"); got != want || !strings.HasPrefix(round75Tagged(out), tag+" OK") {
		t.Errorf("%s:\n got  %q / %q\n want %q + OK", cmd, got, round75Tagged(out), want)
	}
}

func TestThread_BboltMailstoreDoesNotPanic(t *testing.T) {
	run := round75Session(t,
		round75Msg("From: a@x", "Message-ID: <m1@x>", "Subject: hello", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
		round75Msg("From: b@x", "Message-ID: <m2@x>", "Subject: other", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		round75Msg("From: c@x", "Message-ID: <m3@x>", "In-Reply-To: <m1@x>", "References:\r\n <m1@x>", "Subject: Re: hello", "Date: Fri, 12 Jan 2024 10:00:00 +0000"))
	run("a0 SELECT INBOX")
	threadCase(t, run, "t1 THREAD REFERENCES UTF-8 ALL", "* THREAD (1 3)(2)")
	threadCase(t, run, "t2 THREAD ORDEREDSUBJECT UTF-8 ALL", "* THREAD (1 3)(2)")
	threadCase(t, run, "t3 UID THREAD REFERENCES UTF-8 ALL", "* THREAD (1 3)(2)")
	threadCase(t, run, "t4 THREAD REFERENCES UTF-8 NOT FROM c@x", "* THREAD (1)(2)")
	threadCase(t, run, `t5 thread references "utf-8" SUBJECT "hello"`, "* THREAD (1 3)")
	if got := round75Tagged(run("t6 NOOP")); !strings.HasPrefix(got, "t6 OK") {
		t.Fatalf("session unusable after THREAD: %q", got)
	}
}

func TestThread_ReferencesTree(t *testing.T) {
	run := round75Session(t,
		round75Msg("Message-ID: <a@x>", "Subject: topic", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
		round75Msg("Message-ID: <b@x>", "In-Reply-To: <a@x>", "References: <a@x>", "Subject: Re: topic", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		round75Msg("Message-ID: <c@x>", "References: <a@x>", "Subject: Re: topic", "Date: Fri, 12 Jan 2024 10:00:00 +0000"),
		round75Msg("Message-ID: <d@x>", "References: <a@x> <b@x>", "Subject: Re: topic", "Date: Sat, 13 Jan 2024 10:00:00 +0000"),
		round75Msg("Message-ID: <e@x>", "Subject: unrelated", "Date: Tue, 9 Jan 2024 10:00:00 +0000"))
	run("a0 SELECT INBOX")
	threadCase(t, run, "t1 THREAD REFERENCES UTF-8 ALL", "* THREAD (5)(1 (2 4)(3))")
	threadCase(t, run, "t2 UID THREAD REFERENCES US-ASCII SUBJECT topic", "* THREAD (1 (2 4)(3))")
}

func TestThread_ReferencesDummiesSubjectsLoops(t *testing.T) {
	cases := []struct {
		name string
		msgs []string
		want string
	}{
		{"missing parent keeps a dummy root", []string{
			round75Msg("Message-ID: <x1@x>", "References: <gone@x>", "Subject: lost", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <x2@x>", "References: <gone@x>", "Subject: Re: lost", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		}, "* THREAD ((1)(2))"},
		{"replies without references join by subject", []string{
			round75Msg("Message-ID: <s1@x>", "Subject: hello", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <s2@x>", "Subject: Re: hello", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <s3@x>", "Subject: RE: Hello", "Date: Fri, 12 Jan 2024 10:00:00 +0000"),
		}, "* THREAD (1 (2)(3))"},
		{"same subject non-replies share a dummy", []string{
			round75Msg("Message-ID: <n1@x>", "Subject: news", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <n2@x>", "Subject: news", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		}, "* THREAD ((1)(2))"},
		{"reference loop is broken", []string{
			round75Msg("Message-ID: <l1@x>", "References: <l2@x>", "Subject: a", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <l2@x>", "References: <l1@x>", "Subject: b", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		}, "* THREAD (2 1)"},
		{"duplicate Message-ID and folded References", []string{
			round75Msg("Message-ID: <dup@x>", "Subject: x", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <dup@x>", "Subject: y", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
			round75Msg("Message-ID: <r3@x>", "References: <zzz@x>\r\n\t<dup@x>", "Subject: z", "Date: Fri, 12 Jan 2024 10:00:00 +0000"),
		}, "* THREAD (1 3)(2)"},
		{"no Message-ID at all", []string{
			round75Msg("Subject: p", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
			round75Msg("Subject: q", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		}, "* THREAD (1)(2)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := round75Session(t, c.msgs...)
			run("a0 SELECT INBOX")
			threadCase(t, run, "t1 THREAD REFERENCES UTF-8 ALL", c.want)
		})
	}
}

func TestThread_OrderedSubject(t *testing.T) {
	run := round75Session(t,
		round75Msg("Subject: b", "Date: Wed, 10 Jan 2024 10:00:00 +0000"),
		round75Msg("Subject: a", "Date: Fri, 12 Jan 2024 10:00:00 +0000"),
		round75Msg("Subject: Re: a", "Date: Thu, 11 Jan 2024 10:00:00 +0000"),
		round75Msg("Subject: a", "Date: Sat, 13 Jan 2024 10:00:00 +0000"),
		// no Date: INTERNALDATE 14-Jan-2024 is the sent date
		round75Msg("Subject: [list] a"))
	run("a0 SELECT INBOX")
	threadCase(t, run, "t1 THREAD ORDEREDSUBJECT UTF-8 ALL", "* THREAD (1)(3 (2)(4)(5))")
	threadCase(t, run, "t2 THREAD ORDEREDSUBJECT UTF-8 2:5", "* THREAD (3 (2)(4)(5))")
}

func TestThread_Syntax(t *testing.T) {
	run := round75Session(t)
	run("a0 SELECT INBOX")
	threadCase(t, run, "t0 THREAD REFERENCES UTF-8 ALL", "* THREAD")
	for cmd, want := range map[string]string{
		"t1 THREAD":                               "t1 BAD",
		"t2 THREAD REFERENCES UTF-8":              "t2 BAD",
		"t3 THREAD FOO UTF-8 ALL":                 "t3 BAD",
		"t4 THREAD REFERENCES KOI8-R ALL":         "t4 NO [BADCHARSET",
		"t5 UID THREAD ORDEREDSUBJECT UTF-8 (ALL": "t5 BAD",
	} {
		if got := round75Tagged(run(cmd)); !strings.HasPrefix(got, want) {
			t.Errorf("%s: got %q, want prefix %q", cmd, got, want)
		}
	}
}
