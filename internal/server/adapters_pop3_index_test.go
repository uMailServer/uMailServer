package server

// Regression tests for F4977: internal/pop3 addresses the Mailstore with 1-based message
// numbers (server.go loadSnapshotMessageData / handleUpdateCommand pass i+1),
// and pop3MailstoreAdapter adds 1 again, so RETR n reads message n+1 and
// DELE n deletes message n+1. Driven through a real pop3 session over the
// production adapter + BboltMailstore (as startPOP3 wires them; the TLS
// requirement is left off because the scripted conn is plaintext).

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/pop3"
	"golang.org/x/crypto/bcrypt"
)

type pop3IXConn struct {
	mu  sync.Mutex
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *pop3IXConn) Read(p []byte) (int, error) { return c.in.Read(p) }
func (c *pop3IXConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.Write(p)
}
func (c *pop3IXConn) Close() error        { return nil }
func (c *pop3IXConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 110} }
func (c *pop3IXConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40000}
}
func (c *pop3IXConn) SetDeadline(t time.Time) error      { return nil }
func (c *pop3IXConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *pop3IXConn) SetWriteDeadline(t time.Time) error { return nil }

type pop3IXEnv struct {
	srv  *Server
	pop  *pop3.Server
	user string
}

func pop3IXSetup(t *testing.T, subjects ...string) *pop3IXEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID: domain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: string(hash), IsActive: true}); err != nil {
		t.Fatalf("INVALID: account: %v", err)
	}
	for _, subj := range subjects {
		msg := "Subject: " + subj + "\r\nMessage-ID: <" + subj + "@x>\r\n\r\nbody of " + subj + "\r\n"
		if err := srv.deliverMessageWithNotify("bob@x.example", []string{"alice@test.com"}, nil, []byte(msg)); err != nil {
			t.Fatalf("INVALID: deliver %s: %v", subj, err)
		}
	}
	// Mirror Start + startPOP3.
	srv.mailstore = imap.NewBboltMailstoreWithInterfaces(srv.storageDB, srv.msgStore)
	p := pop3.NewServer("127.0.0.1:0", &pop3MailstoreAdapter{mailstore: srv.mailstore, msgStore: srv.msgStore}, srv.logger)
	p.SetAuthFunc(srv.authenticate)
	return &pop3IXEnv{srv: srv, pop: p, user: "alice@test.com"}
}

// pop3IXSession runs one POP3 session and returns the transcript.
func (e *pop3IXEnv) session(cmds ...string) string {
	script := "USER " + e.user + "\r\nPASS secret\r\n" + strings.Join(cmds, "\r\n") + "\r\n"
	conn := &pop3IXConn{in: bytes.NewReader([]byte(script))}
	pop3.NewSession(conn, e.pop).Handle()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.out.String()
}

func TestPOP3AdapterIndexControl(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	tr := e.session("STAT", "QUIT")
	if !strings.Contains(tr, "+OK 2 ") {
		t.Fatalf("INVALID CONTROL: STAT does not list 2 messages:\n%s", tr)
	}
}

func TestPOP3AdapterIndexRetr(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	tr := e.session("RETR 1", "QUIT")
	t.Logf("RETR 1: EXPECTED: body of one")
	t.Logf("RETR 1: ACTUAL: transcript=%q", tr)
	if !strings.Contains(tr, "body of one") || strings.Contains(tr, "body of two") {
		t.Fatalf("DEFECT F4977: RETR 1 did not return message 1")
	}
}

// --- verifier edge cases ---

// Last message (boundary) and a single-message maildrop.
func TestPOP3AdapterIndexRetrLastAndSingle(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	tr := e.session("RETR 2", "QUIT")
	if !strings.Contains(tr, "body of two") {
		t.Fatalf("DEFECT F4977: RETR 2 (last) did not return message 2:\n%s", tr)
	}
	s := pop3IXSetup(t, "solo")
	tr = s.session("RETR 1", "TOP 1 0", "QUIT")
	if !strings.Contains(tr, "body of solo") || strings.Contains(tr, "-ERR") {
		t.Fatalf("DEFECT F4977: single-message RETR/TOP failed:\n%s", tr)
	}
}

// DELE 1 + QUIT removes message 1 only; the next session sees message 2 as 1.
func TestPOP3AdapterIndexDeleRemovesRightMessage(t *testing.T) {
	e := pop3IXSetup(t, "one", "two")
	tr := e.session("DELE 1", "QUIT")
	if strings.Contains(tr, "-ERR") {
		t.Fatalf("DEFECT F4977: DELE/QUIT failed:\n%s", tr)
	}
	tr = e.session("STAT", "RETR 1", "QUIT")
	t.Logf("after DELE 1: EXPECTED: STAT 1 message, RETR 1 = two; ACTUAL: %q", tr)
	if !strings.Contains(tr, "+OK 1 ") || !strings.Contains(tr, "body of two") || strings.Contains(tr, "body of one") {
		t.Fatalf("DEFECT F4977: DELE 1 removed the wrong message or nothing")
	}
	// The kept message was not flagged deleted in IMAP.
	msgs, err := e.srv.mailstore.FetchMessages(e.user, "INBOX", "1:*", []string{"FLAGS"})
	if err != nil {
		t.Fatalf("INVALID: fetch: %v", err)
	}
	for _, m := range msgs {
		deleted := false
		for _, f := range m.Flags {
			deleted = deleted || f == "\\Deleted"
		}
		if m.UID == 2 && deleted {
			t.Fatalf("DEFECT F4977: message 2 flagged \\Deleted")
		}
	}
}

// DELE of the last message.
func TestPOP3AdapterIndexDeleLast(t *testing.T) {
	e := pop3IXSetup(t, "one", "two", "three")
	e.session("DELE 3", "QUIT")
	tr := e.session("STAT", "RETR 1", "RETR 2", "QUIT")
	if !strings.Contains(tr, "+OK 2 ") || !strings.Contains(tr, "body of one") || !strings.Contains(tr, "body of two") || strings.Contains(tr, "body of three") {
		t.Fatalf("DEFECT F4977: DELE 3 wrong result:\n%s", tr)
	}
}
