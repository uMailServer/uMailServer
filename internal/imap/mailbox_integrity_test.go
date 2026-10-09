package imap

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// persistentSession returns a persistent authenticated session over a real
// BboltMailstore with INBOX (n messages) and Archive; run executes one line.
func persistentSession(t *testing.T, n int) (*BboltMailstore, string, func(line string) []string) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { ms.Close() })
	user := "user@example.com"
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(user, mb); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	appendMsgs(t, ms, user, n)
	client, srvConn := net.Pipe()
	s := NewSession(srvConn, NewServer(&Config{}, ms))
	s.state, s.user = StateAuthenticated, user
	lines := make(chan string, 1024)
	go func() {
		r := bufio.NewReader(client)
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				_, _ = io.Copy(io.Discard, r)
				close(lines)
				return
			}
			lines <- strings.TrimRight(l, "\r\n")
		}
	}()
	var mu sync.Mutex
	t.Cleanup(func() { _ = srvConn.Close(); _ = client.Close() })
	run := func(line string) []string {
		mu.Lock()
		defer mu.Unlock()
		tag := strings.Fields(line)[0]
		done := make(chan struct{})
		go func() { defer close(done); _ = s.handleCommand(line) }()
		var out []string
		timeout := time.After(10 * time.Second)
	loop:
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					break loop
				}
				out = append(out, l)
				if strings.HasPrefix(l, tag+" ") {
					break loop
				}
			case <-timeout:
				t.Fatalf("no tagged reply to %q (got %q)", line, out)
			}
		}
		<-done
		return out
	}
	return ms, user, run
}

// F5065: message blobs are content-addressed and shared by COPY/MOVE copies;
// expunging the source must not delete the file the destination still uses.
func TestExpunge_KeepsBlobSharedWithCopy(t *testing.T) {
	ms, user, run := persistentSession(t, 2)
	run("a1 SELECT INBOX")
	run("a2 COPY 2 Archive")
	run("a3 MOVE 1 Archive")
	run(`a4 STORE 1 +FLAGS (\Deleted)`)
	run("a5 EXPUNGE")
	if got := inboxUIDs(t, ms, user); len(got) != 0 {
		t.Fatalf("INBOX not emptied: %v", got)
	}
	run("a6 SELECT Archive")
	out := strings.Join(run("a7 FETCH 1:* (BODY.PEEK[])"), "\n")
	if !strings.Contains(out, "Subject: M0") || !strings.Contains(out, "Subject: M1") {
		t.Fatalf("Archive copies lost their bodies: %q", out)
	}
}

// F5066: STORE must keep the system-flag backslash so EXPUNGE/SEARCH see it.
func TestStore_SystemFlagReachesExpunge(t *testing.T) {
	ms, user, run := persistentSession(t, 2)
	run("a1 SELECT INBOX")
	st := strings.Join(run(`a2 STORE 1 +FLAGS (\Deleted \Seen)`), "\n")
	if !strings.Contains(st, `FLAGS (\Deleted \Seen)`) {
		t.Fatalf("STORE echo %q", st)
	}
	if s := strings.Join(run("a3 SEARCH DELETED"), "\n"); !strings.Contains(s, "* SEARCH 1") {
		t.Fatalf("SEARCH DELETED %q", s)
	}
	run("a4 EXPUNGE")
	if got := inboxUIDs(t, ms, user); len(got) != 1 || got[0] != 2 {
		t.Fatalf("EXPUNGE after STORE \\Deleted left %v", got)
	}
}

// F5067: FETCH BODY[section]<partial> / BODY.PEEK[...] return the data;
// only the non-PEEK form sets \Seen.
func TestFetch_BodySectionReturnsData(t *testing.T) {
	ms, user, run := persistentSession(t, 1)
	run("a1 SELECT INBOX")
	cases := map[string]string{
		"a2 FETCH 1 (BODY.PEEK[])":                        "BODY[] {43}\r\nFrom: s0@example.com",
		"a3 FETCH 1 (BODY.PEEK[HEADER.FIELDS (SUBJECT)])": "BODY[HEADER.FIELDS (SUBJECT)] {15}\r\nSubject: M0\r\n",
		"a4 FETCH 1 (BODY.PEEK[TEXT])":                    "BODY[TEXT] {6}\r\nbody",
		"a5 FETCH 1 (BODY.PEEK[]<6.2>)":                   "BODY[]<6> {2}\r\ns0",
	}
	for cmd, want := range cases {
		if out := strings.Join(run(cmd), "\r\n"); !strings.Contains(out, want) {
			t.Fatalf("%s: want %q in %q", cmd, want, out)
		}
	}
	if hasFlag(mailboxMsgs(t, ms, user, "INBOX")[0].Flags, `\Seen`) {
		t.Fatal("BODY.PEEK set \\Seen")
	}
	run("a6 FETCH 1 (BODY[])")
	if !hasFlag(mailboxMsgs(t, ms, user, "INBOX")[0].Flags, `\Seen`) {
		t.Fatal("BODY[] did not set \\Seen")
	}
}

// F5068: a mailbox opened with EXAMINE is read-only (RFC 3501 §6.3.2/§6.4.2).
func TestExamine_IsReadOnly(t *testing.T) {
	ms, user, run := persistentSession(t, 2)
	if err := ms.StoreFlags(user, "INBOX", "2", []string{`\Deleted`}, FlagAdd); err != nil {
		t.Fatal(err)
	}
	run("a1 EXAMINE INBOX")
	for _, cmd := range []string{`a2 STORE 1 +FLAGS (\Flagged)`, "a3 EXPUNGE", "a4 UID EXPUNGE 1:*", "a5 MOVE 1 Archive"} {
		out := run(cmd)
		if l := out[len(out)-1]; !strings.Contains(l, " NO ") {
			t.Fatalf("%s allowed on EXAMINE: %q", cmd, l)
		}
	}
	run("a6 CLOSE")
	if got := inboxUIDs(t, ms, user); len(got) != 2 {
		t.Fatalf("EXAMINE session changed the mailbox: %v", got)
	}
	if flags := mailboxMsgs(t, ms, user, "INBOX")[0].Flags; hasFlag(flags, `\Flagged`) {
		t.Fatalf("flags changed: %v", flags)
	}
}
