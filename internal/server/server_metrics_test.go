package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// pickFreePort returns an OS-assigned free TCP port. Used so the test never
// races against the production 8080 default or another concurrent test.
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func TestStartMetrics_DisabledNoServer(t *testing.T) {
	srv := &Server{
		logger: slog.Default(),
		config: &config.Config{Metrics: config.MetricsConfig{Enabled: false}},
	}
	srv.startMetrics()
	if srv.metricsHTTPServer != nil {
		t.Error("expected no metrics server when disabled")
	}
}

func TestStartMetrics_ServesPrometheusBody(t *testing.T) {
	port := pickFreePort(t)
	srv := &Server{
		logger: slog.Default(),
		config: &config.Config{Metrics: config.MetricsConfig{
			Enabled: true, Bind: "127.0.0.1", Port: port, Path: "/metrics",
		}},
	}
	srv.startMetrics()
	defer srv.stopMetrics(context.Background())

	if srv.metricsHTTPServer == nil {
		t.Fatal("metrics server not started")
	}

	url := "http://127.0.0.1:" + portString(port) + "/metrics"
	body := getWithRetry(t, url, 2*time.Second)
	if !strings.Contains(body, "umailserver_smtp_connections_total") {
		t.Errorf("body missing expected metric:\n%s", body)
	}
}

func TestStartMetrics_HealthzOK(t *testing.T) {
	port := pickFreePort(t)
	srv := &Server{
		logger: slog.Default(),
		config: &config.Config{Metrics: config.MetricsConfig{
			Enabled: true, Bind: "127.0.0.1", Port: port, Path: "/metrics",
		}},
	}
	srv.startMetrics()
	defer srv.stopMetrics(context.Background())

	url := "http://127.0.0.1:" + portString(port) + "/healthz"
	body := getWithRetry(t, url, 2*time.Second)
	if strings.TrimSpace(body) != "ok" {
		t.Errorf("/healthz body = %q", body)
	}
}

func TestStartMetrics_DefaultPathFallback(t *testing.T) {
	port := pickFreePort(t)
	srv := &Server{
		logger: slog.Default(),
		config: &config.Config{Metrics: config.MetricsConfig{
			Enabled: true, Bind: "127.0.0.1", Port: port, // no Path => default /metrics
		}},
	}
	srv.startMetrics()
	defer srv.stopMetrics(context.Background())

	url := "http://127.0.0.1:" + portString(port) + "/metrics"
	body := getWithRetry(t, url, 2*time.Second)
	if !strings.Contains(body, "umailserver_") {
		t.Errorf("default-path scrape failed:\n%s", body)
	}
}

func TestStopMetrics_NilSafe(t *testing.T) {
	srv := &Server{logger: slog.Default()}
	srv.stopMetrics(context.Background())
}

func portString(p int) string {
	return strconv.Itoa(p)
}

// getWithRetry fetches a URL, retrying briefly while the server boots. The
// body is returned as a string. Failure to reach the server within `timeout`
// fails the test.
func getWithRetry(t *testing.T, url string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		resp, err := http.Get(url) //nolint:gosec // test against 127.0.0.1
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("get %s: %v", url, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
