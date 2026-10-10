package audit

import (
	"path/filepath"
	"testing"
)

// F5791: zero max size must not rotate on every write.
func TestF5791_ZeroMaxSizeNoRotationStorm(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.log")
	l, err := NewLogger(p, 0, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		l.LogLoginSuccess("u", "1.2.3.4")
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	// F5792: Close is idempotent
	if err := l.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if m, _ := filepath.Glob(p + ".*"); len(m) != 0 {
		t.Fatalf("unexpected backups: %v", m)
	}
}
