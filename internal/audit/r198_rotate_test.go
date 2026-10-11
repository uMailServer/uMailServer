package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// F6750: retention must count/age out rotated files even when the configured
// path contains glob metacharacters.
func TestRotatingWriterCleanupGlobMetacharPath_F6750(t *testing.T) {
	for _, name := range []string{"audit.log", "au[dit].log"} {
		dir := t.TempDir()
		path := filepath.Join(dir, name)
		w := &rotatingWriter{filename: path, maxBackups: 1}
		for _, s := range []string{"a", "b", "c"} {
			p := path + ".20200101-00000" + s
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			mt := int64(1000)
			if s == "b" {
				mt = 2000
			} else if s == "c" {
				mt = 3000
			}
			_ = os.Chtimes(p, timeUnix(mt), timeUnix(mt))
		}
		w.cleanup()
		entries, _ := os.ReadDir(dir)
		if len(entries) != 1 {
			t.Errorf("%s: want 1 backup kept, got %d", name, len(entries))
		}
	}
}

func timeUnix(s int64) time.Time { return time.Unix(s, 0) }
