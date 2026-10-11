//go:build !windows

package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F6462: a config file holding credentials must trigger a warning when it is
// group/other readable, and stay quiet for 0600 or secret-free files.
func TestR166ConfigSecretsPermissionWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	write := func(name, body string, mode os.FileMode) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	dd := "server:\n  data_dir: " + t.TempDir() + "\n"
	secret := dd + "security:\n  jwt_secret: \"0123456789abcdef0123456789abcdef0123456789\"\n"

	for _, tc := range []struct {
		name string
		body string
		mode os.FileMode
		warn bool
	}{
		{"open.yaml", secret, 0o644, true},
		{"private.yaml", secret, 0o600, false},
		{"nosecret.yaml", dd + "  hostname: mail.example.com\n", 0o644, false},
	} {
		buf.Reset()
		if _, err := Load(write(tc.name, tc.body, tc.mode)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := strings.Contains(buf.String(), "readable by group/other")
		if got != tc.warn {
			t.Errorf("%s: warned=%v want %v\n%s", tc.name, got, tc.warn, buf.String())
		}
		if strings.Contains(buf.String(), "0123456789abcdef") {
			t.Errorf("%s: log leaked secret", tc.name)
		}
	}
}
