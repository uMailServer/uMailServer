package server

// Regression tests for F5430 (an enabled CalDAV/CardDAV/MCP/metrics/
// ManageSieve listener that cannot bind was only logged while Start returned
// nil) and F5431 (an invalid metrics.path panicked Start).

import (
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func listenersBindConfig(t *testing.T, service string, port int) *config.Config {
	t.Helper()
	cfg := lifecycleTestConfig(t)
	switch service {
	case "caldav":
		cfg.CalDAV = config.CalDAVConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	case "carddav":
		cfg.CardDAV = config.CardDAVConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	case "mcp":
		cfg.MCP = config.MCPConfig{Enabled: true, Bind: "127.0.0.1", Port: port, AuthToken: "tok"}
	case "metrics":
		cfg.Metrics = config.MetricsConfig{Enabled: true, Bind: "127.0.0.1", Port: port, Path: "/metrics"}
	case "managesieve":
		cfg.ManageSieve = config.ManageSieveConfig{Enabled: true, Bind: "127.0.0.1", Port: port}
	default:
		t.Fatalf("unknown service %q", service)
	}
	return cfg
}

// listenersBindStart runs Start, converting a panic into an error, and
// always stops the server afterwards.
func listenersBindStart(t *testing.T, cfg *config.Config) (err error) {
	t.Helper()
	srv, nerr := New(cfg)
	if nerr != nil {
		t.Fatalf("New: %v", nerr)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("PANIC: %v", r)
		}
		_ = srv.Stop()
	}()
	return srv.Start()
}

var listenersBindServices = []string{"caldav", "carddav", "mcp", "metrics", "managesieve"}

func TestStart_OptionalListenerFreePortStarts(t *testing.T) {
	for _, svc := range listenersBindServices {
		if err := listenersBindStart(t, listenersBindConfig(t, svc, 0)); err != nil {
			t.Errorf("%s on a free port: Start: %v", svc, err)
		}
	}
}

func TestStart_OptionalListenerBusyPortFails(t *testing.T) {
	for _, svc := range listenersBindServices {
		busy := lifecycleBusyPort(t)
		cfg := listenersBindConfig(t, svc, busy)
		err := listenersBindStart(t, cfg)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), svc) {
			t.Errorf("F5430: %s on busy port %d: Start error = %v, want a %s bind error", svc, busy, err, svc)
			continue
		}
		if _, statErr := os.Stat(lifecyclePIDPath(cfg)); statErr == nil {
			t.Errorf("F5430: %s: PID file left after failed Start", svc)
		}
	}
}

// A failed optional bind rolls back listeners opened earlier in Start, and a
// further Stop is harmless.
func TestStart_OptionalListenerFailureReleasesIMAP(t *testing.T) {
	imapPort := pickFreePort(t)
	cfg := listenersBindConfig(t, "caldav", lifecycleBusyPort(t))
	cfg.IMAP.Port = imapPort
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := srv.Start(); err == nil {
		_ = srv.Stop()
		t.Fatal("F5430: Start succeeded although CalDAV port was busy")
	}
	if err := srv.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", imapPort))
	if err != nil {
		t.Fatalf("F5430: IMAP port still bound after failed Start: %v", err)
	}
	_ = ln.Close()
}

func TestStart_InvalidMetricsPathReturnsError(t *testing.T) {
	for _, p := range []string{"metrics", "/healthz", "/{bad"} {
		cfg := listenersBindConfig(t, "metrics", 0)
		cfg.Metrics.Path = p
		err := listenersBindStart(t, cfg)
		if err == nil || strings.HasPrefix(err.Error(), "PANIC") || !strings.Contains(err.Error(), "metrics.path") {
			t.Errorf("F5431: metrics.path=%q: Start = %v, want a metrics.path error", p, err)
		}
		if _, statErr := os.Stat(lifecyclePIDPath(cfg)); statErr == nil {
			t.Errorf("F5431: metrics.path=%q: PID file left after failed Start", p)
		}
	}
	for _, p := range []string{"/", "/prom", "GET /metrics"} {
		cfg := listenersBindConfig(t, "metrics", 0)
		cfg.Metrics.Path = p
		if err := listenersBindStart(t, cfg); err != nil {
			t.Errorf("valid metrics.path=%q: Start: %v", p, err)
		}
	}
}
