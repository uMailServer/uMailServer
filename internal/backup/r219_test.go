package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func readArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}

// F6960: a live Maildir changes while it is archived. A file renamed or
// delivered-and-moved between the directory listing and the open must not
// abort the whole backup, and a file that grew after it was listed must be
// archived with the size it has when it is read (previously
// tar.ErrWriteTooLong / ENOENT failed the backup).
func TestBackupSurvivesConcurrentMaildirChanges(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil, nil)
	cur := filepath.Join(dir, "messages", "alice", "cur")
	if err := os.MkdirAll(cur, 0o750); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(cur, "gone")
	grow := filepath.Join(cur, "grow")
	stay := filepath.Join(cur, "stay")
	for _, p := range []string{gone, grow, stay} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archiveFileHook = func(p string) {
		switch p {
		case gone:
			_ = os.Remove(gone)
		case grow:
			_ = os.WriteFile(grow, []byte("xxxxxxxx"), 0o600)
		}
	}
	t.Cleanup(func() { archiveFileHook = nil })

	dest := filepath.Join(dir, "out.tar.gz")
	if err := m.BackupUser("alice", dest, BackupOptions{}); err != nil {
		t.Fatalf("backup aborted by concurrent change: %v", err)
	}
	got := readArchive(t, dest)
	if got["alice/cur/grow"] != "xxxxxxxx" || got["alice/cur/stay"] != "x" {
		t.Fatalf("unexpected archive content: %v", got)
	}
	if _, ok := got["alice/cur/gone"]; ok {
		t.Fatal("vanished file must be skipped")
	}
}
