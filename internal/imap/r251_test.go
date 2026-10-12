package imap

import (
	"strings"
	"testing"
)

// F7280: \Recent is session state (RFC 3501 §2.3.2). SELECT reports N RECENT,
// so SEARCH RECENT/NEW/OLD and FETCH FLAGS in that session must agree.
func TestF7280_RecentVisibleToSearchAndFetch(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 100), round91Msg("two", 100))
	sel := strings.Join(c.cmd("r1 SELECT INBOX"), "\n")
	if !strings.Contains(sel, "2 RECENT") {
		t.Fatalf("precondition: %q", sel)
	}
	if out := strings.Join(c.cmd("r2 SEARCH RECENT"), "\n"); !strings.Contains(out, "* SEARCH 1 2") {
		t.Fatalf("DEFECT F7280: SEARCH RECENT = %q", out)
	}
	if out := strings.Join(c.cmd("r3 SEARCH NEW"), "\n"); !strings.Contains(out, "* SEARCH 1 2") {
		t.Fatalf("DEFECT F7280: SEARCH NEW = %q", out)
	}
	if out := strings.Join(c.cmd("r4 SEARCH OLD"), "\n"); strings.Contains(out, "* SEARCH 1") || strings.Contains(out, "* SEARCH 2") {
		t.Fatalf("DEFECT F7280: SEARCH OLD = %q", out)
	}
	if out := strings.Join(c.cmd("r5 FETCH 1 FLAGS"), "\n"); !strings.Contains(out, "\\Recent") {
		t.Fatalf("DEFECT F7280: FETCH FLAGS lacks \\Recent: %q", out)
	}
	c.cmd("r6 STORE 1 +FLAGS (\\Seen)")
	if out := strings.Join(c.cmd("r7 SEARCH NEW"), "\n"); !strings.Contains(out, "* SEARCH 2") || strings.Contains(out, "SEARCH 1") {
		t.Fatalf("DEFECT F7280: SEARCH NEW after \\Seen = %q", out)
	}
	// A later session no longer sees them as recent.
	c.cmd("r8 SELECT INBOX")
	if out := strings.Join(c.cmd("r9 FETCH 1 FLAGS"), "\n"); strings.Contains(out, "\\Recent") {
		t.Fatalf("second SELECT: messages must no longer be \\Recent: %q", out)
	}
}

// F7281: LSUB of a subscribed name whose mailbox no longer exists is \Noselect.
func TestF7281_LsubDeletedIsNoselect(t *testing.T) {
	c, _ := round82Server(t)
	c.cmd("l1 CREATE Gone")
	c.cmd("l2 SUBSCRIBE Gone")
	c.cmd("l3 DELETE Gone")
	out := strings.Join(c.cmd(`l4 LSUB "" *`), "\n")
	if !strings.Contains(out, "Gone") || !strings.Contains(out, `\Noselect`) {
		t.Fatalf("DEFECT F7281: LSUB = %q", out)
	}
	if strings.Contains(out, "(\\HasNoChildren) \"/\" Archive") {
		t.Fatalf("unexpected: %q", out)
	}
}

// F7282: RENAME into the mailbox's own descendant must fail.
func TestF7282_RenameIntoOwnDescendant(t *testing.T) {
	c, _ := round82Server(t)
	c.cmd("n1 CREATE P")
	got := round75Tagged(c.cmd("n2 RENAME P P/Q/R"))
	if strings.HasPrefix(got, "n2 OK") {
		t.Fatalf("DEFECT F7282: RENAME P P/Q/R succeeded: %q", got)
	}
	if out := strings.Join(c.cmd(`n3 LIST "" *`), "\n"); !strings.Contains(out, `"/" "P"`) || strings.Contains(out, "P/Q") {
		t.Fatalf("hierarchy changed: %q", out)
	}
}

// F7283: a section that does not exist is refused, not answered with an empty literal.
func TestF7283_MissingSectionRefused(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 100))
	c.cmd("s1 SELECT INBOX")
	got := strings.Join(c.cmd("s2 FETCH 1 BODY.PEEK[1.1]"), "\n")
	t.Logf("F7283 observed: %q", got)
	if !strings.Contains(got, "s2 NO") {
		t.Fatalf("DEFECT F7283: %q", got)
	}
}
