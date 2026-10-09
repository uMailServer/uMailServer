package server

// Regression tests for F4975: a ScoreStage "junk" verdict on the real inbound (port 25)
// wiring must file the message into the recipient's Junk folder, and a
// sender-supplied X-Spam-* header must not steer mail into or out of Junk.

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/smtp"
)

type junkRTConn struct {
	mu  sync.Mutex
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *junkRTConn) Read(p []byte) (int, error) { return c.in.Read(p) }
func (c *junkRTConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.Write(p)
}
func (c *junkRTConn) Close() error        { return nil }
func (c *junkRTConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 25} }
func (c *junkRTConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 9), Port: 40000}
}
func (c *junkRTConn) SetDeadline(t time.Time) error      { return nil }
func (c *junkRTConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *junkRTConn) SetWriteDeadline(t time.Time) error { return nil }

func junkRTServer(t *testing.T, inbound bool) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	cfg.SMTP.Inbound.Enabled = true
	cfg.SMTP.Inbound.Bind = "127.0.0.1"
	cfg.SMTP.Inbound.Port = 0
	cfg.SMTP.Submission.Bind = "127.0.0.1"
	cfg.SMTP.Submission.Port = 0
	cfg.Spam.Greylisting.Enabled = false
	cfg.Spam.RBLServers = nil
	cfg.Spam.JunkThreshold = 3.0
	cfg.Spam.RejectThreshold = 50
	cfg.AV.Enabled = false
	cfg.DMARC.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if srv.queue == nil {
		qdir := filepath.Join(dir, "queue")
		if err := os.MkdirAll(qdir, 0o750); err != nil {
			t.Fatalf("INVALID: %v", err)
		}
		srv.queue = queue.NewManager(srv.database, nil, qdir, srv.logger)
	}
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID: domain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: "x", IsActive: true}); err != nil {
		t.Fatalf("INVALID: account: %v", err)
	}
	if inbound {
		srv.startInboundSMTP()
		if srv.smtpServer == nil {
			t.Fatalf("INVALID: inbound SMTP server not wired")
		}
	} else {
		srv.startSubmissionSMTP()
		if srv.submissionServer == nil {
			t.Fatalf("INVALID: submission server not wired")
		}
	}
	return srv
}

// junkRTSend runs one unauthenticated port-25 transaction (null sender, no
// From header: no pipeline stage needs DNS) and returns the transcript.
func junkRTSend(t *testing.T, srv *Server, msg string) string {
	t.Helper()
	conn := &junkRTConn{in: bytes.NewReader([]byte(msg + "\r\n.\r\n"))}
	sess := smtp.NewSession(conn, srv.smtpServer)
	for _, cmd := range []string{"EHLO client.example", "MAIL FROM:<>", "RCPT TO:<alice@test.com>", "DATA"} {
		_ = sess.HandleCommand(cmd)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.out.String()
}

func junkRTCounts(t *testing.T, srv *Server) (inbox, junk int) {
	t.Helper()
	inbox, _, _, err := srv.storageDB.GetMailboxCounts("alice@test.com", "INBOX")
	if err != nil {
		t.Fatalf("INVALID: counts INBOX: %v", err)
	}
	junk, _, _, err = srv.storageDB.GetMailboxCounts("alice@test.com", "Junk")
	if err != nil {
		t.Fatalf("INVALID: counts Junk: %v", err)
	}
	return inbox, junk
}

// junkRTStored returns the single stored message for alice in folder.
func junkRTStored(t *testing.T, srv *Server, folder string) string {
	t.Helper()
	uids, err := srv.storageDB.GetMessageUIDs("alice@test.com", folder)
	if err != nil || len(uids) != 1 {
		t.Fatalf("INVALID: uids in %s: %v %v", folder, uids, err)
	}
	meta, err := srv.storageDB.GetMessageMetadata("alice@test.com", folder, uids[0])
	if err != nil {
		t.Fatalf("INVALID: meta: %v", err)
	}
	data, err := srv.msgStore.ReadMessage("alice@test.com", meta.MessageID)
	if err != nil {
		t.Fatalf("INVALID: read: %v", err)
	}
	return string(data)
}

const (
	// Score 0: Subject, Date and Message-ID present.
	junkRTClean = "Subject: hello\r\nDate: Mon, 1 Jan 2024 00:00:00 +0000\r\nMessage-ID: <c@x.example>\r\n\r\nhi"
	// Score 4 (ALL_CAPS_SUBJECT 2 + MISSING_DATE 1 + MISSING_MESSAGE_ID 1) >= junk threshold 3.
	junkRTSpam = "Subject: FREE MONEY NOW\r\n\r\nbuy"
)

func junkRTExpect(t *testing.T, srv *Server, tr, label string, wantInbox, wantJunk int) {
	t.Helper()
	inbox, junk := junkRTCounts(t, srv)
	t.Logf("%s: EXPECTED: INBOX=%d Junk=%d", label, wantInbox, wantJunk)
	t.Logf("%s: ACTUAL:   INBOX=%d Junk=%d", label, inbox, junk)
	if !strings.Contains(tr, "250 OK") {
		t.Fatalf("INVALID (%s): message not accepted; transcript:\n%s", label, tr)
	}
	if inbox != wantInbox || junk != wantJunk {
		t.Fatalf("DEFECT F4975 (%s): wrong folder", label)
	}
}

func TestJunkRoutingControl(t *testing.T) {
	srv := junkRTServer(t, true)
	tr := junkRTSend(t, srv, junkRTClean)
	junkRTExpect(t, srv, tr, "clean message", 1, 0)
}

func TestJunkRoutingJunkVerdict(t *testing.T) {
	srv := junkRTServer(t, true)
	tr := junkRTSend(t, srv, junkRTSpam)
	junkRTExpect(t, srv, tr, "junk verdict", 0, 1)
}

// --- verifier edge cases ---

// A sender cannot push clean mail into Junk with its own header.
func TestJunkRoutingForgedYesStaysInbox(t *testing.T) {
	srv := junkRTServer(t, true)
	tr := junkRTSend(t, srv, "X-Spam-Status: Yes, score=99.0\r\n"+junkRTClean)
	junkRTExpect(t, srv, tr, "forged Yes on clean", 1, 0)
}

// Case/whitespace variants of the forged header are not trusted either.
func TestJunkRoutingForgedVariantsStayInbox(t *testing.T) {
	srv := junkRTServer(t, true)
	tr := junkRTSend(t, srv, "x-spam-status : YES\r\nX-SPAM-FLAG: YES\r\n"+junkRTClean)
	junkRTExpect(t, srv, tr, "forged variants on clean", 1, 0)
	stored := junkRTStored(t, srv, "INBOX")
	hdr := strings.ToLower(stored[:strings.Index(stored, "\r\n\r\n")])
	if strings.Contains(hdr, "\nx-spam-status") || strings.HasPrefix(hdr, "x-spam-status") || strings.Contains(hdr, "x-spam-flag") {
		t.Fatalf("DEFECT F4975: sender-supplied X-Spam-* header kept verbatim:\n%s", stored)
	}
}

// A sender cannot pull junk out of Junk with its own "No" header, and the
// stored message carries exactly one (the server's) X-Spam-Status.
func TestJunkRoutingForgedNoStaysJunk(t *testing.T) {
	srv := junkRTServer(t, true)
	tr := junkRTSend(t, srv, "X-Spam-Status: No, score=0.0\r\n"+junkRTSpam)
	junkRTExpect(t, srv, tr, "forged No on junk", 0, 1)
	stored := junkRTStored(t, srv, "Junk")
	if n := strings.Count(strings.ToLower(stored), "x-spam-status:"); n != 1 {
		t.Fatalf("DEFECT F4975: want exactly one X-Spam-Status, got %d:\n%s", n, stored)
	}
}

// Repeated deliveries: junk then clean then junk land in the right folders.
func TestJunkRoutingRepeated(t *testing.T) {
	srv := junkRTServer(t, true)
	junkRTSend(t, srv, junkRTSpam)
	junkRTSend(t, srv, junkRTClean)
	tr := junkRTSend(t, srv, junkRTSpam)
	junkRTExpect(t, srv, tr, "junk,clean,junk", 1, 2)
}

// Submission (587, no pipeline) never files into Junk, even with the header.
func TestJunkRoutingSubmissionIgnoresHeader(t *testing.T) {
	srv := junkRTServer(t, false)
	if err := srv.deliverMessageWithNotify("bob@test.com", []string{"alice@test.com"}, nil,
		[]byte("X-Spam-Status: Yes, score=9.0\r\n"+junkRTClean+"\r\n")); err != nil {
		t.Fatalf("INVALID: deliver: %v", err)
	}
	inbox, junk := junkRTCounts(t, srv)
	t.Logf("submission: EXPECTED: INBOX=1 Junk=0; ACTUAL: INBOX=%d Junk=%d", inbox, junk)
	if inbox != 1 || junk != 0 {
		t.Fatalf("DEFECT F4975: submission handler trusted X-Spam-Status")
	}
}
