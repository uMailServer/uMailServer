package server

// Regression tests for F5530: an enabled JMAP, HTTP API, admin or SMTP
// (25/587/465) listener that cannot bind was only logged from a goroutine
// while Start returned nil.

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// coreBindServices maps each service to the text its bind error must contain.
var coreBindServices = map[string]string{
	"jmap":          "jmap",
	"api":           "api",
	"admin":         "admin",
	"smtp":          "smtp",
	"submission":    "submission",
	"submissiontls": "submission tls",
}

func coreBindConfig(t *testing.T, svc string, port int) *config.Config {
	t.Helper()
	cfg := lifecycleTestConfig(t)
	cfg.HTTP.Bind = "127.0.0.1"
	switch svc {
	case "jmap":
		cfg.JMAP = config.JMAPConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	case "api":
		cfg.HTTP.Port = port
	case "admin":
		cfg.Admin = config.AdminConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	case "smtp":
		cfg.SMTP.Inbound.Port = port
	case "submission":
		cfg.SMTP.Submission = config.SubmissionSMTPConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	case "submissiontls":
		cfg.SMTP.SubmissionTLS = config.SubmissionTLSConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
		cfg.TLS.CertFile, cfg.TLS.KeyFile = pop3TLSCert(t, t.TempDir())
	default:
		t.Fatalf("unknown service %q", svc)
	}
	return cfg
}

// coreBindReleased waits, up to a deadline, until port can be bound again:
// a listener whose Serve goroutine had not started when Stop ran is closed
// by Serve on entry.
func coreBindReleased(port int) error {
	deadline := time.Now().Add(3 * time.Second)
	for {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln.Close()
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStart_CoreListenerFreePortStarts(t *testing.T) {
	for svc := range coreBindServices {
		if err := listenersBindStart(t, coreBindConfig(t, svc, 0)); err != nil {
			t.Errorf("%s on a free port: Start: %v", svc, err)
		}
	}
}

func TestStart_CoreListenerBusyPortFails(t *testing.T) {
	for svc, want := range coreBindServices {
		busy := lifecycleBusyPort(t)
		cfg := coreBindConfig(t, svc, busy)
		err := listenersBindStart(t, cfg)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Errorf("F5530: %s on busy port %d: Start error = %v, want a %s bind error", svc, busy, err, want)
			continue
		}
		if _, statErr := os.Stat(lifecyclePIDPath(cfg)); statErr == nil {
			t.Errorf("F5530: %s: PID file left after failed Start", svc)
		}
	}
}

// A bind failure on the admin listener (the last core listener) rolls back
// the SMTP, JMAP and API listeners opened before it; a further Stop is
// harmless.
func TestStart_CoreListenerFailureReleasesPorts(t *testing.T) {
	cfg := coreBindConfig(t, "admin", lifecycleBusyPort(t))
	ports := map[string]int{
		"smtp": pickFreePort(t), "submission": pickFreePort(t), "submissiontls": pickFreePort(t),
		"jmap": pickFreePort(t), "api": pickFreePort(t),
	}
	cfg.SMTP.Inbound.Port = ports["smtp"]
	cfg.SMTP.Submission = config.SubmissionSMTPConfig{Enabled: true, Bind: "127.0.0.1", Port: ports["submission"]}
	cfg.SMTP.SubmissionTLS = config.SubmissionTLSConfig{Enabled: true, Bind: "127.0.0.1", Port: ports["submissiontls"]}
	cfg.JMAP = config.JMAPConfig{Enabled: true, Bind: "127.0.0.1", Port: ports["jmap"]}
	cfg.HTTP.Port = ports["api"]
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err == nil || !strings.Contains(err.Error(), "admin") {
		_ = srv.Stop()
		t.Fatalf("F5530: Start = %v, want an admin bind error", err)
	}
	if err := srv.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	for svc, p := range ports {
		if err := coreBindReleased(p); err != nil {
			t.Errorf("F5530: %s port %d still bound after failed Start: %v", svc, p, err)
		}
	}
}
