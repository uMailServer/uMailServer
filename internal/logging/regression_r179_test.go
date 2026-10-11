package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F6573: a failed rotation (rename refused) must neither drop the record nor
// leave the writer without an open file for all later writes.
func TestR179RotateFailureKeepsWriterUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	w, err := NewRotatingWriter(path, 1, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxSize = 10
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // rename now fails
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	if n, err := w.Write([]byte("overflow-line\n")); err != nil || n != len("overflow-line\n") {
		t.Fatalf("record dropped on rotation failure: n=%d err=%v", n, err)
	}
	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatalf("writer unusable after failed rotation: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "overflow-line") || !strings.HasSuffix(string(data), "a\n") {
		t.Fatalf("log content lost: %q", data)
	}
}
