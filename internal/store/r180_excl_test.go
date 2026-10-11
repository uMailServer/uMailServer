package store

import (
	"os"
	"path/filepath"
	"testing"
)

// F6581: tmp file creation is exclusive and a collision leaves the
// existing file intact.
func TestWriteNewFileExclusive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := writeNewFile(p, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := writeNewFile(p, []byte("b")); !os.IsExist(err) {
		t.Fatalf("want EEXIST, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a" {
		t.Fatalf("existing file clobbered: %q", b)
	}
}
