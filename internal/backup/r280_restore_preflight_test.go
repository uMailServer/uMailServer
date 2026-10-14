package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func r280Archive(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o600, Size: 1, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// F7540: "." names the message store root, not a user.
func TestR280DotUserRejected(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil, nil)
	if err := os.MkdirAll(filepath.Join(dir, "messages", "alice", "cur"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := m.BackupUser(".", filepath.Join(dir, "dot.tar.gz"), BackupOptions{}); err == nil {
		t.Fatal("BackupUser(\".\") must be rejected")
	}
	if err := m.BackupMailbox("alice", ".", filepath.Join(dir, "dot2.tar.gz"), BackupOptions{}); err == nil {
		t.Fatal("BackupMailbox mailbox \".\" must be rejected")
	}
	if _, err := m.ListUserBackups("."); err == nil {
		t.Fatal("ListUserBackups(\".\") must be rejected")
	}
	ok := filepath.Join(dir, "ok.tar.gz")
	r280Archive(t, ok, "alice/cur/a")
	if err := m.Restore(ok, RestoreOptions{Mode: RestoreModeDifferent, TargetUser: "."}); err == nil {
		t.Fatal("restore target \".\" must be rejected")
	}
	if err := m.BackupUser("alice", filepath.Join(dir, "ok2.tar.gz"), BackupOptions{}); err != nil {
		t.Fatalf("regular user backup failed: %v", err)
	}
}

// F7541: an invalid later entry must fail the restore before any member is
// extracted, not leave a partially restored target.
func TestR280RestoreRejectsBeforeExtracting(t *testing.T) {
	cases := []struct {
		name string
		opts RestoreOptions
		ents []string
	}{
		{"multi-user", RestoreOptions{Mode: RestoreModeDifferent, TargetUser: "carol"}, []string{"alice/cur/a", "bob/cur/b"}},
		{"traversal", RestoreOptions{Mode: RestoreModeOverwrite}, []string{"alice/cur/a", "../escape"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			m := NewManager(dir, nil, nil)
			arc := filepath.Join(dir, "a.tar.gz")
			r280Archive(t, arc, c.ents...)
			if err := m.Restore(arc, c.opts); err == nil {
				t.Fatal("expected restore error")
			}
			found := ""
			_ = filepath.Walk(filepath.Join(dir, "messages"), func(p string, i os.FileInfo, err error) error {
				if err == nil && i.Mode().IsRegular() {
					found = p
				}
				return nil
			})
			if found != "" {
				t.Fatalf("partial restore left %s", found)
			}
		})
	}
}
