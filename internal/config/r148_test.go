package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func r148Cfg(t *testing.T) *Config {
	t.Helper()
	c := DefaultConfig()
	c.Server.DataDir = t.TempDir()
	return c
}

func r148Write(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "umailserver.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// F6300: an explicitly requested config file that is missing must be an error.
func TestRegressionF6300MissingExplicitConfig(t *testing.T) {
	t.Setenv("UMAILSERVER_SERVER_DATA_DIR", t.TempDir())
	missing := filepath.Join(t.TempDir(), "typo.yaml")
	cfg, err := Load(missing)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load(missing) = %v, %v; want os.ErrNotExist", cfg, err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error should name the path: %v", err)
	}
	c, err := LoadOptional(missing)
	if err != nil || c.Source() != "built-in defaults" {
		t.Fatalf("LoadOptional = %v, %v", c, err)
	}
	if c, err := Load(""); err != nil || c.Source() != "built-in defaults" {
		t.Fatalf("Load(\"\") = %v, %v", c, err)
	}
}

// F6301: typos in YAML keys are discoverable.
func TestRegressionF6301UnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := r148Write(t, dir, "server:\n  hostname: mx.example.com\n  data_dir: "+dir+"/d\nspam:\n  thershold: 5\ndomains:\n  - name: a.example\n    bogus: 1\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("unknown keys must not fail Load: %v", err)
	}
	got := strings.Join(cfg.UnknownKeys(), "|")
	if !strings.Contains(got, "spam.thershold (line 5)") || !strings.Contains(got, "domains[0].bogus") {
		t.Errorf("UnknownKeys = %q", got)
	}
	if err := cfg.ValidateStrict(); err == nil || !strings.Contains(err.Error(), "spam.thershold") {
		t.Errorf("ValidateStrict = %v", err)
	}
	clean := r148Write(t, t.TempDir(), "server:\n  hostname: mx.example.com\n  data_dir: "+t.TempDir()+"\n")
	c2, err := Load(clean)
	if err != nil || len(c2.UnknownKeys()) != 0 || c2.ValidateStrict() != nil {
		t.Errorf("clean config flagged: %v %v", err, c2.UnknownKeys())
	}
}

// F6302: relative paths in the file resolve against the config directory.
func TestRegressionF6302RelativePaths(t *testing.T) {
	dir := t.TempDir()
	p := r148Write(t, dir, "server:\n  data_dir: ./data\nlogging:\n  output: logs/app.log\ndatabase:\n  path: db/accounts.db\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.DataDir != filepath.Join(dir, "data") {
		t.Errorf("data_dir = %s", cfg.Server.DataDir)
	}
	if cfg.Logging.Output != filepath.Join(dir, "logs/app.log") {
		t.Errorf("logging.output = %s", cfg.Logging.Output)
	}
	if cfg.DatabasePath() != filepath.Join(dir, "db/accounts.db") {
		t.Errorf("database path = %s", cfg.DatabasePath())
	}
	q := r148Write(t, t.TempDir(), "server:\n  data_dir: "+t.TempDir()+"\nlogging:\n  output: stderr\n")
	c2, err := Load(q)
	if err != nil || c2.Logging.Output != "stderr" {
		t.Errorf("stderr must stay: %v %v", err, c2.Logging.Output)
	}
}

// F6303: unset database.path follows data_dir (same file the CLI opens).
func TestRegressionF6303DatabaseDefault(t *testing.T) {
	c := r148Cfg(t)
	if c.Database.Path != "" {
		t.Errorf("default database.path = %q, want empty", c.Database.Path)
	}
	if got, want := c.DatabasePath(), filepath.Join(c.Server.DataDir, "umailserver.db"); got != want {
		t.Errorf("DatabasePath = %s, want %s", got, want)
	}
}

// F6304: negative Size/Duration anywhere in the tree is rejected.
func TestRegressionF6304NegativeSizes(t *testing.T) {
	c := r148Cfg(t)
	c.Domains = []DomainConfig{{Name: "a.example", MaxMailboxSize: -1}}
	if err := c.Validate(); err == nil {
		t.Error("negative domain size accepted")
	}
	c = r148Cfg(t)
	c.LDAP.Timeout = -1
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "ldap.timeout") {
		t.Errorf("negative ldap.timeout: %v", err)
	}
}

// F6305: bare numbers for durations are rejected, via env and YAML.
func TestRegressionF6305BareDuration(t *testing.T) {
	t.Setenv("UMAILSERVER_SPAM_GREYLISTING_DELAY", "300")
	c := DefaultConfig()
	if err := loadFromEnv(c); err == nil || !strings.Contains(err.Error(), "unit") {
		t.Errorf("bare env duration: %v", err)
	}
	p := r148Write(t, t.TempDir(), "imap:\n  idle_timeout: 30\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "30") {
		t.Errorf("bare yaml duration: %v", err)
	}
}

// F6306: every port setting is range-checked, enabled or not.
func TestRegressionF6306PortRange(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"disabled pop3":  func(c *Config) { c.POP3.Enabled = false; c.POP3.Port = 70000 },
		"alert smtp":     func(c *Config) { c.Alert.SMTPPort = -1 },
		"imap starttls":  func(c *Config) { c.IMAP.STARTTLSPort = 99999 },
		"disabled admin": func(c *Config) { c.Admin.Enabled = false; c.Admin.Port = -5 },
	} {
		c := r148Cfg(t)
		mut(c)
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "out of range") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// F6307: bind addresses must be usable as "<bind>:<port>".
func TestRegressionF6307BindAddress(t *testing.T) {
	bad := []string{"::1", "0.0.0.0:25", "bad host", "a..b"}
	for _, b := range bad {
		c := r148Cfg(t)
		c.SMTP.Inbound.Bind = b
		if err := c.Validate(); err == nil {
			t.Errorf("bind %q accepted", b)
		}
	}
	for _, b := range []string{"", "0.0.0.0", "127.0.0.1", "[::1]", "[::]", "mail.example.com", "localhost"} {
		c := r148Cfg(t)
		c.HTTP.Bind = b
		if err := c.Validate(); err != nil {
			t.Errorf("bind %q rejected: %v", b, err)
		}
	}
}
