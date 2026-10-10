package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegressionF5802ZeroMax(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a.log")
	w, _ := NewRotatingWriter(f, 0, 0, 0)
	for i := 0; i < 5; i++ {
		w.Write([]byte("x\n"))
	}
	w.Close()
	e, _ := os.ReadDir(d)
	if len(e) > 2 {
		t.Errorf("files=%d", len(e))
	}
}
func TestRegressionF5804Huge(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "a.log")
	w, _ := NewRotatingWriter(f, 1, 0, 0)
	big := make([]byte, 2<<20)
	w.Write(big)
	w.Write(big)
	w.Close()
	e, _ := os.ReadDir(d)
	if len(e) > 2 {
		t.Errorf("files=%d (empty backups)", len(e))
	}
}
