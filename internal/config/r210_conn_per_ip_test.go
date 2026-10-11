package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// F6870: smtp.max_connections_per_ip must be a real YAML key with a default.
func TestF6870_MaxConnectionsPerIP(t *testing.T) {
	if got := DefaultConfig().SMTP.MaxConnectionsPerIP; got != 50 {
		t.Fatalf("default = %d, want 50", got)
	}
	var c Config
	dec := yaml.NewDecoder(strings.NewReader("smtp:\n  max_connections_per_ip: -1\n"))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("key rejected: %v", err)
	}
	if c.SMTP.MaxConnectionsPerIP != -1 {
		t.Fatalf("got %d, want -1 (disabled)", c.SMTP.MaxConnectionsPerIP)
	}
}
