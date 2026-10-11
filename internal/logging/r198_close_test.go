package logging

import (
	"path/filepath"
	"testing"
)

// F6751: a closed writer must not reopen the file on a late Write (leaked
// descriptor) and a second Close must be a no-op.
func TestRotatingWriterWriteAfterClose_F6751(t *testing.T) {
	w, err := NewRotatingWriter(filepath.Join(t.TempDir(), "a.log"), 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := w.Write([]byte("late\n")); err == nil {
		t.Errorf("Write after Close succeeded (file reopened and leaked)")
	}
}
