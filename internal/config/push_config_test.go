package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_PushDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Push.Enabled {
		t.Error("push must be enabled by default")
	}
	if cfg.Push.Subject != "mailto:admin@umailserver.local" {
		t.Errorf("subject = %q", cfg.Push.Subject)
	}
	if cfg.Push.VAPIDPublicKey != "" || cfg.Push.VAPIDPrivateKey != "" {
		t.Error("VAPID keys must be empty by default (auto-generation)")
	}
}

func TestLoad_PushSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "umailserver.yaml")
	yaml := `
server:
  hostname: localhost
  data_dir: ` + dir + `
push:
  enabled: true
  subject: "https://example.com/push-contact"
  vapid_public_key: "BPub-AAA"
  vapid_private_key: "Priv-BBB"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Push.Subject != "https://example.com/push-contact" {
		t.Errorf("subject: %q", cfg.Push.Subject)
	}
	if cfg.Push.VAPIDPublicKey != "BPub-AAA" || cfg.Push.VAPIDPrivateKey != "Priv-BBB" {
		t.Errorf("vapid: %+v", cfg.Push)
	}
}

func TestValidate_PushDisabled_SkipsChecks(t *testing.T) {
	cfg := validConfigForTest(t)
	cfg.Push.Enabled = false
	cfg.Push.Subject = "" // would normally fail validation
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled push must skip validation: %v", err)
	}
}

func TestValidate_PushSubjectRequired(t *testing.T) {
	cfg := validConfigForTest(t)
	cfg.Push.Enabled = true
	cfg.Push.Subject = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty subject")
	}
}

func TestValidate_PushSubjectScheme(t *testing.T) {
	cases := []struct {
		subject string
		ok      bool
	}{
		{"mailto:admin@example.com", true},
		{"https://example.com/contact", true},
		{"http://example.com/contact", false},
		{"admin@example.com", false},
		{"ftp://x", false},
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			cfg := validConfigForTest(t)
			cfg.Push.Enabled = true
			cfg.Push.Subject = tc.subject
			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestValidate_PushVAPIDPaired(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		ok   bool
	}{
		{"both empty (auto-generate)", func(c *Config) {
			c.Push.VAPIDPublicKey = ""
			c.Push.VAPIDPrivateKey = ""
		}, true},
		{"both set", func(c *Config) {
			c.Push.VAPIDPublicKey = "pub"
			c.Push.VAPIDPrivateKey = "priv"
		}, true},
		{"only public", func(c *Config) {
			c.Push.VAPIDPublicKey = "pub"
		}, false},
		{"only private", func(c *Config) {
			c.Push.VAPIDPrivateKey = "priv"
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfigForTest(t)
			cfg.Push.Enabled = true
			cfg.Push.Subject = "mailto:a@b"
			tc.mut(cfg)
			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Fatalf("ok expected: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
