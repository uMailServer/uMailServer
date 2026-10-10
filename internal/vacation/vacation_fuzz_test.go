package vacation

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func FuzzVacationConfigFile(f *testing.F) {
	for _, s := range []string{
		``, `{}`, `{"enabled":true,"subject":"s","message":"m","send_interval":-1}`,
		`{"send_interval":9223372036854775807,"start_date":"0000-01-01T00:00:00Z"}`,
		`{"exclude_addresses":null}`, `[1]`, `{"enabled":"yes"}`, `{"start_date":"bad"}`,
	} {
		f.Add(s, "a@b.com", "x@y.com", "bulk")
	}
	f.Fuzz(func(t *testing.T, data, user, sender, prec string) {
		dir := t.TempDir()
		m := NewManager(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
		p := filepath.Join(dir, sanitizeFilename(user)+".json")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Skip()
		}
		_ = m.loadConfigs()
		_ = unsanitizeFilename(sanitizeFilename(user))
		if got := unsanitizeFilename(sanitizeFilename(user)); user != "" && got != user && !containsAt(user) {
			t.Fatalf("filename round trip %q -> %q", user, got)
		}
		h := map[string]string{"Precedence": prec}
		_ = m.ShouldSendAutoReply(user, sender, h)
		_ = m.CheckAndRecordAutoReply(user, sender, h)
		_, _, _ = m.GetAutoReplyMessage(user)
		_ = m.ListActiveVacations()
		_ = normalizeAddress(sender)
		_ = isAutomatedSender(normalizeAddress(sender))
	})
}

func containsAt(s string) bool {
	for i := 0; i+4 <= len(s); i++ {
		if s[i:i+4] == "_at_" {
			return true
		}
	}
	return false
}
