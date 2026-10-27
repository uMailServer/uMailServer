package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func TestIsLoopbackBindF6593(t *testing.T) {
	for b, want := range map[string]bool{"127.0.0.1": true, "::1": true, "[::1]": true, "localhost": true,
		"": false, "0.0.0.0": false, "::": false, "192.0.2.1": false, "mail.example.com": false} {
		if got := isLoopbackBind(b); got != want {
			t.Errorf("isLoopbackBind(%q)=%v want %v", b, got, want)
		}
	}
}

func TestStartMCPRefusesNonLoopbackWithoutTokenF6593(t *testing.T) {
	for _, bind := range []string{"", "0.0.0.0"} {
		cfg := lifecycleTestConfig(t)
		cfg.MCP = config.MCPConfig{Enabled: true, Bind: bind, Port: 0}
		srv, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = srv.Stop() })
		if err := srv.startMCP(); err == nil || srv.mcpHTTPServer != nil {
			t.Fatalf("bind %q: expected refusal, got err=%v", bind, err)
		}
	}
}

func TestMCPTokenFileLooseModeTightenedF6593(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	p := filepath.Join(cfg.Server.DataDir, mcpTokenFile)
	if err := os.WriteFile(p, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.loadOrCreateMCPToken(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestReloadTLSF6594(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	srv.config.TLS.CertFile, srv.config.TLS.KeyFile = "", ""
	if err := srv.ReloadTLS(); err != nil {
		t.Fatalf("no manual cert: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "x.pem")
	_ = os.WriteFile(bad, []byte("junk"), 0o600)
	srv.config.TLS.CertFile, srv.config.TLS.KeyFile = bad, bad
	if err := srv.ReloadTLS(); err == nil {
		t.Fatal("invalid pair must error")
	}
}
