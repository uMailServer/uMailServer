//go:build linux

package backup

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

func TestBackupFinalizationErrors(t *testing.T) {
	// File-size limits and signal disposition belong to an isolated child.
	if os.Getenv("UMAIL_BACKUP_FINALIZATION_TEST_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBackupFinalizationErrors$", "-test.v")
		cmd.Env = append(os.Environ(), "UMAIL_BACKUP_FINALIZATION_TEST_HELPER=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("backup finalization child: %v\n%s", err, output)
		}
		return
	}

	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "messages", "alice", "Inbox"), 0700); err != nil {
		t.Fatal(err)
	}
	m := NewManager(base, nil, nil)
	dest := filepath.Join(base, "control.tar.gz")
	if err := m.BackupUser("alice", dest, BackupOptions{}); err != nil {
		t.Fatal("CONTROL FAILED", err)
	}
	if _, err := m.Verify(dest); err != nil {
		t.Fatal("CONTROL FAILED", err)
	}
	fmt.Println("CONTROL EXPECTED: valid archive | ACTUAL: valid archive")
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)
	lim := old
	lim.Cur = 16
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Fatal(err)
	}
	err := m.BackupUser("alice", filepath.Join(base, "limited.tar.gz"), BackupOptions{})
	if e := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); e != nil {
		t.Fatal(e)
	}
	fmt.Printf("EXPECTED: archive write error | ACTUAL: %v\n", err)
	if err == nil {
		fmt.Println("PROBLEM CONFIRMED")
		t.FailNow()
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
	for name, fn := range map[string]func(string) error{
		"mailbox": func(dest string) error { return m.BackupMailbox("alice", "Inbox", dest, BackupOptions{}) },
		"full":    func(dest string) error { return m.BackupFull(dest, BackupOptions{}) },
	} {
		dest := filepath.Join(base, name+".tar.gz")
		if err := fn(dest); err != nil {
			t.Fatal(name, "successful backup", err)
		}
		if _, err := m.Verify(dest); err != nil {
			t.Fatal(name, "invalid archive", err)
		}
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
			t.Fatal(err)
		}
		e := fn(filepath.Join(base, name+"-limited.tar.gz"))
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
			t.Fatal(err)
		}
		if e == nil {
			t.Fatal(name, "lost finalization error")
		}
	}
	if err := m.BackupUser("missing", filepath.Join(base, "missing.tar.gz"), BackupOptions{}); err == nil {
		t.Fatal("missing user accepted")
	}
	fmt.Println("FIX VERIFIED")
}
