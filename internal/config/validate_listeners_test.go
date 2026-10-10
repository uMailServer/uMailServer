package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadTestYAML writes body (with a temp data_dir) to a config file and loads it.
func loadTestYAML(t *testing.T, body string) error {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "umailserver.yaml")
	y := "server:\n  hostname: mail.example.com\n  data_dir: " + filepath.Join(dir, "data") + "\n" + body
	if err := os.WriteFile(p, []byte(y), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	return err
}

// TestValidateListenerPortsAndSigns covers F5360 (port check mirrors the
// listeners server.Start binds), F5361 (port range), F5362 (positive
// max_message_size) and F5363 (non-negative durations).
func TestValidateListenerPortsAndSigns(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"defaults", "", ""},
		{"F5360 imap 995 with pop3 disabled", "imap:\n  port: 995\n", ""},
		{"F5360 imap 995 with pop3 enabled", "imap:\n  port: 995\npop3:\n  enabled: true\n", "port conflict"},
		{"F5360 caldav on metrics port", "caldav:\n  enabled: true\n  port: 8080\n", "port conflict"},
		{"F5360 jmap on http port", "jmap:\n  enabled: true\n  port: 443\n", "port conflict"},
		{"F5360 carddav on managesieve TLS port", "carddav:\n  enabled: true\n  port: 4191\n", "port conflict"},
		{"F5360 disabled caldav on metrics port", "caldav:\n  port: 8080\n", ""},
		{"F5361 imap 70000", "imap:\n  port: 70000\n", "out of range"},
		{"F5361 mcp -1", "mcp:\n  port: -1\n", "out of range"},
		{"F5361 caldav 65535", "caldav:\n  enabled: true\n  port: 65535\n", ""},
		{"F5362 max_message_size -1", "smtp:\n  inbound:\n    max_message_size: -1\n", "must not be negative"},
		{"F5362 max_message_size 0", "smtp:\n  inbound:\n    max_message_size: 0\n", "max_message_size"},
		{"F5363 lockout_duration -15m", "security:\n  lockout_duration: -15m\n", "security.lockout_duration"},
		{"F5363 idle_timeout -1m", "imap:\n  idle_timeout: -1m\n", "imap.idle_timeout"},
		{"F5363 lockout_duration 0s", "security:\n  lockout_duration: 0s\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loadTestYAML(t, tt.yaml)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestSetupWizardNeverSavesUnloadableConfig covers F5364: a blank ACME email
// is re-asked, and a config Load would reject is never saved.
func TestSetupWizardNeverSavesUnloadableConfig(t *testing.T) {
	run := func(t *testing.T, answers ...string) (string, error) {
		t.Helper()
		dir := t.TempDir()
		services := []string{"y", "", "y", "", "y", "", "n", "y", ""}
		in := append(append([]string{dir, "mail.example.com"}, services...), answers...)
		w := NewSetupWizard()
		w.reader = bufio.NewReader(strings.NewReader(strings.Join(in, "\n") + "\n"))
		cfg, err := w.Run()
		if err == nil && cfg.TLS.ACME.Enabled && cfg.TLS.ACME.Email == "" {
			t.Fatal("wizard returned ACME config without email")
		}
		return filepath.Join(dir, "config.yaml"), err
	}

	t.Run("blank email re-asked", func(t *testing.T) {
		p, err := run(t, "y", "", "admin@example.com", "n", "", "")
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if _, err := Load(p); err != nil {
			t.Fatalf("saved config does not load: %v", err)
		}
	})
	t.Run("eof at email fails without saving", func(t *testing.T) {
		p, err := run(t, "y")
		if err == nil || !strings.Contains(err.Error(), "tls.acme.email") {
			t.Fatalf("Run error = %v, want tls.acme.email", err)
		}
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Fatalf("config saved despite invalid configuration: %v", statErr)
		}
	})
}
