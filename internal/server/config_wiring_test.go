package server

// Regression tests: tls.min_version, logging.format and http.trusted_proxies
// were validated by config but never reached the TLS manager, the logger or
// the API server, so setting them changed nothing.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

func TestTLSMinVersionReachesManager(t *testing.T) {
	for v, want := range map[string]uint16{"": tls.VersionTLS12, "1.2": tls.VersionTLS12, "1.3": tls.VersionTLS13} {
		if got := tlsMinVersion(v); got != want {
			t.Errorf("tlsMinVersion(%q) = %#x, want %#x", v, got, want)
		}
	}

	srv := junkRTServer(t, false)
	if got := srv.tlsManager.GetTLSConfig().MinVersion; got != tls.VersionTLS12 {
		t.Fatalf("default config: MinVersion %#x, want TLS 1.2", got)
	}
	srv.config.TLS.MinVersion = "1.3"
	if err := srv.Stop(); err != nil {
		t.Logf("stop: %v", err)
	}
	srv2, err := New(srv.config)
	if err != nil {
		t.Fatalf("INVALID: New: %v", err)
	}
	t.Cleanup(func() { _ = srv2.Stop() })
	if got := srv2.tlsManager.GetTLSConfig().MinVersion; got != tls.VersionTLS13 {
		t.Fatalf("tls.min_version 1.3: MinVersion %#x, want TLS 1.3", got)
	}
}

func TestLoggingFormat(t *testing.T) {
	var buf bytes.Buffer
	newLogHandler(&buf, config.LoggingConfig{Format: "text", Level: "info"}).
		Handle(t.Context(), recordForTest("hello"))
	if out := buf.String(); !strings.Contains(out, "msg=hello") || json.Valid(buf.Bytes()) {
		t.Fatalf("text format: got %q, want key=value output", out)
	}

	buf.Reset()
	newLogHandler(&buf, config.LoggingConfig{Format: "json", Level: "info"}).
		Handle(t.Context(), recordForTest("hello"))
	if !json.Valid(bytes.TrimSpace(buf.Bytes())) {
		t.Fatalf("json format: got %q, want a JSON record", buf.String())
	}
}

func TestAPIConfigTrustedProxies(t *testing.T) {
	srv := junkRTServer(t, false)
	srv.config.HTTP.TrustedProxies = []string{"10.0.0.0/8", "192.0.2.1"}
	if got := srv.apiConfig().TrustedProxies; !reflect.DeepEqual(got, srv.config.HTTP.TrustedProxies) {
		t.Fatalf("api TrustedProxies = %v, want %v", got, srv.config.HTTP.TrustedProxies)
	}
}

func recordForTest(msg string) slog.Record {
	return slog.NewRecord(time.Now(), slog.LevelInfo, msg, 0)
}
