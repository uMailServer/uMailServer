package server

// Regression tests for F5116: ManageSieve listens on the configured
// managesieve.bind / managesieve.port instead of a hard-coded 0.0.0.0:4190.

import (
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func manageSieveBindServer(t *testing.T, bind string, port int) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.DataDir = dir
	cfg.Server.Hostname = "mx.test.com"
	cfg.Database.Path = filepath.Join(dir, "db")
	cfg.Logging.Level = "error"
	cfg.Logging.Output = ""
	cfg.TLS.ACME.Enabled = false
	cfg.ManageSieve.Enabled = true
	cfg.ManageSieve.Bind = bind
	cfg.ManageSieve.Port = port
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func TestManageSieveAddrs(t *testing.T) {
	for _, tc := range []struct {
		bind          string
		port          int
		plain, tlsStr string
	}{
		{"0.0.0.0", 4190, "0.0.0.0:4190", "0.0.0.0:4191"},
		{"127.0.0.1", 0, "127.0.0.1:0", "127.0.0.1:0"},
		{"::1", 4190, "[::1]:4190", "[::1]:4191"},
		{"", 65535, ":65535", ":0"},
	} {
		plain, tlsAddr := manageSieveAddrs(tc.bind, tc.port)
		if plain != tc.plain || tlsAddr != tc.tlsStr {
			t.Errorf("manageSieveAddrs(%q, %d) = %q, %q; want %q, %q", tc.bind, tc.port, plain, tlsAddr, tc.plain, tc.tlsStr)
		}
	}
}

func TestManageSieveUsesConfiguredBind(t *testing.T) {
	// Keep 0.0.0.0:4190 busy so a server ignoring the config cannot start.
	if ln, err := net.Listen("tcp", "0.0.0.0:4190"); err == nil {
		t.Cleanup(func() { _ = ln.Close() })
	}
	srv := manageSieveBindServer(t, "127.0.0.1", 0)
	srv.startManageSieve()
	if srv.manageSieveServer == nil || srv.manageSieveServer.Addr() == nil {
		t.Fatalf("F5116: ManageSieve did not start on the configured 127.0.0.1:0")
	}
	host, _, err := net.SplitHostPort(srv.manageSieveServer.Addr().String())
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("F5116: listening on %v, want 127.0.0.1", srv.manageSieveServer.Addr())
	}
}

// When the TLS listener cannot bind, the already-open plain listener is
// closed instead of being left serving without an owner.
func TestManageSieveListenFailureReleasesPlainPort(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		busy, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		tlsPort := busy.Addr().(*net.TCPAddr).Port
		plainAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(tlsPort-1))
		probe, err := net.Listen("tcp", plainAddr)
		if err != nil {
			_ = busy.Close()
			continue
		}
		_ = probe.Close()

		srv := manageSieveBindServer(t, "127.0.0.1", tlsPort-1)
		srv.startManageSieve()
		_ = busy.Close()
		if srv.manageSieveServer != nil {
			t.Fatalf("ManageSieve started although its TLS port was busy")
		}
		again, err := net.Listen("tcp", plainAddr)
		if err != nil {
			t.Fatalf("plain ManageSieve listener left open after failed start: %v", err)
		}
		_ = again.Close()
		return
	}
	t.Skip("no free port pair found")
}
