package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreVerifyOnlyPreservesTarget(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "backup.tar.gz")
	buildArchive(t, archive, []tarEntry{{name: "alice/INBOX/msg", body: "archived"}})
	controlDir := filepath.Join(root, "control")
	c := NewManager(controlDir, nil, nil)
	if err := c.Restore(archive, RestoreOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	control, e := os.ReadFile(filepath.Join(controlDir, "messages/alice/INBOX/msg"))
	if e != nil || string(control) != "archived" {
		t.Fatal("invalid control")
	}
	targetDir := filepath.Join(root, "target")
	target := filepath.Join(targetDir, "messages/alice/INBOX/msg")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(targetDir, nil, nil)
	if err := m.Restore(archive, RestoreOptions{Overwrite: true, VerifyOnly: true}); err != nil {
		t.Fatal(err)
	}
	actual, e := os.ReadFile(target)
	if e != nil {
		t.Fatal(e)
	}
	if string(actual) != "original" {
		t.Fatalf("VerifyOnly changed target: got %q, want original", actual)
	}
	unused := filepath.Join(root, "unused")
	if err := NewManager(unused, nil, nil).Restore(archive, RestoreOptions{VerifyOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatal("verification created destination")
	}
	bad := filepath.Join(root, "bad.tar.gz")
	if err := os.WriteFile(bad, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(bad, RestoreOptions{VerifyOnly: true, Overwrite: true}); err == nil {
		t.Fatal("invalid archive accepted")
	}
	if err := m.Restore(filepath.Join(root, "missing"), RestoreOptions{VerifyOnly: true}); err == nil {
		t.Fatal("missing archive accepted")
	}
	again, e := os.ReadFile(target)
	if e != nil || string(again) != "original" {
		t.Fatal("failed verify changed target")
	}
}
