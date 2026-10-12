package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func dd(t *testing.T) string { return t.TempDir() }

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// F6891: legacy smtp_per_* aliases drive the IP limits when ip_* is absent.
func TestR212LegacyRateAliases(t *testing.T) {
	cfg, err := Load(writeCfg(t, "server:\n  hostname: mail.example.com\n  data_dir: "+dd(t)+"\nsecurity:\n  rate_limit:\n    smtp_per_minute: 7\n    smtp_per_hour: 77\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Security.RateLimit.IPPerMinute != 7 || cfg.Security.RateLimit.IPPerHour != 77 {
		t.Fatalf("aliases not applied: %+v", cfg.Security.RateLimit)
	}
	// explicit canonical key wins
	cfg, err = Load(writeCfg(t, "server:\n  hostname: mail.example.com\n  data_dir: "+dd(t)+"\nsecurity:\n  rate_limit:\n    smtp_per_minute: 7\n    ip_per_minute: 9\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Security.RateLimit.IPPerMinute != 9 {
		t.Fatalf("ip_per_minute overridden: %d", cfg.Security.RateLimit.IPPerMinute)
	}
	// absent: defaults untouched
	cfg, _ = Load(writeCfg(t, "server:\n  hostname: mail.example.com\n  data_dir: "+dd(t)+"\n"))
	if cfg.Security.RateLimit.IPPerMinute != DefaultConfig().Security.RateLimit.IPPerMinute {
		t.Fatal("default changed")
	}
}

// F6892: ignored keys are detected (warn, not fail).
func TestR212IgnoredKeysWarn(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	cfg, err := Load(writeCfg(t, "server:\n  hostname: mail.example.com\n  data_dir: "+dd(t)+"\nspam:\n  greylisting:\n    delay: 1m\nstorage:\n  shared_folders: true\ndomains:\n  - name: example.com\n"))
	if err != nil {
		t.Fatalf("must not fail: %v", err)
	}
	_ = cfg
	for _, k := range []string{"storage.shared_folders", "domains"} {
		if !strings.Contains(buf.String(), "key="+k+" ") && !strings.Contains(buf.String(), "key="+k+"\n") {
			t.Errorf("no warning for %s: %s", k, buf.String())
		}
	}
	var root yaml.Node
	_ = yaml.Unmarshal([]byte("server:\n  hostname: x\n"), &root)
	if got := findIgnoredKeys(&root); len(got) != 0 {
		t.Fatalf("false positive: %v", got)
	}
}

// F6893: the shipped example no longer sets ignored keys.
func TestR212ExampleHasNoIgnoredKeys(t *testing.T) {
	data, err := os.ReadFile("../../umailserver.yaml.example")
	if err != nil {
		t.Fatal(err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if got := findIgnoredKeys(&root); len(got) != 0 {
		t.Fatalf("example sets ignored keys: %v", got)
	}
}

func TestR225GreylistDelayValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Spam.Greylisting.Delay = Duration(7 * time.Hour)
	if err := cfg.Validate(); err == nil {
		t.Fatal("delay >6h must be rejected")
	}
}
