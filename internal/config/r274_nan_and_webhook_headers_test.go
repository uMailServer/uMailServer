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

func r274Write(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	y := "server:\n  data_dir: " + t.TempDir() + "\n" + body
	if err := os.WriteFile(p, []byte(y), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// F7480: NaN passes every comparison in Validate, so ".nan" thresholds loaded.
func TestR274NaNFloatRejected(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"reject nan", "spam:\n  reject_threshold: .nan\n", false},
		{"junk nan", "spam:\n  junk_threshold: .nan\n", false},
		{"sample rate nan", "tracing:\n  sample_rate: .nan\n", false},
		{"finite ok", "spam:\n  reject_threshold: 8.5\n", true},
		{"out of range still rejected", "spam:\n  reject_threshold: 120\n", false},
	} {
		_, err := Load(r274Write(t, tc.body, 0o600))
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v want ok=%v", tc.name, err, tc.ok)
		}
	}
	t.Setenv("UMAILSERVER_SPAM_JUNK_THRESHOLD", "NaN")
	if _, err := Load(r274Write(t, "", 0o600)); err == nil {
		t.Error("env NaN threshold accepted")
	}
}

// F7481: alert.webhook_headers hold credentials, so a group/other-readable
// file with only those must trigger the permission warning.
func TestR274WebhookHeadersPermissionWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		warn       bool
	}{
		{"headers 0644", "alert:\n  webhook_headers:\n    Authorization: Bearer abc\n", 0o644, true},
		{"headers 0600", "alert:\n  webhook_headers:\n    Authorization: Bearer abc\n", 0o600, false},
		{"empty headers 0644", "alert:\n  webhook_headers: {}\n", 0o644, false},
		{"no secrets 0644", "alert:\n  webhook_template: x\n", 0o644, false},
	} {
		buf.Reset()
		if _, err := Load(r274Write(t, tc.body, tc.mode)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := strings.Contains(buf.String(), "readable by group/other"); got != tc.warn {
			t.Errorf("%s: warned=%v want %v", tc.name, got, tc.warn)
		}
	}
}
