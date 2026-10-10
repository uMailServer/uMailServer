package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// startMetrics starts the dedicated Prometheus metrics HTTP server when
// `cfg.Metrics.Enabled` is true. It listens on `cfg.Metrics.Bind:Port` and
// serves the Prometheus text exposition format on `cfg.Metrics.Path` (default
// `/metrics`). Scrape access is intentionally unauthenticated — Prometheus
// servers reach this endpoint over private networking, and the metrics
// surface contains no secrets.
//
// The admin JSON `/metrics` endpoint on the API server stays in place for
// dashboards that want richer structure than the text format provides.
func (s *Server) startMetrics() error {
	if !s.config.Metrics.Enabled {
		s.logger.Debug("Metrics server disabled in config")
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.config.Metrics.Bind, s.config.Metrics.Port)
	path := s.config.Metrics.Path
	if path == "" {
		path = "/metrics"
	}

	mux, err := newMetricsMux(path)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := s.serveHTTP("Metrics", srv); err != nil {
		return err
	}
	s.metricsHTTPServer = srv
	s.logger.Info("Metrics server started", "addr", addr, "path", path)
	return nil
}

// newMetricsMux registers the Prometheus handler on path plus /healthz.
// ServeMux panics on an invalid pattern ("metrics") or on one that clashes
// with /healthz; report that as a config error instead of crashing Start
// (F5431).
func newMetricsMux(path string) (mux *http.ServeMux, err error) {
	defer func() {
		if r := recover(); r != nil {
			mux, err = nil, fmt.Errorf("invalid metrics.path %q: %v", path, r)
		}
	}()
	mux = http.NewServeMux()
	mux.HandleFunc(path, metrics.Get().PrometheusHandler)
	// Liveness probe convenience: scraper rigs often want a known-good URL
	// distinct from the metrics body itself.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux, nil
}

// stopMetrics shuts the metrics HTTP server down with a short grace period.
// Safe to call when the server was never started.
func (s *Server) stopMetrics(ctx context.Context) {
	if s.metricsHTTPServer == nil {
		return
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.metricsHTTPServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Warn("Metrics server shutdown returned error", "error", err)
	}
}
