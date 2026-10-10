package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/mcp"
)

// startMCP creates and starts the MCP server (if enabled).
func (s *Server) startMCP() error {
	if !s.config.MCP.Enabled {
		return nil
	}

	mcpAddr := fmt.Sprintf("%s:%d", s.config.MCP.Bind, s.config.MCP.Port)
	mcpSrv := mcp.NewServer(s.database)
	if s.config.MCP.AuthToken == "" {
		token := generateSecureToken()
		s.config.MCP.AuthToken = token
		s.logger.Warn("MCP: no auth token configured; generated a random token - check server logs for token on first start")
		s.logger.Info("MCP auth token generated", "token_length", len(token))
	}
	mcpSrv.SetAuthToken(s.config.MCP.AuthToken)
	if s.config.MCP.AdminAuthToken != "" {
		mcpSrv.SetAdminAuthToken(s.config.MCP.AdminAuthToken)
	}
	if len(s.config.HTTP.CorsOrigins) > 0 {
		mcpSrv.SetCorsOrigin(strings.Join(s.config.HTTP.CorsOrigins, ","))
	}
	// Configure MCP rate limiting (use same limit as HTTP API)
	mcpSrv.SetRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)
	mcpSrv.SetTracingProvider(s.tracingProvider)
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
