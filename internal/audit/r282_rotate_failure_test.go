package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F7560: a failed rotation (rename refused) must not drop the audit record.
func TestR282RotateFailureKeepsRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	w, err := newRotatingWriter(path, 1, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxSize = 10
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	rec := "{\"type\":\"admin_action\"}\n"
	if n, err := w.Write([]byte(rec)); err != nil || n != len(rec) {
		t.Fatalf("audit record dropped on rotation failure: n=%d err=%v", n, err)
	}
	if _, err := w.Write([]byte("next\n")); err != nil {
		t.Fatalf("writer unusable after failed rotation: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "admin_action") || !strings.HasSuffix(string(data), "next\n") {
		t.Fatalf("content lost: %q", data)
	}
}

// Control: rotation still works when the directory is writable.
func TestR282RotateStillRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	w, err := newRotatingWriter(path, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxSize = 10
	_, _ = w.Write([]byte("0123456789"))
	_, _ = w.Write([]byte("second\n"))
	data, _ := os.ReadFile(path)
	if string(data) != "second\n" {
		t.Fatalf("expected fresh file, got %q", data)
	}
	m, _ := filepath.Glob(path + ".*")
	if len(m) != 1 {
		t.Fatalf("expected 1 backup, got %v", m)
	}
}
