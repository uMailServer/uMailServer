package server

// Regression tests for F4915 (Start failure leaves earlier subsystems running)
// and F4916 (Stop removes a PID file owned by another live process).

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func lifecycleTestConfig(t *testing.T) *config.Config {
	t.Helper()
	d := t.TempDir()
	return &config.Config{
		Server:   config.ServerConfig{Hostname: "test.example.com", DataDir: d},
		Database: config.DatabaseConfig{Path: d + "/test.db"},
		Logging:  config.LoggingConfig{Level: "error"},
		SMTP: config.SMTPConfig{Inbound: config.InboundSMTPConfig{
			Enabled: true, Bind: "127.0.0.1", Port: 0, MaxMessageSize: 1 << 20, MaxRecipients: 10}},
		IMAP:     config.IMAPConfig{Enabled: true, Bind: "127.0.0.1", Port: 0},
		POP3:     config.POP3Config{Enabled: true, Bind: "127.0.0.1", Port: 0},
		Admin:    config.AdminConfig{Bind: "127.0.0.1", Port: 0},
		Security: config.SecurityConfig{JWTSecret: "test-jwt-secret-0123456789abcdef0123456789"},
	}
}

func lifecycleBusyPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("INVALID: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func lifecyclePIDPath(cfg *config.Config) string {
	return filepath.Join(cfg.Server.DataDir, "umailserver.pid")
}

// assertRolledBack checks the invariant: a Start that returned an error
// leaves nothing it started behind (PID file, background goroutines' ctx).
func lifecycleAssertRolledBack(t *testing.T, srv *Server, cfg *config.Config, stage string) {
	t.Helper()
	_, statErr := os.Stat(lifecyclePIDPath(cfg))
	pidLeft := statErr == nil
	ctxLive := srv.ctx.Err() == nil
	t.Logf("%s: EXPECTED: pid file removed=true, background ctx cancelled=true", stage)
	t.Logf("%s: ACTUAL:   pid file removed=%v, background ctx cancelled=%v", stage, !pidLeft, !ctxLive)
	if pidLeft || ctxLive {
		t.Errorf("DEFECT F4915 (%s): failed Start left PID file=%v, goroutines/listeners running=%v", stage, pidLeft, ctxLive)
	}
}

// Control: a successful Start keeps its PID file and live context.
func TestStart_SuccessKeepsPIDFile(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("INVALID: Start: %v", err)
	}
	if _, err := os.Stat(lifecyclePIDPath(cfg)); err != nil || srv.ctx.Err() != nil {
		t.Fatalf("INVALID control: pid=%v ctx=%v", err, srv.ctx.Err())
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("INVALID: Stop: %v", err)
	}
	if _, err := os.Stat(lifecyclePIDPath(cfg)); !os.IsNotExist(err) {
		t.Fatalf("INVALID control: own PID file not removed by Stop: %v", err)
	}
}

func TestStart_IMAPFailureRollsBack(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	cfg.IMAP.Port = lifecycleBusyPort(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	if err := srv.Start(); err == nil || !strings.Contains(err.Error(), "IMAP") {
		t.Fatalf("INVALID: expected IMAP start failure, got %v", err)
	}
	lifecycleAssertRolledBack(t, srv, cfg, "imap")
}

func TestStart_POP3FailureRollsBack(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	cfg.POP3.Port = lifecycleBusyPort(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	if err := srv.Start(); err == nil || !strings.Contains(err.Error(), "POP3") {
		t.Fatalf("INVALID: expected POP3 start failure, got %v", err)
	}
	lifecycleAssertRolledBack(t, srv, cfg, "pop3")
	// Repeated Stop after the rollback must stay a no-op without error.
	if err := srv.Stop(); err != nil {
		t.Errorf("DEFECT F4915: Stop after rollback returned %v", err)
	}
}

// F4916 control: Stop removes the PID file this process wrote.
func TestStop_RemovesOwnPIDFile(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	if err := os.WriteFile(lifecyclePIDPath(cfg), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatalf("INVALID: write pid: %v", err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	_ = srv.Stop()
	if _, err := os.Stat(lifecyclePIDPath(cfg)); !os.IsNotExist(err) {
		t.Fatalf("INVALID control: own PID file survived Stop: %v", err)
	}
}

func lifecycleForeignPIDCase(t *testing.T, startFirst bool) {
	cfg := lifecycleTestConfig(t)
	foreign := os.Getppid() // a live process that is not us
	if foreign <= 1 || foreign == os.Getpid() {
		t.Fatalf("INVALID: no usable foreign pid (%d)", foreign)
	}
	content := strconv.Itoa(foreign) + "\n"
	if err := os.WriteFile(lifecyclePIDPath(cfg), []byte(content), 0o600); err != nil {
		t.Fatalf("INVALID: write pid: %v", err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	if startFirst {
		if err := srv.Start(); err == nil || !strings.Contains(err.Error(), "already running") {
			_ = srv.Stop()
			t.Fatalf("INVALID: expected 'already running', got %v", err)
		}
	}
	_ = srv.Stop()
	_ = srv.Stop()
	got, readErr := os.ReadFile(lifecyclePIDPath(cfg))
	t.Logf("EXPECTED: foreign PID file kept with %q", content)
	t.Logf("ACTUAL:   content=%q readErr=%v", string(got), readErr)
	if readErr != nil || string(got) != content {
		t.Errorf("DEFECT F4916: Stop removed/altered PID file of live process %d", foreign)
	}
}

func TestStop_KeepsForeignPIDFileAfterFailedStart(t *testing.T) { lifecycleForeignPIDCase(t, true) }
func TestStop_KeepsForeignPIDFileWithoutStart(t *testing.T)     { lifecycleForeignPIDCase(t, false) }
