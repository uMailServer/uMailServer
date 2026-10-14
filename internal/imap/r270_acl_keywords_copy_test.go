package imap

import (
	"fmt"
	"strings"
	"testing"
)

func r270Keywords(prefix string, n int) string {
	kw := make([]string, n)
	for i := range kw {
		kw[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return strings.Join(kw, " ")
}

// F7440: SETACL on a name that is not a mailbox must fail; otherwise the ACL
// row silently applies to whatever mailbox is created under that name later.
func TestR270_SetACLRequiresExistingMailbox(t *testing.T) {
	c, _ := round82Server(t)
	if got := round75Tagged(c.cmd("a1 SETACL inbox bob lr")); !strings.HasPrefix(got, "a1 OK") {
		t.Fatalf("control SETACL on INBOX: %q", got)
	}
	c.cmd("a2 CREATE P/Q")
	if got := round75Tagged(c.cmd("a3 SETACL P/Q bob lr")); !strings.HasPrefix(got, "a3 OK") {
		t.Fatalf("control SETACL on existing child mailbox: %q", got)
	}
	if got := round75Tagged(c.cmd("a4 SETACL Typo bob lr")); !strings.HasPrefix(got, "a4 NO [NONEXISTENT]") {
		t.Fatalf("DEFECT F7440: SETACL on nonexistent mailbox = %q", got)
	}
	c.cmd("a5 CREATE Typo")
	if out := strings.Join(c.cmd("a6 GETACL Typo"), "\n"); strings.Contains(out, "bob") {
		t.Fatalf("DEFECT F7440: ACL attached to later-created mailbox: %q", out)
	}
	c.cmd("a7 DELETE Typo")
	if got := round75Tagged(c.cmd("a8 SETACL Typo bob lr")); !strings.HasPrefix(got, "a8 NO") {
		t.Fatalf("SETACL on deleted mailbox = %q", got)
	}
}

// F7441: a message carries at most maxKeywordsPerMessage keywords (the JMAP
// policy) through STORE and APPEND.
func TestR270_KeywordCap(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 100))
	c.cmd("k0 SELECT INBOX")
	if got := round75Tagged(c.cmd("k1 STORE 1 +FLAGS.SILENT (" + r270Keywords("a", 100) + ")")); !strings.HasPrefix(got, "k1 OK") {
		t.Fatalf("control: 100 keywords refused: %q", got)
	}
	if got := round75Tagged(c.cmd("k2 STORE 1 +FLAGS.SILENT (extra)")); !strings.HasPrefix(got, "k2 NO [LIMIT]") {
		t.Fatalf("DEFECT F7441: 101st keyword accepted: %q", got)
	}
	if out := strings.Join(c.cmd("k3 FETCH 1 FLAGS"), " "); strings.Contains(out, "extra") {
		t.Fatalf("refused keyword was stored: %q", out)
	}
	// Re-adding a held keyword, replacing within the cap and removals pass.
	if got := round75Tagged(c.cmd("k4 STORE 1 +FLAGS.SILENT (a0 a1)")); !strings.HasPrefix(got, "k4 OK") {
		t.Fatalf("re-adding held keywords refused: %q", got)
	}
	if got := round75Tagged(c.cmd("k5 STORE 1 -FLAGS.SILENT (a0)")); !strings.HasPrefix(got, "k5 OK") {
		t.Fatalf("removal refused: %q", got)
	}
	if got := round75Tagged(c.cmd("k6 STORE 1 +FLAGS.SILENT (extra)")); !strings.HasPrefix(got, "k6 OK") {
		t.Fatalf("keyword after removal refused: %q", got)
	}
	if got := round75Tagged(c.cmd("k7 STORE 1 FLAGS.SILENT (" + r270Keywords("r", 100) + ")")); !strings.HasPrefix(got, "k7 OK") {
		t.Fatalf("replace with 100 refused: %q", got)
	}
	if got := round75Tagged(c.cmd("k8 STORE 1 FLAGS.SILENT (" + r270Keywords("r", 101) + ")")); !strings.HasPrefix(got, "k8 NO [LIMIT]") {
		t.Fatalf("DEFECT F7441: replace with 101 accepted: %q", got)
	}

	app := func(tag string, n int) string {
		msg := round91Msg("two", 100)
		c.write(fmt.Sprintf("%s APPEND INBOX (%s) {%d+}\r\n%s\r\n", tag, r270Keywords("z", n), len(msg), msg))
		return round75Tagged(c.until(tag + " "))
	}
	if got := app("m1", 100); !strings.HasPrefix(got, "m1 OK") {
		t.Fatalf("control: APPEND with 100 keywords refused: %q", got)
	}
	if got := app("m2", 101); !strings.HasPrefix(got, "m2 BAD") {
		t.Fatalf("DEFECT F7441: APPEND with 101 keywords = %q", got)
	}
	// The refused APPEND's literal was discarded: the stream is in sync.
	if got := round75Tagged(c.cmd("m3 NOOP")); !strings.HasPrefix(got, "m3 OK") {
		t.Fatalf("stream out of sync after refused APPEND: %q", got)
	}
}

// F7442: COPY/MOVE of a message sequence number above EXISTS is an error
// (as FETCH/STORE, F6904) and copies nothing; UID forms and "*" are unaffected.
func TestR270_CopyMoveSequenceOutOfRange(t *testing.T) {
	c, _ := round82Server(t, round91Msg("one", 100), round91Msg("two", 100))
	c.cmd("c0 SELECT INBOX")
	for _, cmd := range []string{"COPY 9 Archive", "MOVE 9 Archive", "COPY 3:5 Archive"} {
		if got := round75Tagged(c.cmd("c1 " + cmd)); !strings.HasPrefix(got, "c1 NO Invalid message sequence number") {
			t.Fatalf("DEFECT F7442: %s = %q", cmd, got)
		}
	}
	if out := strings.Join(c.cmd("c2 STATUS Archive (MESSAGES)"), " "); !strings.Contains(out, "MESSAGES 0") {
		t.Fatalf("refused COPY/MOVE changed Archive: %q", out)
	}
	for _, cmd := range []string{"UID COPY 99 Archive", "COPY 1:9 Archive", "COPY 2:* Archive", "COPY 1:* Archive"} {
		if got := round75Tagged(c.cmd("c3 " + cmd)); !strings.HasPrefix(got, "c3 OK") {
			t.Fatalf("control %s = %q", cmd, got)
		}
	}
}
