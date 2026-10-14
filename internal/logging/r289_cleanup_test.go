package logging

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// F7630: retention pruning must only touch this writer's own timestamped
// backups, never other files that merely share the "<name>." prefix.
func TestR289CleanupLeavesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "mail.log")
	oldTime := time.Now().Add(-1000 * 24 * time.Hour)
	touch := func(p string) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, oldTime, oldTime)
	}
	keep := []string{"mail.log.keep", "mail.log.old-manual", "mail.log.gz", "mail.log.", "mail.log.2020", "mail.log.20200101-12000", "mail.log.20200101-120000x", "other.log"}
	for _, k := range keep {
		touch(filepath.Join(dir, k))
	}
	gone := []string{log + ".20200101-120000.000000000", log + ".20200101-120000"}
	for _, g := range gone {
		touch(g)
	}
	w, err := NewRotatingWriter(log, 1, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, k := range keep {
		if _, err := os.Stat(filepath.Join(dir, k)); err != nil {
			t.Errorf("unrelated file %s was deleted", k)
		}
	}
	for _, g := range gone {
		if _, err := os.Stat(g); err == nil {
			t.Errorf("expired backup %s was kept", g)
		}
	}
}

// Rotation happens only when a write would exceed maxSize (exactly maxSize
// does not rotate), no file exceeds maxSize under concurrent writers, and all
// log files are 0600.
func TestR289RotationBoundaryConcurrent(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "a.log")
	w, err := NewRotatingWriter(log, 1, 1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.maxSize = 100
	line := make([]byte, 50)
	for i := range line {
		line[i] = 'x'
	}
	line[49] = '\n'
	_, _ = w.Write(line)
	_, _ = w.Write(line)
	if m, _ := filepath.Glob(log + ".*"); len(m) != 0 {
		t.Fatalf("rotated at exactly maxSize: %v", m)
	}
	_, _ = w.Write(line)
	if m, _ := filepath.Glob(log + ".*"); len(m) != 1 {
		t.Fatalf("want 1 backup after exceeding maxSize, got %v", m)
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = w.Write(line)
			}
		}()
	}
	wg.Wait()
	files, _ := filepath.Glob(log + "*")
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		if fi.Size() > 100 {
			t.Errorf("%s size %d exceeds maxSize", f, fi.Size())
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", f, fi.Mode().Perm())
		}
	}
}
