package imap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const round82User = "user@example.com"

// round82UIDValidity returns the UIDVALIDITY STATUS reports for mailbox.
func round82UIDValidity(t *testing.T, c *round82Client, mailbox string) string {
	t.Helper()
	out := strings.Join(c.cmd("v0 STATUS "+mailbox+" (UIDVALIDITY)"), "\n")
	i := strings.Index(out, "UIDVALIDITY ")
	if i < 0 {
		t.Fatalf("no UIDVALIDITY in %q", out)
	}
	return strings.TrimRight(strings.Fields(out[i+len("UIDVALIDITY "):])[0], ")")
}

// round82Search returns the "* SEARCH ..." line of a UID SEARCH in the
// selected mailbox.
func round82Search(c *round82Client, tag, keys string) string {
	return round75Line(c.cmd(tag+" UID SEARCH "+keys), "* SEARCH")
}

// round82Messages returns the number of messages mailbox holds.
func round82Messages(t *testing.T, c *round82Client, mailbox string) string {
	t.Helper()
	out := strings.Join(c.cmd("n0 STATUS "+mailbox+" (MESSAGES)"), "\n")
	i := strings.Index(out, "MESSAGES ")
	if i < 0 {
		t.Fatalf("no MESSAGES in %q", out)
	}
	return strings.TrimRight(strings.Fields(out[i+len("MESSAGES "):])[0], ")")
}

// round82Unreadable makes the stored body of INBOX message uid fail to read
// (a transient I/O or permission error) and restores it on cleanup.
func round82Unreadable(t *testing.T, ms *BboltMailstore, uid uint32) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("file modes do not restrict root")
	}
	meta, err := ms.db.GetMessageMetadata(round82User, "INBOX", uid)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(ms.dataDir, "messages", round82User, meta.MessageID[:2], meta.MessageID[2:4], meta.MessageID)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
}

func round82ThreeMsgs() []string {
	return []string{
		round75Msg("Subject: one"), round75Msg("Subject: two"), round75Msg("Subject: three"),
	}
}

// F5640: CopyMessagesUIDs skipped every message it could not read, store or
// index ("continue") and still answered OK; MoveMessages then flagged the
// whole set \Deleted and handleMove expunged it, deleting the unreadable
// original and its body although it was never copied (RFC 3501 §6.4.7: a
// failed COPY must leave the destination unchanged; RFC 6851 §3.3).
func TestRound82_PartialCopyAndMoveFailClosed(t *testing.T) {
	t.Run("control all readable", func(t *testing.T) {
		c, _ := round82Server(t, round82ThreeMsgs()...)
		c.cmd("s0 SELECT INBOX")
		if got := round75Tagged(c.cmd("m1 MOVE 1:3 Archive")); !strings.HasPrefix(got, "m1 OK") {
			t.Fatalf("MOVE: %q", got)
		}
		if n := round82Messages(t, c, "Archive"); n != "3" {
			t.Errorf("Archive has %s messages, want 3", n)
		}
		if got := round82Search(c, "m2", "ALL"); got != "* SEARCH" {
			t.Errorf("INBOX after MOVE: %q", got)
		}
	})
	t.Run("copy", func(t *testing.T) {
		c, ms := round82Server(t, round82ThreeMsgs()...)
		round82Unreadable(t, ms, 2)
		c.cmd("s0 SELECT INBOX")
		got := round75Tagged(c.cmd("c1 COPY 1:3 Archive"))
		if !strings.HasPrefix(got, "c1 NO") {
			t.Errorf("DEFECT F5640: COPY with an unreadable message answered %q, want NO", got)
		}
		if n := round82Messages(t, c, "Archive"); n != "0" {
			t.Errorf("DEFECT F5640: failed COPY left %s message(s) in the destination, want 0", n)
		}
	})
	t.Run("move", func(t *testing.T) {
		c, ms := round82Server(t, round82ThreeMsgs()...)
		round82Unreadable(t, ms, 2)
		c.cmd("s0 SELECT INBOX")
		got := round75Tagged(c.cmd("m1 MOVE 1:3 Archive"))
		if !strings.HasPrefix(got, "m1 NO") {
			t.Errorf("DEFECT F5640: MOVE with an unreadable message answered %q, want NO", got)
		}
		if got := round82Search(c, "m2", "ALL"); got != "* SEARCH 1 2 3" {
			t.Errorf("DEFECT F5640: INBOX after failed MOVE = %q, want \"* SEARCH 1 2 3\" (source messages deleted without being copied)", got)
		}
		if n := round82Messages(t, c, "Archive"); n != "0" {
			t.Errorf("DEFECT F5640: failed MOVE left %s message(s) in the destination, want 0", n)
		}
	})
}

// round82Append sends APPEND with one literal per message and returns the
// reply lines.
func round82Append(t *testing.T, c *round82Client, tag, mailbox string, msgs ...string) []string {
	t.Helper()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s APPEND %s {%d}\r\n", tag, mailbox, len(msgs[0]))
	c.write(sb.String())
	c.until("+")
	for i, m := range msgs {
		if i+1 < len(msgs) {
			c.write(fmt.Sprintf("%s {%d}\r\n", m, len(msgs[i+1])))
			c.until("+")
			continue
		}
		c.write(m + "\r\n")
	}
	return c.until(tag + " ")
}

// F5641: UIDPLUS is advertised but APPEND answered a bare "OK APPEND
// completed" and MOVE sent no COPYUID (RFC 4315 §3, RFC 6851 §4.3), so a
// client cannot learn the UID of a message it just stored.
func TestRound82_AppendUIDAndMoveCopyUID(t *testing.T) {
	c, _ := round82Server(t, round82ThreeMsgs()...)
	validity := round82UIDValidity(t, c, "Archive")
	one, two := round75Msg("Subject: x1"), round75Msg("Subject: x2")

	out := round82Append(t, c, "p1", "Archive", one)
	if got, want := round75Tagged(out), "p1 OK [APPENDUID "+validity+" 1] APPEND completed"; got != want {
		t.Errorf("DEFECT F5641: APPEND reply %q, want %q", got, want)
	}
	out = round82Append(t, c, "p2", "Archive", one+"y", two+"y")
	if got, want := round75Tagged(out), "p2 OK [APPENDUID "+validity+" 2:3] APPEND completed"; got != want {
		t.Errorf("DEFECT F5641: MULTIAPPEND reply %q, want %q", got, want)
	}

	c.cmd("s0 SELECT INBOX")
	out = c.cmd("m1 UID MOVE 1,3 Archive")
	if got, want := round75Line(out, "* OK [COPYUID"), "* OK [COPYUID "+validity+" 1,3 4:5] Moved"; got != want {
		t.Errorf("DEFECT F5641: MOVE untagged response %q, want %q", got, want)
	}
	if got := round75Tagged(out); !strings.HasPrefix(got, "m1 OK") {
		t.Errorf("MOVE tagged reply %q", got)
	}
	if got := round82Search(c, "m2", "ALL"); got != "* SEARCH 2" {
		t.Errorf("INBOX after MOVE: %q, want \"* SEARCH 2\"", got)
	}
}
