package imap

import (
	"net"
	"strings"
	"testing"
	"time"
)

// Regression coverage for IMAP RENAME/DELETE guards on the reserved INBOX
// mailbox.
//
// INBOX is the mandatory, reserved mailbox: it must not be deleted or renamed
// away, or the user loses their INBOX and its messages. The DELETE handler
// already refused `DELETE INBOX`; RENAME had no such guard, so `RENAME INBOX X`
// silently destroyed INBOX. These tests pin the guard on both commands.

func newRenameGuardSession(t *testing.T) (net.Conn, *Session, *BboltMailstore, string) {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	t.Cleanup(func() { ms.Close() })

	user := "testuser"
	for _, box := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(user, box); err != nil {
			t.Fatalf("CreateMailbox %s: %v", box, err)
		}
	}
	if err := ms.AppendMessage(user, "INBOX", nil, time.Now(),
		[]byte("Subject: keep me\r\n\r\nbody")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	client, srvConn := net.Pipe()
	sess := NewSession(srvConn, NewServer(&Config{Addr: ":0"}, ms))
	sess.user = user
	sess.tag = "t1"
	sess.state = StateAuthenticated
	return client, sess, ms, user
}

func runAndRead(t *testing.T, client net.Conn, fn func() error) string {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 512)
	var out strings.Builder
	for {
		n, err := client.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if strings.Contains(out.String(), "\n") {
				break
			}
		}
		if err != nil {
			break
		}
	}
	<-done
	return strings.TrimRight(out.String(), "\r\n")
}

func mailboxExists(t *testing.T, ms *BboltMailstore, user, name string) bool {
	t.Helper()
	boxes, err := ms.ListMailboxes(user, "*")
	if err != nil {
		t.Fatalf("ListMailboxes: %v", err)
	}
	for _, b := range boxes {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	return false
}

// RENAME must refuse to rename INBOX (in any letter case) and must leave INBOX
// and its messages intact.
func TestRename_RejectsINBOX(t *testing.T) {
	for _, old := range []string{"INBOX", "inbox", "Inbox"} {
		t.Run("old="+old, func(t *testing.T) {
			client, sess, ms, user := newRenameGuardSession(t)
			defer client.Close()

			resp := runAndRead(t, client, func() error {
				return sess.handleRename([]string{old, "Renamed"})
			})

			if !strings.Contains(resp, "NO") {
				t.Errorf("RENAME %s Renamed should be refused with NO, got %q", old, resp)
			}
			if !mailboxExists(t, ms, user, "INBOX") {
				t.Errorf("INBOX must still exist after RENAME %s Renamed", old)
			}
			if mailboxExists(t, ms, user, "Renamed") {
				t.Errorf("the illegal RENAME %s Renamed was applied", old)
			}
			// The message in INBOX must survive. An empty SearchCriteria matches
			// every message, so its result count is the INBOX size.
			if uids, _ := ms.SearchMessages(user, "INBOX", SearchCriteria{}); len(uids) == 0 {
				t.Errorf("the message in INBOX was lost by the illegal RENAME")
			}
		})
	}
}

// Control: renaming an ordinary mailbox must keep working.
func TestRename_NonINBOX_Succeeds(t *testing.T) {
	client, sess, ms, user := newRenameGuardSession(t)
	defer client.Close()

	resp := runAndRead(t, client, func() error {
		return sess.handleRename([]string{"Archive", "Work"})
	})

	if !strings.Contains(resp, "OK") {
		t.Errorf("RENAME Archive Work should succeed with OK, got %q", resp)
	}
	if !mailboxExists(t, ms, user, "Work") {
		t.Errorf("'Work' should exist after RENAME Archive Work")
	}
	if mailboxExists(t, ms, user, "Archive") {
		t.Errorf("'Archive' should no longer exist after RENAME Archive Work")
	}
}

// The pre-existing DELETE guard must keep working (guards against regressing the
// reserved-mailbox protection while adding the RENAME guard).
func TestDelete_RejectsINBOX(t *testing.T) {
	client, sess, ms, user := newRenameGuardSession(t)
	defer client.Close()

	resp := runAndRead(t, client, func() error {
		return sess.handleDelete([]string{"INBOX"})
	})

	if !strings.Contains(resp, "NO") {
		t.Errorf("DELETE INBOX should be refused with NO, got %q", resp)
	}
	if !mailboxExists(t, ms, user, "INBOX") {
		t.Errorf("INBOX must still exist after DELETE INBOX")
	}
}
