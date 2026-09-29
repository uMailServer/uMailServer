package backup

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// tarEntry describes one member of a crafted backup archive.
type tarEntry struct {
	name string
	body string
	dir  bool
}

// buildArchive writes a real .tar.gz consumed by Manager.Restore.
func buildArchive(t *testing.T, path string, entries []tarEntry) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o600}
		if e.dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Size = 0
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", e.name, err)
		}
		if !e.dir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %q: %v", e.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}

// TestRestore_ConfinesEntriesToTargetDir is the regression for a crafted
// archive: a tar member whose name resolves outside the target messages
// directory must be refused, not written. Without the guard a member named
// "../escape.txt" is created outside targetDir, letting a backup overwrite
// arbitrary files. The CLI restore (internal/cli/backup.go) enforces the
// same rule, which is the contract this package is expected to honour.
func TestRestore_ConfinesEntriesToTargetDir(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		// escaped is the path that must NOT exist after the restore.
		escaped string
	}{
		{
			name:    "parent traversal",
			entries: []tarEntry{{name: "../escape.txt", body: "pwned"}},
			escaped: "escape.txt",
		},
		{
			name:    "nested parent traversal",
			entries: []tarEntry{{name: "a/b/../../../escape.txt", body: "pwned"}},
			escaped: "escape.txt",
		},
		{
			name:    "traversal to absolute path",
			entries: []tarEntry{{name: "/tmp/umail-absolute-escape.txt", body: "pwned"}},
			escaped: "..",
		},
		{
			name:    "traversing directory entry",
			entries: []tarEntry{{name: "../escapedir", dir: true}, {name: "../escapedir/f.txt", body: "pwned"}},
			escaped: "escapedir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "evil.tar.gz")
			buildArchive(t, archive, tt.entries)

			m := NewManager(dir, nil, nil)
			// Restore may legitimately report the archive as invalid. Accept
			// either outcome, but assert nothing landed outside targetDir.
			_ = m.Restore(archive, RestoreOptions{Overwrite: true})

			// Sweep the whole sandbox (and /tmp for the absolute case) for any
			// file the archive managed to place outside dataDir/messages.
			if escaped := filepath.Join(dir, tt.escaped); tt.escaped != ".." {
				if _, err := os.Stat(escaped); err == nil {
					t.Fatalf("FAIL: entry %q escaped the target messages directory and "+
						"was written to %s", tt.entries[0].name, escaped)
				}
			}
			if _, err := os.Stat("/tmp/umail-absolute-escape.txt"); err == nil {
				_ = os.Remove("/tmp/umail-absolute-escape.txt")
				t.Fatalf("FAIL: entry %q escaped to an absolute path", tt.entries[0].name)
			}
		})
	}
}

// TestRestore_ExtractsEntriesInsideTargetDir is the control: ordinary members
// must still be restored, so the guard cannot be satisfied by refusing all
// archives. It passes before and after the fix.
func TestRestore_ExtractsEntriesInsideTargetDir(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "good.tar.gz")
	buildArchive(t, archive, []tarEntry{
		{name: "INBOX/msg1", body: "hello world"},
		{name: "INBOX/cur/msg2", body: "second"},
	})

	m := NewManager(dir, nil, nil)
	if err := m.Restore(archive, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("Restore of an ordinary archive returned %v", err)
	}

	for rel, want := range map[string]string{
		"INBOX/msg1":     "hello world",
		"INBOX/cur/msg2": "second",
	} {
		got, err := os.ReadFile(filepath.Join(dir, "messages", rel))
		if err != nil {
			t.Fatalf("entry %q was not restored: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("entry %q = %q, want %q", rel, got, want)
		}
	}
}

// TestRestore_NormalisesDotSegmentsWithinTarget covers the boundary the guard
// must not break: a member that stays inside targetDir after cleaning
// ("INBOX/./msg") is legitimate and must still be extracted.
func TestRestore_NormalisesDotSegmentsWithinTarget(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "dot.tar.gz")
	buildArchive(t, archive, []tarEntry{{name: "INBOX/./msg", body: "inside"}})

	m := NewManager(dir, nil, nil)
	if err := m.Restore(archive, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatalf("Restore of an in-tree member returned %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "messages", "INBOX", "msg"))
	if err != nil {
		t.Fatalf("in-tree member was not restored: %v", err)
	}
	if string(got) != "inside" {
		t.Fatalf("restored body = %q, want %q", got, "inside")
	}
}
