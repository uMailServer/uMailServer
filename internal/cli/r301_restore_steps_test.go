//go:build !windows

package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func r301Capture(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

// r301Restore backs up a data directory holding the given sub-directories
// (each with one file), restores it from an unrelated working directory and
// returns the command output and the restore_temp path.
func r301Restore(t *testing.T, dirs ...string) (string, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "data")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(data, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, d, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{}
	cfg.Server.DataDir = data
	bm := NewBackupManager(cfg)
	out := t.TempDir()
	if err := bm.Backup(out); err != nil {
		t.Fatalf("backup: %v", err)
	}
	files, _ := os.ReadDir(out)
	old, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	text := r301Capture(t, func() error { return bm.Restore(filepath.Join(out, files[0].Name())) })
	return text, filepath.Join(base, "restore_temp")
}

// F7760: the operator is told the real extraction path, and only the parts the
// archive holds.
func TestRestoreNamesTheRealPathAndOnlyPresentParts(t *testing.T) {
	text, want := r301Restore(t, "mail")
	if !strings.Contains(text, "Backup extracted to "+want) {
		t.Errorf("output does not name %s:\n%s", want, text)
	}
	if strings.Contains(text, "/config") {
		t.Errorf("asks for a config directory the archive does not have:\n%s", text)
	}
	if !strings.Contains(text, "2. Copy "+filepath.Join(want, "messages")) || !strings.Contains(text, "3. Start uMailServer") {
		t.Errorf("steps not numbered over the present parts:\n%s", text)
	}
}

// Control: an archive that does hold a config directory lists that step too.
func TestRestoreListsTheConfigStepWhenArchived(t *testing.T) {
	text, want := r301Restore(t, "mail", "config")
	if !strings.Contains(text, "2. Copy "+filepath.Join(want, "config")) || !strings.Contains(text, "3. Copy "+filepath.Join(want, "messages")) {
		t.Errorf("config step missing:\n%s", text)
	}
}
