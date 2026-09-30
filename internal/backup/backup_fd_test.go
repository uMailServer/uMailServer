package backup

// Regression test for the addDirToTar file-descriptor accumulation defect:
// the filepath.Walk callback deferred file.Close() per file, so handles
// accumulated until the whole walk finished. Backing up a maildrop with more
// files than the process fd limit (commonly 1024) failed with EMFILE
// ("too many open files") partway through. The fix closes each file
// explicitly after copying, so peak concurrent open fds stay bounded.
//
// The proof backs up a directory with many files while sampling
// /proc/self/fd from a goroutine: pre-fix the peak grows to ~N (N = file
// count); post-fix it stays near the baseline.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// maxBackupTestPeakFDs is the peak concurrent-open-fd budget during a backup.
// The fixed implementation holds a handful of fds; the deferred-close
// implementation accumulates one fd per file, so any N well above this budget
// distinguishes the two.
const maxBackupTestPeakFDs = 1000

const backupTestFileCount = 30000

// openFDCount returns the number of fds the current process holds open.
func openFDCount() (int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func TestAddDirToTarDoesNotAccumulateFileDescriptors(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("/proc/self/fd unavailable on this platform")
	}

	base := t.TempDir()
	// BackupUser requires <dataDir>/messages/<user> to exist.
	userDir := filepath.Join(base, "messages", "anything")
	for i := 0; i < backupTestFileCount; i++ {
		dir := filepath.Join(userDir, fmt.Sprintf("d%02d", i%50))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		name := filepath.Join(dir, fmt.Sprintf("msg-%06d.eml", i))
		if err := os.WriteFile(name, []byte("Subject: x\r\n\r\nbody\r\n"), 0o600); err != nil {
			t.Fatalf("write fixture %d: %v", i, err)
		}
	}

	// Sample the process fd count while the backup walks the tree. The
	// accumulation is monotonic during the walk, so periodic sampling bounds
	// the observed peak only from below — which is enough: pre-fix the peak
	// reaches ~N, post-fix it stays at single digits above baseline.
	var peak atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if n, err := openFDCount(); err == nil {
					for {
						cur := peak.Load()
						if int64(n) <= cur || peak.CompareAndSwap(cur, int64(n)) {
							break
						}
					}
				}
			}
		}
	}()

	m := NewManager(base, nil, nil)
	dest := filepath.Join(t.TempDir(), "backup.tar.gz")
	backupErr := m.BackupUser("anything", dest, BackupOptions{})

	close(stop)
	wg.Wait()

	if backupErr != nil {
		t.Fatalf("FAIL: backup of %d files errored (fd exhaustion?): %v", backupTestFileCount, backupErr)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("FAIL: backup archive missing: %v", err)
	}
	if peak.Load() > int64(maxBackupTestPeakFDs) {
		t.Fatalf("FAIL: backup held %d fds concurrently (budget %d) — file handles accumulate during the walk", peak.Load(), maxBackupTestPeakFDs)
	}
}
