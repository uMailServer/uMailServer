package imap

import (
	"bufio"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
)

// expungeMoveSession returns a selected INBOX session on a real BboltMailstore
// (INBOX with n messages, plus an empty Archive) and a run function that
// executes one command line and returns the response lines.
func expungeMoveSession(t *testing.T, n int) (*BboltMailstore, string, func(line string) []string) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	t.Cleanup(func() { ms.Close() })
	user := "user@example.com"
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(user, mb); err != nil {
			t.Fatalf("CreateMailbox: %v", err)
		}
	}
	appendMsgs(t, ms, user, n)
	run := func(line string) []string {
		client, srvConn := net.Pipe()
		s := NewSession(srvConn, NewServer(&Config{}, ms))
		s.state, s.user, s.selected = StateSelected, user, &Mailbox{Name: "INBOX"}
		var mu sync.Mutex
		var out []string
		done := make(chan struct{})
		go func() {
			defer close(done)
			r := bufio.NewReader(client)
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					_, _ = io.Copy(io.Discard, r)
					return
				}
				mu.Lock()
				out = append(out, strings.TrimRight(l, "\r\n"))
				mu.Unlock()
			}
		}()
		_ = s.handleCommand(line)
		_ = srvConn.Close()
		<-done
		_ = client.Close()
		return out
	}
	return ms, user, run
}

func inboxUIDs(t *testing.T, ms *BboltMailstore, user string) []uint32 {
	var uids []uint32
	for _, m := range mailboxMsgs(t, ms, user, "INBOX") {
		uids = append(uids, m.UID)
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	return uids
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// F4955: RFC 4315 UID EXPUNGE removes only the \Deleted messages in the set.
func TestUIDExpunge_RemovesOnlyDeletedInSet(t *testing.T) {
	ms, user, run := expungeMoveSession(t, 4)
	if err := ms.StoreFlags(user, "INBOX", "2,3", []string{`\Deleted`}, FlagAdd); err != nil {
		t.Fatalf("StoreFlags: %v", err)
	}
	out := run("u1 UID EXPUNGE 2")
	got := inboxUIDs(t, ms, user)
	if len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 4 {
		t.Errorf("UIDs after UID EXPUNGE 2 = %v, want [1 3 4]", got)
	}
	if !containsLine(out, "* 2 EXPUNGE") || !containsLine(out, "u1 OK UID EXPUNGE completed") {
		t.Errorf("responses = %q", out)
	}
}

// F4958: MOVE within the same mailbox with "*" must not flag the new copies.
func TestMoveMessages_SameMailboxStarKeepsCopies(t *testing.T) {
	ms, user, _ := expungeMoveSession(t, 3)
	if err := ms.MoveMessages(user, "INBOX", "INBOX", "2:*"); err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}
	var live []string
	for _, m := range mailboxMsgs(t, ms, user, "INBOX") {
		if !hasFlag(m.Flags, `\Deleted`) {
			live = append(live, m.Subject)
		}
	}
	sort.Strings(live)
	if strings.Join(live, ",") != "M0,M1,M2" {
		t.Errorf("undeleted after MOVE 2:* INBOX = %v, want [M0 M1 M2]", live)
	}
}

// F4959: RFC 6851 MOVE expunges the moved messages from the source and
// reports them, without touching other \Deleted messages.
func TestMove_ExpungesMovedSourceMessages(t *testing.T) {
	ms, user, run := expungeMoveSession(t, 3)
	if err := ms.StoreFlags(user, "INBOX", "1", []string{`\Deleted`}, FlagAdd); err != nil {
		t.Fatalf("StoreFlags: %v", err)
	}
	out := run("m1 MOVE 2 Archive")
	got := inboxUIDs(t, ms, user)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("INBOX UIDs after MOVE = %v, want [1 3]", got)
	}
	if n := len(mailboxMsgs(t, ms, user, "Archive")); n != 1 {
		t.Errorf("Archive has %d messages, want 1", n)
	}
	if !containsLine(out, "* 2 EXPUNGE") || containsLine(out, "* 1 EXPUNGE") {
		t.Errorf("responses = %q", out)
	}
}
