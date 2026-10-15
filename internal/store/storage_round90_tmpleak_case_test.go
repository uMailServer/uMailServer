//go:build linux

package store

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// audit5724Limit lowers the process-wide RLIMIT_FSIZE for the duration of a
// test and restores it via t.Cleanup. The restore error is checked: a leaked
// limit breaks every later file write in the process.
//
// IMPORTANT: this helper must only run in a CHILD test binary (see
// TestAudit5724Child). The testing framework buffers the per-package testlog
// and flushes it in 4096-byte chunks to an fd it owns; any flush landing
// inside the lowered-limit window fails with EFBIG, the framework sticks on
// that error, and the whole package reports FAIL even though every test
// passed (CI run 38820416307). The child process is exec'd without
// -test.testlogfile, so there is no testlog fd to break.
func audit5724Limit(t *testing.T, limit uint64) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	nl := old
	nl.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &nl); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
			t.Errorf("failed to restore RLIMIT_FSIZE to Cur=%d Max=%d: %v", old.Cur, old.Max, err)
		}
	})
}

func audit5724Tmp(t *testing.T, s *MaildirStore) []string {
	t.Helper()
	p := s.folderPath("example.com", "u", "INBOX")
	ents, _ := os.ReadDir(filepath.Join(p, "tmp"))
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestAudit5724Control(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	if _, err := s.Deliver("example.com", "u", "INBOX", []byte("small")); err != nil {
		t.Fatalf("INVALID: %v", err)
	}
	if n := audit5724Tmp(t, s); len(n) != 0 {
		t.Fatalf("INVALID control: tmp not empty %v", n)
	}
}

// runAudit5724ChildProcess re-executes this test binary with the child-mode
// env set, so the kernel-level EFBIG scenario runs in a process that owns no
// testlog fd (see audit5724Limit).
func runAudit5724ChildProcess(t *testing.T, mode string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestAudit5724Child$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), "STORE_5724_CHILD_MODE="+mode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child %s failed: %v\n%s", mode, err, out)
	}
	if !strings.Contains(string(out), "CHILD5724 "+mode+" OK") {
		t.Fatalf("child %s did not report success:\n%s", mode, out)
	}
}

// TestAudit5724Failure drives the kernel-level partial-write scenario in a
// child process: a write that fails part-way must leave no tmp/ leftovers
// (F5724), with the EFBIG surfaced to the Deliver caller.
func TestAudit5724Failure(t *testing.T) {
	runAudit5724ChildProcess(t, "failure")
}

// TestAudit5724Child is the child half of TestAudit5724Failure and
// TestVerify5724Edges. It is filtered out of normal parent runs via
// -test.run and only executes when STORE_5724_CHILD_MODE is set.
func TestAudit5724Child(t *testing.T) {
	mode := os.Getenv("STORE_5724_CHILD_MODE")
	if mode == "" {
		t.Skip("parent-only run: no STORE_5724_CHILD_MODE")
	}
	s := NewMaildirStore(t.TempDir())
	if mode == "edges" {
		if _, err := s.DeliverWithFlags("example.com", "u", "INBOX", []byte("ok"), "S"); err != nil {
			t.Fatal(err)
		}
		if n := audit5724Tmp(t, s); len(n) != 0 {
			t.Fatalf("success left tmp %v", n)
		}
		audit5724Limit(t, 1024)
		if _, err := s.Deliver("example.com", "u", "INBOX", bytes.Repeat([]byte("y"), 50000)); err == nil {
			t.Fatal("expected failure")
		}
		if n := audit5724Tmp(t, s); len(n) != 0 {
			t.Fatalf("failure left tmp %v", n)
		}
		if used, _, _ := s.Quota("example.com", "u"); used != 2 {
			t.Fatalf("quota %d want 2 (only the good message)", used)
		}
		t.Log("CHILD5724 edges OK")
		return
	}
	big := bytes.Repeat([]byte("x"), 200000)
	audit5724Limit(t, 4096)
	_, err := s.Deliver("example.com", "u", "INBOX", big)
	_, err2 := s.DeliverWithFlags("example.com", "u", "INBOX", big, "S")
	if !errors.Is(err, syscall.EFBIG) || !errors.Is(err2, syscall.EFBIG) {
		t.Fatalf("INVALID: expected EFBIG, got %v / %v", err, err2)
	}
	left := audit5724Tmp(t, s)
	used, _, _ := s.Quota("example.com", "u")
	if len(left) != 0 {
		t.Fatalf("DEFECT F5724: %d partial file(s) left in tmp/", len(left))
	}
	if used != 0 {
		t.Fatalf("DEFECT F5724: quota %d want 0", used)
	}
	t.Log("CHILD5724 failure OK")
}
