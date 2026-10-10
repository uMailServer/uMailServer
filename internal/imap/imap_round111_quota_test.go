package imap

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const r111User = "user@example.com"

type r111Counter struct {
	mu    sync.Mutex
	used  int64
	limit int64 // 0 = unlimited
}

func (c *r111Counter) adjust(_ string, delta int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if delta > 0 && c.limit > 0 && c.used+delta > c.limit {
		return &storage.QuotaExceededError{User: r111User, Used: c.used, Need: delta, Limit: c.limit}
	}
	c.used += delta
	return nil
}

func (c *r111Counter) get() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}

func r111Store(t *testing.T) (*BboltMailstore, *r111Counter) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ms.Close() })
	for _, mb := range []string{"INBOX", "Archive", "Other"} {
		if err := ms.CreateMailbox(r111User, mb); err != nil {
			t.Fatal(err)
		}
	}
	c := &r111Counter{}
	ms.SetQuotaAdjustFunc(c.adjust)
	return ms, c
}

func r111Append(t *testing.T, ms *BboltMailstore, mb, body string) error {
	t.Helper()
	return ms.AppendMessage(r111User, mb, nil, time.Now(), []byte("From: a@example.com\r\nSubject: s\r\n\r\n"+body+"\r\n"))
}

func r111Size(body string) int64 {
	return int64(len("From: a@example.com\r\nSubject: s\r\n\r\n" + body + "\r\n"))
}

func TestRound111AppendCopyExpungeAdjust(t *testing.T) {
	ms, c := r111Store(t)
	if err := r111Append(t, ms, "INBOX", "one"); err != nil {
		t.Fatal(err)
	}
	n := r111Size("one")
	if c.get() != n {
		t.Fatalf("after append used=%d want %d", c.get(), n)
	}
	// COPY shares the blob: no new bytes.
	if _, _, _, err := ms.CopyMessagesUIDs(r111User, "INBOX", "Archive", "1"); err != nil {
		t.Fatal(err)
	}
	if c.get() != n {
		t.Fatalf("after copy used=%d want %d (blob shared)", c.get(), n)
	}
	// Identical APPEND also dedups.
	if err := r111Append(t, ms, "Other", "one"); err != nil {
		t.Fatal(err)
	}
	if c.get() != n {
		t.Fatalf("after identical append used=%d want %d", c.get(), n)
	}
	del := func(mb string) {
		if err := ms.StoreFlags(r111User, mb, "1:*", []string{"\\Deleted"}, FlagAdd); err != nil {
			t.Fatal(err)
		}
		if err := ms.Expunge(r111User, mb); err != nil {
			t.Fatal(err)
		}
	}
	del("INBOX")
	del("Archive")
	if c.get() != n {
		t.Fatalf("blob still referenced by Other: used=%d want %d", c.get(), n)
	}
	del("Other")
	if c.get() != 0 {
		t.Fatalf("after last expunge used=%d want 0", c.get())
	}
}

func TestRound111OverQuotaRefusedAndRolledBack(t *testing.T) {
	ms, c := r111Store(t)
	c.limit = r111Size("one") + 5
	if err := r111Append(t, ms, "INBOX", "one"); err != nil {
		t.Fatal(err)
	}
	err := r111Append(t, ms, "INBOX", "twotwo")
	if !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Fatalf("over-quota append err=%v", err)
	}
	if c.get() != r111Size("one") {
		t.Fatalf("used=%d after refused append", c.get())
	}
	// Disk-side refusal must roll the reservation back.
	ms.SetQuotaLimitFunc(func(string) int64 { return 1 })
	c.limit = 0
	if err := r111Append(t, ms, "INBOX", "three"); !errors.Is(err, storage.ErrQuotaExceeded) {
		t.Fatalf("disk over-quota err=%v", err)
	}
	if c.get() != r111Size("one") {
		t.Fatalf("reservation leaked on store failure: used=%d", c.get())
	}
}

func TestRound111MoveAndDeleteMailbox(t *testing.T) {
	ms, c := r111Store(t)
	_ = r111Append(t, ms, "INBOX", "mv")
	_ = r111Append(t, ms, "Archive", "del")
	n := r111Size("mv") + r111Size("del")
	if c.get() != n {
		t.Fatalf("used=%d want %d", c.get(), n)
	}
	_, src, _, err := ms.MoveMessagesUIDs(r111User, "INBOX", "Other", "1")
	if err != nil {
		t.Fatal(err)
	}
	if c.get() != n {
		t.Fatalf("move reserved extra: %d", c.get())
	}
	if err := ms.ExpungeUIDs(r111User, "INBOX", src); err != nil {
		t.Fatal(err)
	}
	if c.get() != n {
		t.Fatalf("move-out expunge freed shared blob: %d", c.get())
	}
	// F5931: DELETE mailbox must release the blobs.
	if err := ms.DeleteMailbox(r111User, "Archive"); err != nil {
		t.Fatal(err)
	}
	if want := r111Size("mv"); c.get() != want {
		t.Fatalf("DEFECT F5931: delete mailbox used=%d want %d", c.get(), want)
	}
	if u, _ := ms.msgStore.UserUsage(r111User); u != r111Size("mv") {
		t.Fatalf("DEFECT F5931: disk usage %d after DELETE", u)
	}
}

func TestRound111ConcurrentIdenticalAppendBalanced(t *testing.T) {
	ms, c := r111Store(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = r111Append(t, ms, "INBOX", "same") }()
	}
	wg.Wait()
	if c.get() != r111Size("same") {
		t.Fatalf("used=%d want one blob %d", c.get(), r111Size("same"))
	}
	if err := ms.StoreFlags(r111User, "INBOX", "1:*", []string{"\\Deleted"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	if err := ms.Expunge(r111User, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if c.get() != 0 {
		t.Fatalf("used=%d after expunge want 0", c.get())
	}
}

// F5933/F5934: search keys that cannot be honoured must be BAD, and a
// repeated key must be ANDed, not overwrite the earlier one.
func TestRound111SearchKeys(t *testing.T) {
	c, ms := round82Server(t,
		"From: a@x.com\r\nSubject: alpha beta\r\n\r\none\r\n",
		"From: b@x.com\r\nSubject: alpha\r\n\r\ntwo\r\n")
	c.cmd("s SELECT INBOX")
	if err := ms.StoreFlags(r111User, "INBOX", "2", []string{"\\Draft", "work"}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ cmd, want string }{
		{"SEARCH SUBJECT alpha SUBJECT beta", "* SEARCH 1"},
		{"SEARCH SUBJECT beta SUBJECT alpha", "* SEARCH 1"},
		{"SEARCH FROM a@x.com FROM b@x.com", "* SEARCH"},
		{"SEARCH DRAFT", "* SEARCH 2"},
		{"SEARCH UNDRAFT", "* SEARCH 1"},
		{"SEARCH KEYWORD work", "* SEARCH 2"},
		{"SEARCH UNKEYWORD work", "* SEARCH 1"},
		{"SEARCH SUBJECT", "BAD"},
		{"SEARCH SMALLER abc", "BAD"},
		{"SEARCH LARGER", "BAD"},
		{"SEARCH BOGUSKEY", "BAD"},
		{"SEARCH KEYWORD", "BAD"},
		{"SEARCH HEADER From", "BAD"},
	}
	for i, tc := range cases {
		tag := "q" + string(rune('a'+i))
		out := c.cmd(tag + " " + tc.cmd)
		got := out[0]
		if tc.want == "BAD" {
			got = out[len(out)-1]
			if !strings.HasPrefix(got, tag+" BAD") {
				t.Errorf("DEFECT F5933: %s => %q", tc.cmd, out)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("DEFECT F5933/F5934: %s => %q want %q", tc.cmd, out, tc.want)
		}
	}
}

// F5932: STORE cannot set/clear \Recent and never stores duplicate flags.
func TestRound111StoreFlagSemantics(t *testing.T) {
	ms, _ := r111Store(t)
	_ = r111Append(t, ms, "INBOX", "x")
	flags := func() []string {
		msgs, err := ms.FetchMessages(r111User, "INBOX", "1", []string{"FLAGS"})
		if err != nil || len(msgs) != 1 {
			t.Fatal(err)
		}
		return msgs[0].Flags
	}
	if err := ms.StoreFlags(r111User, "INBOX", "1", []string{"\\Seen", "\\seen", "\\Seen"}, FlagReplace); err != nil {
		t.Fatal(err)
	}
	got := flags()
	if len(got) != 2 || !hasFlag(got, "\\Seen") || !hasFlag(got, "\\Recent") {
		t.Fatalf("DEFECT F5932: replace -> %v (want \\Seen once, server's \\Recent kept)", got)
	}
	if err := ms.StoreFlags(r111User, "INBOX", "1", []string{"\\Recent"}, FlagRemove); err != nil {
		t.Fatal(err)
	}
	if !hasFlag(flags(), "\\Recent") {
		t.Fatalf("DEFECT F5932: client cleared \\Recent")
	}
}

// F5935-F5938: FETCH macros, RFC822.HEADER/TEXT, implicit \Seen, bad items.
func TestRound111FetchItems(t *testing.T) {
	c, _ := round82Server(t, round75Msg("Subject: a"))
	c.cmd("s SELECT INBOX")
	if out := c.cmd("f1 FETCH 1 FAST"); !strings.Contains(out[0], "RFC822.SIZE") {
		t.Errorf("DEFECT F5935: FAST => %q", out)
	}
	if out := c.cmd("f2 FETCH 1 RFC822.HEADER"); !strings.HasPrefix(out[0], "* 1 FETCH (RFC822.HEADER {") {
		t.Errorf("DEFECT F5935: RFC822.HEADER => %q", out)
	}
	if out := c.cmd("f3 FETCH 1 FOO"); !strings.HasPrefix(out[len(out)-1], "f3 BAD") {
		t.Errorf("DEFECT F5937: unknown item => %q", out)
	}
	if out := c.cmd("f4 FETCH 1 BODY.PEEK[]<0.4294967295>"); !strings.Contains(out[0], "BODY[]<0> {") {
		t.Errorf("DEFECT F5938: huge partial count => %q", out)
	}
	if out := c.cmd("f5 FETCH 1 FLAGS"); strings.Contains(out[0], "Seen") {
		t.Fatalf("PEEK set \\Seen: %q", out)
	}
	c.cmd("f6 FETCH 1 RFC822.TEXT")
	if out := c.cmd("f7 FETCH 1 FLAGS"); !strings.Contains(out[0], "\\Seen") {
		t.Errorf("DEFECT F5936: RFC822.TEXT did not set \\Seen: %q", out)
	}
}
