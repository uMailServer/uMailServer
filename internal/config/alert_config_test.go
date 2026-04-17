package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig_AlertDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Alert.Enabled {
		t.Error("alerts must be disabled by default")
	}
	if got, want := time.Duration(cfg.Alert.MinInterval), 5*time.Minute; got != want {
		t.Errorf("min_interval = %v, want %v", got, want)
	}
	if cfg.Alert.MaxAlerts != 100 {
		t.Errorf("max_alerts = %d, want 100", cfg.Alert.MaxAlerts)
	}
	if cfg.Alert.DiskThreshold != 85.0 {
		t.Errorf("disk_threshold = %v, want 85.0", cfg.Alert.DiskThreshold)
	}
	if cfg.Alert.MemoryThreshold != 90.0 {
		t.Errorf("memory_threshold = %v, want 90.0", cfg.Alert.MemoryThreshold)
	}
	if cfg.Alert.ErrorThreshold != 5.0 {
		t.Errorf("error_threshold = %v, want 5.0", cfg.Alert.ErrorThreshold)
	}
	if cfg.Alert.TLSWarningDays != 7 {
		t.Errorf("tls_warning_days = %d, want 7", cfg.Alert.TLSWarningDays)
	}
	if cfg.Alert.QueueThreshold != 1000 {
		t.Errorf("queue_threshold = %d, want 1000", cfg.Alert.QueueThreshold)
	}
}

func TestLoad_AlertSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "umailserver.yaml")
	yaml := `
server:
  hostname: localhost
  data_dir: ` + dir + `
alert:
  enabled: true
  webhook_url: "https://hooks.example.com/alert"
  webhook_headers:
    Authorization: "Bearer abc"
  smtp_server: smtp.example.com
  smtp_port: 587
  smtp_username: alert@example.com
  smtp_password: s3cret
  from_address: alert@example.com
  to_addresses:
    - oncall@example.com
  use_tls: true
  min_interval: 2m
  max_alerts: 50
  disk_threshold: 80
  memory_threshold: 75
  error_threshold: 2.5
  tls_warning_days: 14
  queue_threshold: 500
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := cfg.Alert
	if !a.Enabled {
		t.Error("expected alert.enabled true")
	}
	if a.WebhookURL != "https://hooks.example.com/alert" {
		t.Errorf("webhook_url = %q", a.WebhookURL)
	}
	if a.WebhookHeaders["Authorization"] != "Bearer abc" {
		t.Errorf("webhook_headers not loaded: %#v", a.WebhookHeaders)
	}
	if a.SMTPServer != "smtp.example.com" || a.SMTPPort != 587 {
		t.Errorf("smtp settings: %+v", a)
	}
	if a.SMTPPassword != "s3cret" {
		t.Errorf("smtp_password = %q", a.SMTPPassword)
	}
	if len(a.ToAddresses) != 1 || a.ToAddresses[0] != "oncall@example.com" {
		t.Errorf("to_addresses = %v", a.ToAddresses)
	}
	if got, want := time.Duration(a.MinInterval), 2*time.Minute; got != want {
		t.Errorf("min_interval = %v, want %v", got, want)
	}
	if a.MaxAlerts != 50 || a.DiskThreshold != 80 || a.MemoryThreshold != 75 || a.ErrorThreshold != 2.5 {
		t.Errorf("thresholds: %+v", a)
	}
	if a.TLSWarningDays != 14 || a.QueueThreshold != 500 {
		t.Errorf("days/queue: %+v", a)
	}
}

func TestValidate_AlertEnabledRequiresDeliveryChannel(t *testing.T) {
	cfg := validConfigForTest(t)
	cfg.Alert.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when alert.enabled with no delivery channel")
	}
}

func TestValidate_AlertWebhookOnlyOK(t *testing.T) {
	cfg := validConfigForTest(t)
	cfg.Alert.Enabled = true
	cfg.Alert.WebhookURL = "https://hooks.example.com/x"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_AlertSMTPRequiresPortAndRecipients(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		ok   bool
	}{
		{"missing port", func(c *Config) {
			c.Alert.SMTPServer = "smtp.example.com"
			c.Alert.FromAddress = "a@b"
			c.Alert.ToAddresses = []string{"c@d"}
		}, false},
		{"missing from", func(c *Config) {
			c.Alert.SMTPServer = "smtp.example.com"
			c.Alert.SMTPPort = 587
			c.Alert.ToAddresses = []string{"c@d"}
		}, false},
		{"missing recipients", func(c *Config) {
			c.Alert.SMTPServer = "smtp.example.com"
			c.Alert.SMTPPort = 587
			c.Alert.FromAddress = "a@b"
		}, false},
		{"complete", func(c *Config) {
			c.Alert.SMTPServer = "smtp.example.com"
			c.Alert.SMTPPort = 587
			c.Alert.FromAddress = "a@b"
			c.Alert.ToAddresses = []string{"c@d"}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfigForTest(t)
			cfg.Alert.Enabled = true
			tc.mut(cfg)
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

func TestValidate_AlertThresholdRanges(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"disk too low", func(c *Config) { c.Alert.DiskThreshold = -1 }},
		{"disk too high", func(c *Config) { c.Alert.DiskThreshold = 101 }},
		{"memory out of range", func(c *Config) { c.Alert.MemoryThreshold = 200 }},
		{"error out of range", func(c *Config) { c.Alert.ErrorThreshold = -5 }},
		{"negative tls days", func(c *Config) { c.Alert.TLSWarningDays = -1 }},
		{"negative queue", func(c *Config) { c.Alert.QueueThreshold = -1 }},
		{"negative max alerts", func(c *Config) { c.Alert.MaxAlerts = -1 }},
		{"negative min interval", func(c *Config) { c.Alert.MinInterval = Duration(-time.Second) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfigForTest(t)
			cfg.Alert.Enabled = true
			cfg.Alert.WebhookURL = "https://hooks.example.com"
			tc.mut(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidate_AlertDisabled_AllowsInvalidValues(t *testing.T) {
	// When disabled, alert validation is skipped — bad values are tolerated
	// so users can leave the section in place while turning the feature off.
	cfg := validConfigForTest(t)
	cfg.Alert.Enabled = false
	cfg.Alert.DiskThreshold = -100
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected disabled alerts to skip validation: %v", err)
	}
}
