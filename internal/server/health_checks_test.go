package server

// setupHealthChecks was never called, so /health (and the MCP status tool)
// always answered "healthy" with no checks; its storage check was a no-op.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/health"
)

func TestHealthChecksRegistered(t *testing.T) {
	dir := t.TempDir()
	srv, err := New(&config.Config{
		Server:   config.ServerConfig{Hostname: "test.example.com", DataDir: dir},
		Database: config.DatabaseConfig{Path: dir + "/test.db"},
		Logging:  config.LoggingConfig{Level: "error"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.database.Close(); srv.storageDB.Close() })
	srv.setupHealthChecks()

	status := func() map[string]health.Status {
		out := map[string]health.Status{}
		for _, c := range srv.healthMonitor.Check(context.Background()).Checks {
			out[c.Name] = c.Status
		}
		return out
	}
	got := status()
	for _, name := range []string{"database", "storage", "disk_space"} {
		if _, ok := got[name]; !ok {
			t.Fatalf("check %q not registered: %v", name, got)
		}
	}
	if got["storage"] != health.StatusHealthy {
		t.Fatalf("message store unhealthy on a fresh data dir: %v", got)
	}

	// A store that cannot take a file is reported.
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	msgDir := filepath.Join(dir, "mail", "messages")
	if err := os.Chmod(msgDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(msgDir, 0o750) })
	if got := status(); got["storage"] != health.StatusUnhealthy {
		t.Fatalf("read-only message store reported %q", got["storage"])
	}
}
