package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/cli"
	"github.com/umailserver/umailserver/internal/mcp"
)

// startMCP creates and starts the MCP server (if enabled).
func (s *Server) startMCP() error {
	if !s.config.MCP.Enabled {
		return nil
	}

	mcpAddr := fmt.Sprintf("%s:%d", s.config.MCP.Bind, s.config.MCP.Port)
	mcpSrv := mcp.NewServer(s.database)
	// F6593: an empty bind means all interfaces; a generated token read from
	// a local file is not an acceptable credential for a network-exposed
	// listener, so require an explicit token there.
	if !isLoopbackBind(s.config.MCP.Bind) && s.config.MCP.AuthToken == "" && s.config.MCP.AdminAuthToken == "" {
		return fmt.Errorf("MCP: refusing to listen on non-loopback bind %q without an explicit mcp.auth_token", s.config.MCP.Bind)
	}
	if s.config.MCP.AuthToken == "" && s.config.MCP.AdminAuthToken == "" {
		// F5590: a generated token must be obtainable by the operator, or
		// no client can ever authenticate. Keep it in data_dir (0600, stable
		// across restarts) instead of logging the secret.
		token, path, err := s.loadOrCreateMCPToken()
		if err != nil {
			return fmt.Errorf("MCP: no auth token configured and generated token could not be stored: %w", err)
		}
		s.config.MCP.AuthToken = token
		s.logger.Warn("MCP: no auth token configured; using generated token stored in file (set mcp.auth_token to override)", "path", path)
	}
	if s.config.MCP.AuthToken != "" {
		mcpSrv.SetAuthToken(s.config.MCP.AuthToken)
	}
	if s.config.MCP.AdminAuthToken != "" {
		mcpSrv.SetAdminAuthToken(s.config.MCP.AdminAuthToken)
	}
	if len(s.config.HTTP.CorsOrigins) > 0 {
		mcpSrv.SetCorsOrigin(strings.Join(s.config.HTTP.CorsOrigins, ","))
	}
	// Configure MCP rate limiting (use same limit as HTTP API)
	mcpSrv.SetRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)
	mcpSrv.SetTracingProvider(s.tracingProvider)
	if s.queue != nil {
		mcpSrv.SetQueue(s.queue)
	}
	mcpSrv.SetDiagnostics(cli.NewDiagnostics(s.config))
	if s.healthMonitor != nil {
		mcpSrv.SetHealthCheck(s.healthMonitor.Check)
	}
	mcpSrv.SetTLSReload(s.ReloadTLS)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", mcpSrv.HandleHTTP)

	srv := &http.Server{
		Addr:              mcpAddr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := s.serveHTTP("MCP", srv); err != nil {
		return err
	}
	s.mcpHTTPServer = srv
	s.logger.Info("MCP server started", "addr", mcpAddr)
	return nil
}

// mcpTokenFile is the data_dir file holding the generated MCP auth token.
const mcpTokenFile = "mcp_auth_token"

// loadOrCreateMCPToken returns the generated MCP token from data_dir,
// creating it (mode 0600) on first use (F5590).
func (s *Server) loadOrCreateMCPToken() (token, path string, err error) {
	path = filepath.Join(s.config.Server.DataDir, mcpTokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if token = strings.TrimSpace(string(data)); token != "" {
			// F6593: an existing file may have looser permissions.
			if err := os.Chmod(path, 0o600); err != nil && runtime.GOOS != "windows" {
				return "", path, err
			}
			return token, path, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", path, err
	}
	token = generateSecureToken()
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", path, err
	}
	return token, path, nil
}

// isLoopbackBind reports whether bind names only a loopback address. An empty
// bind (all interfaces) and unspecified addresses are not loopback.
func isLoopbackBind(bind string) bool {
	bind = strings.Trim(strings.TrimSpace(bind), "[]")
	if bind == "" {
		return false
	}
	if strings.EqualFold(bind, "localhost") {
		return true
	}
	ip := net.ParseIP(bind)
	return ip != nil && ip.IsLoopback()
}
