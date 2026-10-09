package imap

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func rfcUntagged(lines []string, prefix string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return "<none>"
}

func rfcListNames(lines []string) string {
	var names []string
	for _, l := range lines {
		if strings.HasPrefix(l, "* LIST ") {
			i := strings.LastIndex(strings.TrimSuffix(l, `"`), `"`)
			names = append(names, strings.Trim(l[i:], `"`))
		}
	}
	return strings.Join(names, ",")
}

// F5215: EXAMINE and STATUS must not end the \Recent state (RFC 3501
// §6.3.2, §6.3.10); SELECT still does.
func TestExamineAndStatus_KeepRecent(t *testing.T) {
	_, _, run := persistentSession(t, 2)
	run("a1 EXAMINE INBOX")
	if got := rfcUntagged(run("a2 STATUS INBOX (RECENT)"), "* STATUS"); got != `* STATUS "INBOX" (RECENT 2)` {
		t.Fatalf("STATUS after EXAMINE: %q", got)
	}
	run("a3 STATUS INBOX (RECENT)")
	if got := strings.Join(run("a4 SELECT INBOX"), "\n"); !strings.Contains(got, "* 2 RECENT") {
		t.Fatalf("SELECT after EXAMINE/STATUS: %q", got)
	}
	if got := strings.Join(run("a5 SELECT INBOX"), "\n"); !strings.Contains(got, "* 0 RECENT") {
		t.Fatalf("second SELECT must see \\Recent cleared: %q", got)
	}
}

// F5216: APPEND's date-time argument becomes the INTERNALDATE (RFC 3501
// §6.3.11); an invalid one is rejected.
func TestAppend_DateTimeSetsInternalDate(t *testing.T) {
	const msg = "From: a@example.com\r\nSubject: old\r\n\r\nhi\r\n" // 41 octets
	do := func(cmd string) (*BboltMailstore, string, string) {
		ms, err := NewBboltMailstore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ms.Close() })
		user := "user@example.com"
		if err := ms.CreateMailbox(user, "INBOX"); err != nil {
			t.Fatal(err)
		}
		client, srvConn := net.Pipe()
		t.Cleanup(func() { _ = srvConn.Close(); _ = client.Close() })
		s := NewSession(srvConn, NewServer(&Config{}, ms))
		s.state, s.user = StateAuthenticated, user
		go func() { _, _ = client.Write([]byte(msg + "\r\n")) }()
		reply := make(chan string, 1)
		go func() {
			r := bufio.NewReader(client)
			for {
				l, err := r.ReadString('\n')
				if err != nil || strings.HasPrefix(l, "a1 ") {
					reply <- strings.TrimSpace(l)
					_, _ = io.Copy(io.Discard, r)
					return
				}
			}
		}()
		_ = s.handleCommand(cmd)
		return ms, user, <-reply
	}
	want := time.Date(2021, 1, 5, 10, 0, 0, 0, time.UTC)
	ms, user, r := do(`a1 APPEND INBOX (\Seen) " 5-Jan-2021 12:00:00 +0200" {41+}`)
	if m := mailboxMsgs(t, ms, user, "INBOX"); !strings.Contains(r, "OK") || len(m) != 1 || !m[0].InternalDate.Equal(want) {
		t.Fatalf("reply %q msgs %+v", r, m)
	}
	ms, user, r = do(`a1 APPEND INBOX "31-Foo-2021 10:00:00 +0000" {41+}`)
	if m := mailboxMsgs(t, ms, user, "INBOX"); !strings.Contains(r, "BAD") || len(m) != 0 {
		t.Fatalf("invalid date-time: reply %q msgs %+v", r, m)
	}
}

// F5217: FETCH responses caused by UID FETCH / UID STORE carry the UID
// (RFC 3501 §6.4.8).
func TestUIDCommands_ResponsesIncludeUID(t *testing.T) {
	_, _, run := persistentSession(t, 2)
	run("a1 SELECT INBOX")
	if out := strings.Join(run("a2 UID FETCH 2 (FLAGS)"), "\n"); !strings.Contains(out, "* 2 FETCH (UID 2 FLAGS") {
		t.Fatalf("UID FETCH: %q", out)
	}
	if out := strings.Join(run(`a3 UID STORE 2 +FLAGS (\Flagged)`), "\n"); !strings.Contains(out, `* 2 FETCH (UID 2 FLAGS (\Flagged))`) {
		t.Fatalf("UID STORE: %q", out)
	}
	if out := strings.Join(run("a4 FETCH 2 (FLAGS)"), "\n"); strings.Contains(out, "UID") {
		t.Fatalf("plain FETCH gained UID: %q", out)
	}
}

// F5218: SEARCH honours the UID <set> key and a bare sequence-set key
// (RFC 3501 §6.4.4).
func TestSearch_HonoursSequenceAndUIDSets(t *testing.T) {
	_, _, run := persistentSession(t, 4)
	run("a1 SELECT INBOX")
	run(`a2 STORE 2 +FLAGS (\Seen)`)
	for cmd, want := range map[string]string{
		"a3 UID SEARCH UID 3:*":        "* SEARCH 3 4",
		"a4 SEARCH 2,4":                "* SEARCH 2 4",
		"a5 UID SEARCH UID 2:3 UNSEEN": "* SEARCH 3",
		"a6 UID SEARCH UID 10:*":       "* SEARCH 4",
	} {
		if got := rfcUntagged(run(cmd), "* SEARCH"); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}

// F5219: LIST '%' matches within one hierarchy level, '*' across levels
// (RFC 3501 §6.3.8).
func TestList_PercentWildcard(t *testing.T) {
	ms, user, run := persistentSession(t, 0)
	if err := ms.CreateMailbox(user, "Archive/2024"); err != nil {
		t.Fatal(err)
	}
	for cmd, want := range map[string]string{
		`a1 LIST "" "%"`:         "Archive,INBOX",
		`a2 LIST "" "Archive/%"`: "Archive/2024",
		`a3 LIST "" "*"`:         "Archive,Archive/2024,INBOX",
	} {
		if got := rfcListNames(run(cmd)); got != want {
			t.Errorf("%s: got %q want %q", cmd, got, want)
		}
	}
}
