package server

// Regression tests for F4976: Stop() closes indexWork (and later the databases) without
// fencing SMTP deliveries that are still in flight. smtp.Server.Stop does not
// wait for sessions, so a session whose pipeline finishes during shutdown
// calls the delivery handler on a stopping Server.
//
// Gating (no sleeps): a fake index worker registered in s.wg drains
// s.indexWork like runIndexWorker does; when Stop closes the channel the
// worker's range loop ends, and only then is the blocked pipeline stage
// released. Stop is held in s.wg.Wait() (before the databases close) until
// the session has finished, which models the normal drain window.

import (
	"bytes"
	"fmt"
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

type stopIFConn struct {
	mu  sync.Mutex
	in  *bytes.Reader
	out bytes.Buffer
}

func (c *stopIFConn) Read(p []byte) (int, error) { return c.in.Read(p) }
func (c *stopIFConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.out.Write(p)
}
func (c *stopIFConn) Close() error        { return nil }
func (c *stopIFConn) LocalAddr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 25} }
func (c *stopIFConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 40000}
}
func (c *stopIFConn) SetDeadline(t time.Time) error      { return nil }
func (c *stopIFConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *stopIFConn) SetWriteDeadline(t time.Time) error { return nil }

type stopIFGate struct {
	entered chan struct{}
	release chan struct{}
}

func (g *stopIFGate) Name() string { return "Gate" }
func (g *stopIFGate) Process(ctx *smtp.MessageContext) smtp.PipelineResult {
	close(g.entered)
	<-g.release
	return smtp.ResultAccept
}

func stopIFServer(t *testing.T) *Server {
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
	cfg.AV.Enabled = false
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	qdir := filepath.Join(dir, "queue")
	if err := os.MkdirAll(qdir, 0o750); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	srv.queue = queue.NewManager(srv.database, nil, qdir, srv.logger)
	if err := srv.database.CreateDomain(&db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("INVALID: domain: %v", err)
	}
	if err := srv.database.CreateAccount(&db.AccountData{Email: "alice@test.com", LocalPart: "alice", Domain: "test.com", PasswordHash: "x", IsActive: true}); err != nil {
		t.Fatalf("INVALID: account: %v", err)
	}
	if srv.searchSvc == nil || srv.storageDB == nil {
		t.Fatalf("INVALID: search/storage not initialised")
	}
	srv.startInboundSMTP()
	return srv
}

type stopIFResult struct {
	transcript string
	panicked   interface{}
}

// stopIFSession runs one port-25 transaction with a gated pipeline. A panic
// is recovered as smtp.Server.handleConnection would (connection dropped).
func stopIFSession(srv *Server, gate *stopIFGate, done chan<- stopIFResult) {
	p := smtp.NewPipeline(nil)
	p.AddStage(gate)
	srv.smtpServer.SetPipeline(p)
	conn := &stopIFConn{in: bytes.NewReader([]byte("Subject: hi\r\n\r\nbody\r\n.\r\n"))}
	sess := smtp.NewSession(conn, srv.smtpServer)
	var res stopIFResult
	defer func() {
		res.panicked = recover()
		conn.mu.Lock()
		res.transcript = conn.out.String()
		conn.mu.Unlock()
		done <- res
	}()
	for _, cmd := range []string{"EHLO client.example", "MAIL FROM:<>", "RCPT TO:<alice@test.com>", "DATA"} {
		_ = sess.HandleCommand(cmd)
	}
}

func stopIFLastReply(tr string) string {
	lines := strings.Split(strings.TrimSpace(tr), "\r\n")
	return lines[len(lines)-1]
}

// stopIFCheck: the client must get either a transient refusal (nothing
// stored) or 250 with the message fully delivered; never a panic.
func stopIFCheck(t *testing.T, srv *Server, res stopIFResult, label string) {
	t.Helper()
	last := stopIFLastReply(res.transcript)
	t.Logf("%s: EXPECTED: no panic; reply 4xx (refused during shutdown) or 250 (delivered)", label)
	t.Logf("%s: ACTUAL:   panic=%v last reply=%q", label, res.panicked, last)
	if res.panicked != nil {
		t.Fatalf("DEFECT F4976 (%s): delivery panicked during Stop: %v", label, res.panicked)
	}
	if !strings.HasPrefix(last, "4") && !strings.HasPrefix(last, "250") {
		t.Fatalf("DEFECT F4976 (%s): unexpected reply %q", label, last)
	}
}

// Control: no Stop — the gated session delivers normally.
func TestStopInflightControl(t *testing.T) {
	srv := stopIFServer(t)
	defer srv.Stop()
	gate := &stopIFGate{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan stopIFResult, 1)
	go stopIFSession(srv, gate, done)
	<-gate.entered
	close(gate.release)
	res := <-done
	if res.panicked != nil || stopIFLastReply(res.transcript) != "250 OK" {
		t.Fatalf("INVALID CONTROL: panic=%v transcript:\n%s", res.panicked, res.transcript)
	}
}

// The pipeline finishes after Stop has closed indexWork but before the
// databases close.
func TestStopInflightStopDuringPipeline(t *testing.T) {
	srv := stopIFServer(t)
	gate := &stopIFGate{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan stopIFResult, 1)
	go stopIFSession(srv, gate, done)
	<-gate.entered

	var res stopIFResult
	srv.wg.Add(1)
	go func() { // fake index worker
		defer srv.wg.Done()
		for range srv.indexWork {
		}
		close(gate.release) // indexWork is closed: Stop is past the close
		res = <-done
	}()
	if err := srv.Stop(); err != nil {
		t.Fatalf("INVALID: Stop: %v", err)
	}
	stopIFCheck(t, srv, res, "stop during pipeline")
}

// --- verifier edge cases ---

// A delivery that arrives after Stop returned is refused cleanly.
func TestStopInflightDeliveryAfterStop(t *testing.T) {
	srv := stopIFServer(t)
	if err := srv.Stop(); err != nil {
		t.Fatalf("INVALID: Stop: %v", err)
	}
	var err error
	var p interface{}
	func() {
		defer func() { p = recover() }()
		err = srv.deliverMessageWithNotify("", []string{"alice@test.com"}, nil, []byte("Subject: x\r\n\r\nb\r\n"))
	}()
	t.Logf("after stop: EXPECTED: error, no panic; ACTUAL: err=%v panic=%v", err, p)
	if p != nil || err == nil {
		t.Fatalf("DEFECT F4976: late delivery err=%v panic=%v", err, p)
	}
}

// Stop twice while a session is gated, then release: still no panic.
func TestStopInflightDoubleStop(t *testing.T) {
	srv := stopIFServer(t)
	gate := &stopIFGate{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan stopIFResult, 1)
	go stopIFSession(srv, gate, done)
	<-gate.entered
	var res stopIFResult
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for range srv.indexWork {
		}
		close(gate.release)
		res = <-done
	}()
	_ = srv.Stop()
	_ = srv.Stop()
	stopIFCheck(t, srv, res, "double stop")
}

// Several sessions released during the drain window: none panics.
func TestStopInflightManySessions(t *testing.T) {
	srv := stopIFServer(t)
	const n = 4
	gates := make([]*stopIFGate, n)
	dones := make([]chan stopIFResult, n)
	for i := range gates {
		gates[i] = &stopIFGate{entered: make(chan struct{}), release: make(chan struct{})}
		dones[i] = make(chan stopIFResult, 1)
		// each session builds its own pipeline before running
		go stopIFSession(srv, gates[i], dones[i])
		<-gates[i].entered
	}
	results := make([]stopIFResult, n)
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for range srv.indexWork {
		}
		for i := range gates {
			close(gates[i].release)
			results[i] = <-dones[i]
		}
	}()
	_ = srv.Stop()
	for i, r := range results {
		stopIFCheck(t, srv, r, fmt.Sprintf("session %d", i))
	}
}
