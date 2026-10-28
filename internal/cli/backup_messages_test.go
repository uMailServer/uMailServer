package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

// A CLI user cannot call SetPassword(); the error must name the flag, and a
// wrong password must not be announced as a successful decryption first.
func TestRestoreEncryptedWithoutPasswordNamesTheFlag(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{DataDir: t.TempDir()}}
	src := NewBackupManager(cfg)
	src.SetPassword("correct-horse-battery")
	out := t.TempDir()
	if err := src.Backup(out); err != nil {
		t.Fatalf("backup: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(out, "*.enc"))
	if len(files) != 1 {
		t.Fatalf("encrypted backup not found in %s", out)
	}

	err := NewBackupManager(cfg).Restore(files[0])
	if err == nil || !strings.Contains(err.Error(), "--password") || strings.Contains(err.Error(), "SetPassword") {
		t.Fatalf("error should point at --password, got %v", err)
	}
	_ = os.RemoveAll("restore_temp")
}
