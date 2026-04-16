package api

import (
	"net/http"

	"github.com/umailserver/umailserver/internal/tracing"
)

// traceRequest wraps the next handler in an http.<METHOD> server-kind span via
// the shared tracing.HTTPMiddleware. Passthrough when no provider is set.
func (s *Server) traceRequest(next http.Handler) http.Handler {
	return tracing.HTTPMiddleware(s.tracingProvider, "http", next)
}
