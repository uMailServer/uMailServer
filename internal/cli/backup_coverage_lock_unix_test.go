//go:build unix

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	bolterrors "go.etcd.io/bbolt/errors"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

func r74Site(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Server.DataDir = filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(cfg.Server.DataDir, "mail"), 0o750); err != nil {
		t.Fatal(err)
	}
	adb, err := db.Open(filepath.Join(cfg.Server.DataDir, "umailserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := adb.Close(); err != nil {
		t.Fatal(err)
	}
	sdb, err := storage.OpenDatabase(filepath.Join(cfg.Server.DataDir, "mail", "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sdb.Close(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestBackupRefusesLiveDatabase (F5551): a running server holds its bbolt
// databases under an exclusive lock and commits at any time; a plain copy can
// pair an old meta page with reused pages. Backup must refuse instead of
// archiving the bytes.
func TestBackupRefusesLiveDatabase(t *testing.T) {
	for _, held := range []string{"umailserver.db", "mail/mail.db"} {
		t.Run(held, func(t *testing.T) {
			cfg := r74Site(t)
			p := filepath.Join(cfg.Server.DataDir, held)
			var closeFn func() error
			if held == "umailserver.db" {
				d, err := db.Open(p)
				if err != nil {
					t.Fatal(err)
				}
				closeFn = d.Close
			} else {
				d, err := storage.OpenDatabase(p)
				if err != nil {
					t.Fatal(err)
				}
				closeFn = d.Close
			}
			out := filepath.Join(t.TempDir(), "out")
			err := NewBackupManager(cfg).Backup(out)
			if cerr := closeFn(); cerr != nil {
				t.Fatal(cerr)
			}
			if err == nil {
				t.Fatalf("Backup succeeded while %s was held open read-write by another handle", held)
			}
			if entries, _ := os.ReadDir(out); len(entries) != 0 {
				t.Fatalf("archive written despite refusal: %v", entries)
			}
			// Once the holder is gone the backup works.
			if err := NewBackupManager(cfg).Backup(out); err != nil {
				t.Fatalf("Backup after release: %v", err)
			}
		})
	}
}

// TestBackupHoldsDatabaseLock (F5551): while Backup runs, a server-style
// read-write open must not succeed. Backup is parked on a FIFO under config/,
// which it reads after taking the locks; opening the FIFO for writing returns
// only once Backup opened it, so the order is gated without sleeps.
func TestBackupHoldsDatabaseLock(t *testing.T) {
	cfg := r74Site(t)
	if err := os.MkdirAll(filepath.Join(cfg.Server.DataDir, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(cfg.Server.DataDir, "config", "gate")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- NewBackupManager(cfg).Backup(filepath.Join(t.TempDir(), "out")) }()
	w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv, srvErr := db.Open(filepath.Join(cfg.Server.DataDir, "umailserver.db"))
	if srv != nil {
		_ = srv.Close()
	}
	if cerr := w.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if err := <-done; err != nil {
		t.Fatalf("gated Backup: %v", err)
	}
	if !errors.Is(srvErr, bolterrors.ErrTimeout) {
		t.Fatalf("read-write open during Backup: err=%v, want bbolt timeout", srvErr)
	}
}
