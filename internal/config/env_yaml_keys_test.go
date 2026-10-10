package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFromEnvYAMLSpelledKeys is a regression test for F5290: env keys
// spelled like the YAML keys (as in docs/configuration.md and the shipped
// docker-compose files) must be applied, the legacy Go-field spelling must
// keep working, and the YAML spelling wins when both are set.
func TestLoadFromEnvYAMLSpelledKeys(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "umailserver.yaml")
	fileDir := filepath.Join(base, "fromfile")
	if err := os.WriteFile(cfgPath, []byte("server:\n  hostname: file.example.com\n  data_dir: "+fileDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("yaml spelling", func(t *testing.T) {
		envDir := filepath.Join(base, "yaml")
		t.Setenv("UMAILSERVER_SERVER_DATA_DIR", envDir)
		t.Setenv("UMAILSERVER_SPAM_REJECT_THRESHOLD", "12.5")
		t.Setenv("UMAILSERVER_SMTP_SUBMISSION_TLS_MAX_CONNECTIONS", "77")
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.DataDir != envDir {
			t.Errorf("data_dir = %q, want %q", cfg.Server.DataDir, envDir)
		}
		if cfg.Spam.RejectThreshold != 12.5 {
			t.Errorf("reject_threshold = %v, want 12.5", cfg.Spam.RejectThreshold)
		}
		if cfg.SMTP.SubmissionTLS.MaxConnections != 77 {
			t.Errorf("submission_tls.max_connections = %d, want 77", cfg.SMTP.SubmissionTLS.MaxConnections)
		}
	})

	t.Run("legacy spelling", func(t *testing.T) {
		envDir := filepath.Join(base, "legacy")
		t.Setenv("UMAILSERVER_SERVER_DATADIR", envDir)
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.DataDir != envDir {
			t.Errorf("data_dir = %q, want %q", cfg.Server.DataDir, envDir)
		}
	})

	t.Run("yaml spelling wins", func(t *testing.T) {
		want := "0123456789abcdef0123456789abcdef-yaml"
		t.Setenv("UMAILSERVER_SECURITY_JWTSECRET", "0123456789abcdef0123456789abcdef-legacy")
		t.Setenv("UMAILSERVER_SECURITY_JWT_SECRET", want)
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Security.JWTSecret != want {
			t.Errorf("jwt_secret = %q, want %q", cfg.Security.JWTSecret, want)
		}
	})
}
