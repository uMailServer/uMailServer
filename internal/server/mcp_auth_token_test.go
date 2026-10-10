package server

// Regression test for F5590: with no mcp.auth_token configured (the shipped
// default) the MCP listener ran with a random token that was never shown to
// the operator, so no client could authenticate.

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func TestStartMCP_GeneratedTokenIsUsableAndStable(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	cfg.MCP = config.MCPConfig{Enabled: true, Bind: "127.0.0.1", Port: 0}
	path := filepath.Join(cfg.Server.DataDir, "mcp_auth_token")

	start := func() (*Server, string) {
		c := *cfg
		srv, err := New(&c)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		var logs bytes.Buffer
		srv.logger = slog.New(slog.NewTextHandler(&logs, nil))
		t.Cleanup(func() { _ = srv.Stop() })
		if err := srv.startMCP(); err != nil {
			t.Fatalf("startMCP: %v", err)
		}
		if srv.mcpHTTPServer == nil {
			t.Fatal("MCP listener not started")
		}
		if strings.Contains(logs.String(), c.MCP.AuthToken) {
			t.Error("generated MCP token written to the log")
		}
		return srv, c.MCP.AuthToken
	}
	status := func(srv *Server, token string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		srv.mcpHTTPServer.Handler.ServeHTTP(rr, req)
		return rr.Code
	}

	srv, generated := start()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("token file: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %v, want 0600", fi.Mode().Perm())
	}
	fileToken := strings.TrimSpace(string(data))
	if fileToken == "" || fileToken != generated {
		t.Fatal("token file does not hold the token the listener uses")
	}
	if got := status(srv, fileToken); got != http.StatusOK {
		t.Errorf("token from file: status %d, want 200", got)
	}
	if got := status(srv, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", got)
	}
	_ = srv.Stop()

	if _, again := start(); again != generated {
		t.Error("generated token changed across restarts with the same data_dir")
	}
}

func TestStartMCP_UnstorableTokenFailsStart(t *testing.T) {
	cfg := lifecycleTestConfig(t)
	cfg.MCP = config.MCPConfig{Enabled: true, Bind: "127.0.0.1", Port: 0}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = srv.Stop() }()
	// A directory at the token path makes both read and write fail.
	if err := os.Mkdir(filepath.Join(cfg.Server.DataDir, "mcp_auth_token"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := srv.startMCP(); err == nil {
		t.Error("startMCP succeeded without a usable token")
	}
	if srv.mcpHTTPServer != nil {
		t.Error("MCP listener started without a usable token")
	}
}
