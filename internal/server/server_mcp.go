package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/umailserver/umailserver/internal/mcp"
)

// startMCP creates and starts the MCP server (if enabled).
func (s *Server) startMCP() {
	if !s.config.MCP.Enabled {
		return
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
	if len(s.config.HTTP.CorsOrigins) > 0 {
		mcpSrv.SetCorsOrigin(strings.Join(s.config.HTTP.CorsOrigins, ","))
	}
	// Configure MCP rate limiting (use same limit as HTTP API)
	mcpSrv.SetRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", mcpSrv.HandleHTTP)

	s.mcpHTTPServer = &http.Server{
		Addr:    mcpAddr,
		Handler: mux,
	}

	go func() {
		if err := s.mcpHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("MCP server error", "error", err)
		}
	}()
	s.logger.Info("MCP server started", "addr", mcpAddr)
}
