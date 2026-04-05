package api

import (
	"net/http"
	"net/http/pprof"
	"strings"
)

// pprofHandler routes pprof requests to the standard library handlers.
// This provides Go runtime profiling data for performance analysis.
func (s *Server) pprofHandler(w http.ResponseWriter, r *http.Request) {
	// Extract the pprof command from the URL path
	// Path format: /debug/pprof/{cmd}
	path := strings.TrimPrefix(r.URL.Path, "/debug/pprof")
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "index":
		pprof.Index(w, r)
	case "cmdline":
		pprof.Cmdline(w, r)
	case "profile":
		pprof.Profile(w, r)
	case "symbol":
		pprof.Symbol(w, r)
	case "trace":
		pprof.Trace(w, r)
	case "goroutine":
		pprof.Handler("goroutine").ServeHTTP(w, r)
	case "heap":
		pprof.Handler("heap").ServeHTTP(w, r)
	case "threadcreate":
		pprof.Handler("threadcreate").ServeHTTP(w, r)
	case "block":
		pprof.Handler("block").ServeHTTP(w, r)
	case "mutex":
		pprof.Handler("mutex").ServeHTTP(w, r)
	case "allocs":
		pprof.Handler("allocs").ServeHTTP(w, r)
	default:
		// Try to handle as a custom profile
		if handler := pprof.Handler(path); handler != nil {
			handler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	}
}

// RegisterPprofRoutes adds pprof routes to the router (admin only)
func (s *Server) RegisterPprofRoutes(mux *http.ServeMux) {
	// Wrap pprof handlers with admin middleware
	mux.HandleFunc("/debug/pprof/", s.adminMiddleware(http.HandlerFunc(s.pprofHandler)).ServeHTTP)
}
