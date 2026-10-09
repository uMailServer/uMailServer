package server

// Regression tests for inbound delivery defects in server_handlers.go
// (audit findings F4875–F4879):
//
//   - F4875: a multi-recipient delivery with one failed recipient returned an
//     error, which SMTP turns into 451 for the whole transaction; the client's
//     retry duplicated the mail to every recipient already served.
//   - F4876: the domain catch-all was only consulted for inactive accounts, so
//     mail to a mailbox that does not exist never reached it.
//   - F4877: a catch-all target that was itself inactive recursed without
//     bound and crashed the process with a stack overflow.
//   - F4878: on a forwarding loop the message was dropped (even with
//     ForwardKeepCopy) while the quota reserved for it was never released.
//   - F4879: with ForwardKeepCopy off, a failed forward enqueue still dropped
//     the local copy and reported success, losing the message.
//   - F4880: the inbound MX server (port 25) had no relay policy, so an
//     unauthenticated client could queue mail to any external domain (open
//     relay). server_smtp.go now runs relayPolicyStage first.

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/storage"
)

const deliveryAuditMsg = "From: s@ext.com\r\nTo: bob@test.com\r\nSubject: hi\r\nMessage-ID: <x@ext.com>\r\n\r\nbody body body\r\n"

func newDeliveryAuditServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st, err := storage.OpenDatabase(filepath.Join(dir, "storage.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ms, err := storage.NewMessageStore(filepath.Join(dir, "messages"))
	if err != nil {
		t.Fatalf("open message store: %v", err)
	}
	t.Cleanup(func() { ms.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := d.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 20, IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	for _, a := range []*db.AccountData{
		{LocalPart: "alice", IsActive: true, QuotaLimit: 10}, // always over quota
		{LocalPart: "bob", IsActive: true},
		{LocalPart: "carol", IsActive: true},
		{LocalPart: "old", IsActive: false},
		{LocalPart: "catch", IsActive: false},
	} {
		a.Domain, a.Email, a.PasswordHash = "test.com", a.LocalPart+"@test.com", "x"
		if err := d.CreateAccount(a); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	return &Server{
		database: d, storageDB: st, msgStore: ms, logger: log,
		queue: queue.NewManager(d, nil, filepath.Join(dir, "q"), log),
		bgSem: make(chan struct{}, 10),
	}
}

func breakDeliveryAuditQueue(t *testing.T, s *Server) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.queue = queue.NewManager(s.database, nil, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := s.queue.Enqueue("a@b.c", []string{"d@e.f"}, []byte("x")); err == nil {
		t.Fatal("fault injection did not make Enqueue fail")
	}
}

func quotaUsed(t *testing.T, s *Server, user string) int64 {
	t.Helper()
	a, err := s.database.GetAccount("test.com", user)
	if err != nil {
		t.Fatalf("get %s: %v", user, err)
	}
	return a.QuotaUsed
}

func storedInbox(s *Server, email string) bool {
	m, err := s.storageDB.GetMessageMetadata(email, "INBOX", 1)
	return err == nil && m != nil && m.MessageID != ""
}

func bouncesTo(t *testing.T, s *Server, sender string) []string {
	t.Helper()
	entries, err := s.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.From == "" && len(e.To) == 1 && e.To[0] == sender {
			b, err := os.ReadFile(e.MessagePath)
			if err != nil {
				t.Fatalf("read DSN: %v", err)
			}
			out = append(out, string(b))
		}
	}
	return out
}

func setCatchAll(t *testing.T, s *Server, target string) {
	t.Helper()
	d, err := s.database.GetDomain("test.com")
	if err != nil {
		t.Fatal(err)
	}
	d.CatchAllTarget = target
	if err := s.database.UpdateDomain(d); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryPartialFailureAcceptsAndBounces(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"bob@test.com", "alice@test.com"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("partial delivery must be accepted (a 451 makes the client duplicate bob's copy): %v", err)
	}
	if got := quotaUsed(t, s, "bob"); got != int64(len(deliveryAuditMsg)) {
		t.Fatalf("bob QuotaUsed = %d, want one copy (%d)", got, len(deliveryAuditMsg))
	}
	dsns := bouncesTo(t, s, "s@ext.com")
	if len(dsns) != 1 || !strings.Contains(dsns[0], "alice@test.com") || !strings.Contains(dsns[0], "5.2.2") {
		t.Fatalf("want one quota failure DSN for alice, got %d: %q", len(dsns), dsns)
	}
}

func TestDeliveryAllFailedReturnsError(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"alice@test.com", "nobody@test.com"}, nil, []byte(deliveryAuditMsg)); err == nil {
		t.Fatal("all recipients failed: want error so the client retries")
	}
	if n := len(bouncesTo(t, s, "s@ext.com")); n != 0 {
		t.Fatalf("all-failed delivery must not also bounce (got %d DSNs)", n)
	}
}

func TestDeliveryPartialFailureHonoursNotifyNeverAndNullSender(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"bob@test.com", "alice@test.com"}, []string{"", "NEVER"}, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("NOTIFY=NEVER partial delivery: %v", err)
	}
	if n := len(bouncesTo(t, s, "s@ext.com")); n != 0 {
		t.Fatalf("NOTIFY=NEVER recipient was bounced (%d DSNs)", n)
	}
	if err := s.deliverMessageWithNotify("", []string{"carol@test.com", "alice@test.com"}, nil, []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("null-sender partial delivery: %v", err)
	}
	if entries, _ := s.queue.GetPendingEntries(); len(entries) != 0 {
		t.Fatalf("null-sender message was bounced (%d queue entries)", len(entries))
	}
}

func TestDeliveryPartialFailureUnreportableReturnsError(t *testing.T) {
	s := newDeliveryAuditServer(t)
	breakDeliveryAuditQueue(t, s)
	if err := s.deliverMessageWithNotify("s@ext.com", []string{"bob@test.com", "alice@test.com"}, nil, []byte(deliveryAuditMsg)); err == nil {
		t.Fatal("failed recipient could not be reported: want error rather than silent loss")
	}
}

func TestDeliveryCatchAllCoversUnknownMailbox(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.deliverLocal("nobody", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err == nil {
		t.Fatal("unknown mailbox without catch-all must fail")
	}
	setCatchAll(t, s, "carol@test.com")
	if err := s.deliverLocal("nobody", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("unknown mailbox with catch-all: %v", err)
	}
	if err := s.deliverLocal("bob", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatalf("existing mailbox: %v", err)
	}
	if got := quotaUsed(t, s, "carol"); got != int64(len(deliveryAuditMsg)) {
		t.Fatalf("carol QuotaUsed = %d, want exactly the catch-all copy (%d)", got, len(deliveryAuditMsg))
	}
}

func TestDeliveryCatchAllTargetUnusableFailsWithoutRecursion(t *testing.T) {
	s := newDeliveryAuditServer(t)
	// A recursion would die quickly against this limit instead of 1 GB.
	defer debug.SetMaxStack(debug.SetMaxStack(16 << 20))
	for _, target := range []string{"catch@test.com", "missing@test.com", "old@test.com"} {
		setCatchAll(t, s, target)
		for _, user := range []string{"old", "catch", "nobody"} {
			if err := s.deliverLocal(user, "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err == nil {
				t.Fatalf("catch-all %s: delivery to %s reported success", target, user)
			}
		}
	}
}

func createForwarder(t *testing.T, s *Server, forwardTo string, keep bool) {
	t.Helper()
	if err := s.database.CreateAccount(&db.AccountData{Email: "fw@test.com", LocalPart: "fw", Domain: "test.com", PasswordHash: "x", IsActive: true, ForwardTo: forwardTo, ForwardKeepCopy: keep}); err != nil {
		t.Fatal(err)
	}
}

const deliveryAuditLoopMsg = "From: s@ext.com\r\nX-Mail-Loop: fw@test.com\r\nSubject: hi\r\n\r\nbody body\r\n"

func TestDeliveryForwardLoopKeepsMessageAndQuotaConsistent(t *testing.T) {
	for _, keep := range []bool{true, false} {
		s := newDeliveryAuditServer(t)
		createForwarder(t, s, "other@ext.com", keep)
		if err := s.deliverLocal("fw", "test.com", "s@ext.com", []byte(deliveryAuditLoopMsg)); err != nil {
			t.Fatalf("keep=%v: %v", keep, err)
		}
		if !storedInbox(s, "fw@test.com") {
			t.Fatalf("keep=%v: looped message dropped", keep)
		}
		if got := quotaUsed(t, s, "fw"); got != int64(len(deliveryAuditLoopMsg)) {
			t.Fatalf("keep=%v: QuotaUsed = %d, want %d", keep, got, len(deliveryAuditLoopMsg))
		}
		if entries, _ := s.queue.GetPendingEntries(); len(entries) != 0 {
			t.Fatalf("keep=%v: looped message forwarded again", keep)
		}
	}
}

func TestDeliveryForwardFailureKeepsLocalCopy(t *testing.T) {
	cases := map[string]func(t *testing.T, s *Server){
		"enqueue fails": breakDeliveryAuditQueue,
		"no queue":      func(t *testing.T, s *Server) { s.queue = nil },
	}
	for name, setup := range cases {
		s := newDeliveryAuditServer(t)
		createForwarder(t, s, "other@ext.com", false)
		setup(t, s)
		if err := s.deliverLocal("fw", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !storedInbox(s, "fw@test.com") || quotaUsed(t, s, "fw") != int64(len(deliveryAuditMsg)) {
			t.Fatalf("%s: forward not queued and no local copy kept (mail lost)", name)
		}
	}

	// Only empty targets: nothing was forwarded, so the copy must stay.
	s := newDeliveryAuditServer(t)
	createForwarder(t, s, " , ", false)
	if err := s.deliverLocal("fw", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil || !storedInbox(s, "fw@test.com") {
		t.Fatalf("empty forward list: err=%v stored=%v", err, storedInbox(s, "fw@test.com"))
	}

	// Control: a successful forward without keep-copy stores nothing locally.
	s = newDeliveryAuditServer(t)
	createForwarder(t, s, "other@ext.com", false)
	if err := s.deliverLocal("fw", "test.com", "s@ext.com", []byte(deliveryAuditMsg)); err != nil {
		t.Fatal(err)
	}
	if storedInbox(s, "fw@test.com") || quotaUsed(t, s, "fw") != 0 {
		t.Fatal("successful forward without keep-copy kept a local copy or quota")
	}
}

// relayAuditConn is an in-memory net.Conn: reads come from in, writes are kept.
type relayAuditConn struct {
	mu  sync.Mutex
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *relayAuditConn) Read(p []byte) (int, error) { return c.in.Read(p) }
func (c *relayAuditConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.Write(p)
}
func (c *relayAuditConn) Close() error { return nil }
func (c *relayAuditConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 25}
}
func (c *relayAuditConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 40000}
}
func (c *relayAuditConn) SetDeadline(t time.Time) error      { return nil }
func (c *relayAuditConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *relayAuditConn) SetWriteDeadline(t time.Time) error { return nil }

// newRelayAuditInbound builds the real port-25 server through startInboundSMTP.
func newRelayAuditInbound(t *testing.T) *Server {
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
	cfg.Spam.Greylisting.Enabled = false
	cfg.Spam.RBLServers = nil
	cfg.AV.Enabled = false
	cfg.DMARC.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if srv.queue == nil {
		// Mirror server_start.go: the queue dir exists before the manager is used.
		qdir := filepath.Join(dir, "queue")
		if err := os.MkdirAll(qdir, 0o750); err != nil {
			t.Fatalf("%v", err)
		}
		srv.queue = queue.NewManager(srv.database, nil, qdir, srv.logger)
	}
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("domain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: "x", IsActive: true}); err != nil {
		t.Fatalf("account: %v", err)
	}
	srv.startInboundSMTP()
	if srv.smtpServer == nil {
		t.Fatalf("inbound SMTP server not wired")
	}
	return srv
}

// relayAuditSession runs an unauthenticated port-25 transaction (null sender, no
// From header, so no pipeline stage needs DNS) and returns the transcript.
func relayAuditSession(t *testing.T, srv *Server, rcpt string) string {
	t.Helper()
	conn := &relayAuditConn{in: bytes.NewReader([]byte("Subject: relay test\r\n\r\nhello\r\n.\r\n"))}
	sess := smtp.NewSession(conn, srv.smtpServer)
	for _, cmd := range []string{"EHLO client.example", "MAIL FROM:<>", "RCPT TO:<" + rcpt + ">", "DATA"} {
		_ = sess.HandleCommand(cmd)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.out.String()
}

func relayAuditQueued(t *testing.T, srv *Server, rcpt string) bool {
	entries, err := srv.queue.GetPendingEntries()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	for _, e := range entries {
		for _, to := range e.To {
			if strings.EqualFold(to, rcpt) {
				return true
			}
		}
	}
	return false
}

func TestInboundSMTPRefusesUnauthenticatedRelay(t *testing.T) {
	srv := newRelayAuditInbound(t)
	tr := relayAuditSession(t, srv, "victim@external.example")
	if relayAuditQueued(t, srv, "victim@external.example") {
		t.Fatalf("open relay: unauthenticated port-25 mail to an external domain was queued; transcript:\n%s", tr)
	}
	// Local recipients are still accepted and delivered over port 25.
	relayAuditSession(t, srv, "alice@test.com")
	if a, err := srv.database.GetAccount("test.com", "alice"); err != nil || a.QuotaUsed == 0 {
		t.Fatalf("local delivery over port 25 failed: %v", err)
	}
}

func TestRelayPolicyStage(t *testing.T) {
	local := func(d string) bool { return d == "test.com" }
	st := &relayPolicyStage{isLocalDomain: local}
	cases := []struct {
		name string
		auth bool
		to   []string
		want smtp.PipelineResult
	}{
		{"unauth local", false, []string{"a@test.com"}, smtp.ResultAccept},
		{"unauth external", false, []string{"v@ext.example"}, smtp.ResultReject},
		{"unauth mixed", false, []string{"a@test.com", "v@ext.example"}, smtp.ResultReject},
		{"unauth no domain", false, []string{"postmaster"}, smtp.ResultReject},
		{"auth external", true, []string{"v@ext.example"}, smtp.ResultAccept},
	}
	for _, c := range cases {
		ctx := smtp.NewMessageContext(net.IPv4(192, 0, 2, 7), "s@ext.example", c.to, nil)
		ctx.Authenticated = c.auth
		if got := st.Process(ctx); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsLocalDomainRequiresActiveDomain(t *testing.T) {
	s := newDeliveryAuditServer(t)
	if err := s.database.CreateDomain(&db.DomainData{Name: "off.com", IsActive: false}); err != nil {
		t.Fatal(err)
	}
	if !s.isLocalDomain("test.com") || !s.isLocalDomain("TEST.com") {
		t.Fatal("active local domain not recognised")
	}
	if s.isLocalDomain("off.com") || s.isLocalDomain("ext.example") {
		t.Fatal("inactive or unknown domain treated as local")
	}
}
