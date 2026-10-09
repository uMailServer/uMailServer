package server

// Regression tests for F5115: the actions of a mailbox owner's active Sieve
// script (fileinto, redirect, discard, reject, keep) must decide how
// inbound mail is delivered. Before the fix every message landed in INBOX.

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/smtp"
)

func sieveDTCount(t *testing.T, srv *Server, mailbox, folder string) int {
	t.Helper()
	n, _, _, err := srv.storageDB.GetMailboxCounts(mailbox, folder)
	if err != nil {
		return 0
	}
	return n
}

// sieveDTQueued returns the queued messages addressed to rcpt.
func sieveDTQueued(t *testing.T, srv *Server, rcpt string) []string {
	t.Helper()
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	var out []string
	for _, e := range entries {
		for _, to := range e.To {
			if strings.EqualFold(to, rcpt) {
				b, err := os.ReadFile(e.MessagePath)
				if err != nil {
					t.Fatalf("read queued: %v", err)
				}
				out = append(out, string(b))
			}
		}
	}
	return out
}

func sieveDTScript(t *testing.T, srv *Server, user, src string) {
	t.Helper()
	if err := srv.sieveManager.StoreScript(user, "main", src); err != nil {
		t.Fatalf("store script: %v", err)
	}
	if err := srv.sieveManager.SetActiveScriptByName(user, "main"); err != nil {
		t.Fatalf("activate: %v", err)
	}
}

// sieveDTSend runs one port-25 transaction from sender to rcpts.
func sieveDTSend(t *testing.T, srv *Server, sender string, rcpts []string, msg string) string {
	t.Helper()
	conn := &junkRTConn{in: bytes.NewReader([]byte(msg + "\r\n.\r\n"))}
	sess := smtp.NewSession(conn, srv.smtpServer)
	cmds := []string{"EHLO client.example", "MAIL FROM:<" + sender + ">"}
	for _, r := range rcpts {
		cmds = append(cmds, "RCPT TO:<"+r+">")
	}
	for _, cmd := range append(cmds, "DATA") {
		_ = sess.HandleCommand(cmd)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	tr := conn.out.String()
	if !strings.Contains(tr, "250 OK") {
		t.Fatalf("message not accepted:\n%s", tr)
	}
	return tr
}

func TestSieveDeliveryFileinto(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require \"fileinto\";\nfileinto \"Work\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if in, work := sieveDTCount(t, srv, "alice@test.com", "INBOX"), sieveDTCount(t, srv, "alice@test.com", "Work"); in != 0 || work != 1 {
		t.Fatalf("F5115: fileinto Work: INBOX=%d Work=%d, want 0/1", in, work)
	}
}

func TestSieveDeliveryRedirectCancelsKeep(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "redirect \"carol@remote.example\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	q := sieveDTQueued(t, srv, "carol@remote.example")
	if in := sieveDTCount(t, srv, "alice@test.com", "INBOX"); in != 0 || len(q) != 1 {
		t.Fatalf("F5115: redirect: INBOX=%d queued=%d, want 0/1", in, len(q))
	}
	if !strings.Contains(q[0], "X-Mail-Loop: alice@test.com") {
		t.Fatalf("F5115: redirected copy lacks loop marker:\n%s", q[0])
	}
}

func TestSieveDeliveryRedirectAndKeep(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "redirect \"carol@remote.example\";\nkeep;\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if in, q := sieveDTCount(t, srv, "alice@test.com", "INBOX"), len(sieveDTQueued(t, srv, "carol@remote.example")); in != 1 || q != 1 {
		t.Fatalf("F5115: redirect+keep: INBOX=%d queued=%d, want 1/1", in, q)
	}
}

// A message that already went through alice's redirect is kept, not
// redirected again.
func TestSieveDeliveryRedirectLoopKeeps(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "redirect \"carol@remote.example\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, "X-Mail-Loop: alice@test.com\r\n"+junkRTClean)
	if in, q := sieveDTCount(t, srv, "alice@test.com", "INBOX"), len(sieveDTQueued(t, srv, "carol@remote.example")); in != 1 || q != 0 {
		t.Fatalf("F5115: looped redirect: INBOX=%d queued=%d, want 1/0", in, q)
	}
}

func TestSieveDeliveryDiscard(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "discard;\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if in := sieveDTCount(t, srv, "alice@test.com", "INBOX"); in != 0 {
		t.Fatalf("F5115: discard: INBOX=%d, want 0", in)
	}
}

// Reject after DATA is reported to the sender with a failure DSN.
func TestSieveDeliveryRejectBouncesToSender(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "require \"reject\";\nreject \"Not here\";\n")
	sieveDTSend(t, srv, "sender@remote.example", []string{"alice@test.com"}, junkRTClean)
	if in := sieveDTCount(t, srv, "alice@test.com", "INBOX"); in != 0 {
		t.Fatalf("F5115: reject stored the message: INBOX=%d", in)
	}
	dsn := sieveDTQueued(t, srv, "sender@remote.example")
	if len(dsn) != 1 || !strings.Contains(dsn[0], "5.7.1 Not here") {
		t.Fatalf("F5115: reject: want one DSN with the reason, got %d:\n%s", len(dsn), strings.Join(dsn, "\n---\n"))
	}
}

// Each recipient's own script applies; a recipient without one gets INBOX.
func TestSieveDeliveryPerRecipient(t *testing.T) {
	srv := junkRTServer(t, true)
	if err := srv.database.CreateAccount(&db.AccountData{Email: "bob@test.com", LocalPart: "bob", Domain: "test.com", PasswordHash: "x", IsActive: true}); err != nil {
		t.Fatalf("account: %v", err)
	}
	sieveDTScript(t, srv, "alice@test.com", "require \"fileinto\";\nfileinto \"Work\";\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com", "bob@test.com"}, junkRTClean)
	if work, bobIn := sieveDTCount(t, srv, "alice@test.com", "Work"), sieveDTCount(t, srv, "bob@test.com", "INBOX"); work != 1 || bobIn != 1 {
		t.Fatalf("F5115: per recipient: alice Work=%d bob INBOX=%d, want 1/1", work, bobIn)
	}
	if bobWork := sieveDTCount(t, srv, "bob@test.com", "Work"); bobWork != 0 {
		t.Fatalf("F5115: alice's script applied to bob (Work=%d)", bobWork)
	}
}

// A script that does not file the message keeps it in the default folder,
// including Junk for a spam verdict.
func TestSieveDeliveryImplicitKeepUsesJunk(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTScript(t, srv, "alice@test.com", "if header :contains \"Subject\" \"nomatch\" { discard; }\n")
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTSpam)
	if in, junk := sieveDTCount(t, srv, "alice@test.com", "INBOX"), sieveDTCount(t, srv, "alice@test.com", "Junk"); in != 0 || junk != 1 {
		t.Fatalf("F5115: implicit keep of junk: INBOX=%d Junk=%d, want 0/1", in, junk)
	}
}

// Without a script, delivery is unchanged.
func TestSieveDeliveryNoScript(t *testing.T) {
	srv := junkRTServer(t, true)
	sieveDTSend(t, srv, "", []string{"alice@test.com"}, junkRTClean)
	if in := sieveDTCount(t, srv, "alice@test.com", "INBOX"); in != 1 {
		t.Fatalf("no script: INBOX=%d, want 1", in)
	}
}
