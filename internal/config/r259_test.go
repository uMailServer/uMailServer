package config

import (
	"strings"
	"testing"
)

func r259Base(t *testing.T) *Config {
	c := DefaultConfig()
	c.Server.Hostname = "mail.example.com"
	c.Server.DataDir = t.TempDir()
	return c
}

// F7360: plaintext OTLP to a non-loopback host is refused unless insecure.
func TestF7360_TracingPlaintextNonLoopback(t *testing.T) {
	cases := []struct {
		endpoint string
		insecure bool
		wantErr  bool
	}{
		{"collector.example.com:4317", false, true},
		{"http://collector.example.com:4317", false, true},
		{"10.0.0.5:4317", false, true},
		{"https://collector.example.com:4317", false, false},
		{"collector.example.com:4317", true, false},
		{"localhost:4317", false, false},
		{"http://127.0.0.1:4317", false, false},
		{"[::1]:4317", false, false},
	}
	for _, tc := range cases {
		c := r259Base(t)
		c.Tracing.Enabled = true
		c.Tracing.Exporter = "otlp"
		c.Tracing.OTLPEndpoint = tc.endpoint
		c.Tracing.Insecure = tc.insecure
		err := c.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("%q insecure=%v: err=%v wantErr=%v", tc.endpoint, tc.insecure, err, tc.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), "tracing.insecure") {
			t.Errorf("error should mention tracing.insecure: %v", err)
		}
	}
	// Disabled tracing / non-otlp exporter is not subject to the check.
	c := r259Base(t)
	c.Tracing.OTLPEndpoint = "collector.example.com:4317"
	if err := c.Validate(); err != nil {
		t.Errorf("disabled tracing: %v", err)
	}
}

// F7361: an MCP listener on a non-loopback bind needs an explicit token; this
// must fail at load rather than at server start.
func TestF7361_MCPNonLoopbackNeedsToken(t *testing.T) {
	c := r259Base(t)
	c.MCP.Enabled = true
	c.MCP.Bind = "0.0.0.0"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "mcp.auth_token") {
		t.Fatalf("want mcp.auth_token error, got %v", err)
	}
	c.MCP.AuthToken = "an-explicit-token"
	if err := c.Validate(); err != nil {
		t.Fatalf("with token: %v", err)
	}
	c = r259Base(t)
	c.MCP.Enabled = true
	c.MCP.Bind = "127.0.0.1"
	if err := c.Validate(); err != nil {
		t.Fatalf("loopback default: %v", err)
	}
	c.MCP.Enabled = false
	c.MCP.Bind = "0.0.0.0"
	if err := c.Validate(); err != nil {
		t.Fatalf("disabled: %v", err)
	}
}

// F7362: defaults for admin/metrics/MCP binds stay loopback.
func TestF7362_DefaultBindsLoopback(t *testing.T) {
	c := DefaultConfig()
	for name, b := range map[string]string{"admin": c.Admin.Bind, "metrics": c.Metrics.Bind, "mcp": c.MCP.Bind} {
		if !IsLoopbackHost(b) {
			t.Errorf("%s default bind %q is not loopback", name, b)
		}
	}
	if c.Tracing.Insecure {
		t.Error("tracing.insecure must default to false")
	}
}
