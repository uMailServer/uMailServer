package imap

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func r183Setup(t *testing.T, n int) (*BboltMailstore, string) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	user := "r183@example.com"
	_ = ms.CreateMailbox(user, "INBOX")
	appendMsgs(t, ms, user, n)
	return ms, user
}

func flagsOfUID(t *testing.T, ms *BboltMailstore, user string, uid uint32) []string {
	t.Helper()
	m, err := ms.db.GetMessageMetadata(user, "INBOX", uid)
	if err != nil {
		t.Fatal(err)
	}
	return m.Flags
}

// F6600 (High): after another session expunged, this session's sequence
// numbers silently shifted: "STORE 3 +FLAGS (\Deleted)" flagged the message
// that used to be number 5, so a following EXPUNGE destroyed the wrong mail.
// Numbers must stay stable until the session can be told (NOOP etc.).
func TestF6600_SequenceNumbersStableAfterForeignExpunge(t *testing.T) {
	ms, user := r183Setup(t, 5)
	a := newR183(t, ms, user)
	b := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	b.cmd("SELECT INBOX")
	b.cmd("STORE 1:2 +FLAGS (\\Deleted)")
	b.cmd("EXPUNGE")

	// Gated: A has not yet seen the expunge.
	out := joined(a.cmd("FETCH 1:5 (UID)"))
	for _, want := range []string{"* 3 FETCH (UID 3)", "* 4 FETCH (UID 4)", "* 5 FETCH (UID 5)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "EXPUNGE") {
		t.Fatalf("EXPUNGE during FETCH: %q", out)
	}
	if s := joined(a.cmd("SEARCH ALL")); !strings.Contains(s, "* SEARCH 3 4 5") || strings.Contains(s, "EXPUNGE") {
		t.Fatalf("search: %q", s)
	}
	if s := joined(a.cmd("STORE 3 +FLAGS (\\Flagged)")); strings.Contains(s, "EXPUNGE") || !strings.Contains(s, "* 3 FETCH (FLAGS (\\Flagged))") {
		t.Fatalf("store: %q", s)
	}
	if f := flagsOfUID(t, ms, user, 3); len(f) != 1 || f[0] != "\\Flagged" {
		t.Fatalf("UID 3 flags %v: store hit the wrong message", f)
	}
	if f := flagsOfUID(t, ms, user, 5); len(f) != 0 {
		t.Fatalf("UID 5 flags %v", f)
	}
	// An expunged number answers nothing rather than another message.
	if s := joined(a.cmd("FETCH 1 (UID)")); strings.Contains(s, "FETCH (") {
		t.Fatalf("expunged message 1 answered: %q", s)
	}
	// NOOP is where renumbering is announced.
	s := joined(a.cmd("NOOP"))
	if !strings.Contains(s, "* 2 EXPUNGE\n* 1 EXPUNGE") {
		t.Fatalf("noop: %q", s)
	}
	if s := joined(a.cmd("FETCH 1:3 (UID)")); !strings.Contains(s, "* 1 FETCH (UID 3)") || !strings.Contains(s, "* 3 FETCH (UID 5)") {
		t.Fatalf("renumbered fetch: %q", s)
	}
}

func TestF6600_UIDCommandsAndNewMessages(t *testing.T) {
	ms, user := r183Setup(t, 3)
	a := newR183(t, ms, user)
	b := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	b.cmd("SELECT INBOX")
	b.cmd("STORE 1 +FLAGS (\\Deleted)")
	b.cmd("EXPUNGE")
	appendMsgs(t, ms, user, 1) // UID 4
	// UID commands resolve to the session's numbers; new mail is not shown yet.
	if s := joined(a.cmd("UID FETCH 1:* (FLAGS)")); strings.Contains(s, "UID") && strings.Contains(s, "EXPUNGE") {
		t.Fatalf("expunge in UID FETCH: %q", s)
	}
	if s := joined(a.cmd("UID SEARCH ALL")); strings.Contains(s, "4") || strings.Contains(s, "EXPUNGE") {
		t.Fatalf("uid search leaked unannounced message / expunge: %q", s)
	}
	if s := joined(a.cmd("UID STORE 2 +FLAGS (\\Seen)")); !strings.Contains(s, "* 2 FETCH (UID 2 FLAGS (\\Seen))") {
		t.Fatalf("uid store: %q", s)
	}
	s := joined(a.cmd("NOOP"))
	if !strings.Contains(s, "* 1 EXPUNGE") || !strings.Contains(s, "* 3 EXISTS") {
		t.Fatalf("noop: %q", s)
	}
	// Empty UID match is OK, not an error.
	if s := joined(a.cmd("UID FETCH 99 (FLAGS)")); !strings.Contains(s, " OK ") {
		t.Fatalf("empty uid fetch: %q", s)
	}
}

// F6600: IDLE reports a foreign expunge with the session's own numbering.
func TestF6600_IdleAnnouncesFromSnapshot(t *testing.T) {
	ms, user := r183Setup(t, 4)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	a.c.SetDeadline(time.Now().Add(5 * time.Second))
	a.send("i1 IDLE")
	if l, _ := a.r.ReadString('\n'); !strings.HasPrefix(l, "+") {
		t.Fatalf("no continuation: %q", l)
	}
	_ = ms.StoreFlags(user, "INBOX", "2", []string{"\\Deleted"}, FlagAdd)
	_, _, _ = ms.ExpungeReport(user, "INBOX")
	l, err := a.r.ReadString('\n')
	for err == nil && !strings.Contains(l, "EXPUNGE") {
		l, err = a.r.ReadString('\n')
	}
	if err != nil || strings.TrimSpace(l) != "* 2 EXPUNGE" {
		t.Fatalf("got %q %v", l, err)
	}
	a.send("DONE")
	if out := a.until("i1"); !strings.HasPrefix(out[len(out)-1], "i1 OK") {
		t.Fatalf("%q", out)
	}
}

// F6601: RFC 2177 - a line other than DONE during IDLE was swallowed and IDLE
// kept running; it must end with a tagged BAD, and the subscription must go.
func TestF6601_IdleNonDoneLineEndsWithBad(t *testing.T) {
	ms, user := r183Setup(t, 1)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	hub := GetNotificationHub()
	count := func() int { hub.mu.RLock(); defer hub.mu.RUnlock(); return len(hub.subscribers[user]) }
	a.send("i1 IDLE")
	if l, _ := a.r.ReadString('\n'); !strings.HasPrefix(l, "+") {
		t.Fatalf("no continuation: %q", l)
	}
	a.send("x1 NOOP")
	out := a.until("i1")
	if last := out[len(out)-1]; !strings.HasPrefix(last, "i1 BAD") {
		t.Fatalf("got %q", out)
	}
	// Session is usable again and no subscription leaked.
	if s := a.cmd("NOOP"); !strings.Contains(s[len(s)-1], "OK") {
		t.Fatalf("%q", s)
	}
	for i := 0; i < 50 && count() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if n := count(); n != 0 {
		t.Fatalf("%d leaked subscriptions", n)
	}
}

// F6603: nested NOT/OR/groups each cost a mailbox scan; the nesting is capped.
func TestF6603_SearchNestingCapped(t *testing.T) {
	ms, user := r183Setup(t, 2)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	deep := "SEARCH " + strings.Repeat("NOT ", 5000) + "ALL"
	s := a.cmd(deep)
	if !strings.Contains(s[len(s)-1], " BAD ") {
		t.Fatalf("got %q", s[len(s)-1])
	}
	ok := a.cmd("SEARCH " + strings.Repeat("NOT ", 10) + "ALL")
	if !strings.Contains(joined(ok), "* SEARCH 1 2") {
		t.Fatalf("%q", ok)
	}
}

// F6604: SMALLER 0 matches nothing; LARGER 0 excludes empty messages (the
// zero value was treated as "no key" so SMALLER 0 returned everything).
func TestF6604_SizeZeroBoundaries(t *testing.T) {
	ms, user := r183Setup(t, 2)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	if s := joined(a.cmd("SEARCH SMALLER 0")); strings.Contains(s, "SEARCH 1") {
		t.Fatalf("SMALLER 0: %q", s)
	}
	if s := joined(a.cmd("SEARCH LARGER 0")); !strings.Contains(s, "* SEARCH 1 2") {
		t.Fatalf("LARGER 0: %q", s)
	}
	size := len("From: s0@example.com\r\nSubject: M0\r\n\r\nbody\r\n")
	if s := joined(a.cmd("SEARCH LARGER " + strconv.Itoa(size))); strings.Contains(s, "SEARCH 1") {
		t.Fatalf("LARGER = size must exclude: %q", s)
	}
	if s := joined(a.cmd("SEARCH SMALLER " + strconv.Itoa(size+1))); !strings.Contains(s, "SEARCH 1") {
		t.Fatalf("SMALLER size+1: %q", s)
	}
}

// F6605: "." / ".." hierarchy levels are rejected in new mailbox names.
func TestF6605_RelativeMailboxLevelsRejected(t *testing.T) {
	ms, user := r183Setup(t, 0)
	a := newR183(t, ms, user)
	for _, n := range []string{"../x", "a/../b", "./x", "a/.", ".."} {
		if s := a.cmd("CREATE \"" + n + "\""); !strings.Contains(s[len(s)-1], " NO ") {
			t.Errorf("CREATE %q: %q", n, s[len(s)-1])
		}
	}
	// Encoded separators/dots cannot smuggle a level either.
	for _, n := range []string{"&AC4ALg-", "a&AC8-b"} {
		if s := a.cmd("CREATE " + n); strings.Contains(s[len(s)-1], " OK ") {
			t.Errorf("CREATE %q accepted: %q", n, s[len(s)-1])
		}
	}
	if s := a.cmd("CREATE a&-b"); !strings.Contains(s[len(s)-1], " OK ") {
		t.Errorf("literal & rejected: %q", s)
	}
}

// F6606: a non-PEEK BODY fetch that sets \Seen reports FLAGS in the response.
func TestF6606_ImplicitSeenReportsFlags(t *testing.T) {
	ms, user := r183Setup(t, 1)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	if s := joined(a.cmd("FETCH 1 BODY.PEEK[]")); strings.Contains(s, "FLAGS") {
		t.Fatalf("peek: %q", s)
	}
	if s := joined(a.cmd("FETCH 1 BODY[TEXT]")); !strings.Contains(s, "FLAGS (\\Seen)") {
		t.Fatalf("body: %q", s)
	}
	b := newR183(t, ms, user)
	b.cmd("EXAMINE INBOX")
	for _, c := range []string{"STORE 1 +FLAGS (\\Seen)", "EXPUNGE"} {
		if s := b.cmd(c); !strings.Contains(s[len(s)-1], "[READ-ONLY]") {
			t.Errorf("%s: %q", c, s[len(s)-1])
		}
	}
}

// F6602 (Medium): after DONE the session waited on the finished DONE reader
// for 5 s (idleCleanup), so every command after IDLE stalled.
func TestF6602_NoStallAfterDone(t *testing.T) {
	ms, user := r183Setup(t, 1)
	a := newR183(t, ms, user)
	a.cmd("SELECT INBOX")
	a.send("i1 IDLE")
	a.r.ReadString('\n')
	a.send("DONE")
	a.until("i1")
	start := time.Now()
	if s := a.cmd("NOOP"); !strings.Contains(s[len(s)-1], "OK") {
		t.Fatalf("%q", s)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("command after DONE took %v", d)
	}
}
