//go:build linux

package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

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
		// The restore error must never be silently discarded: RLIMIT_FSIZE
		// is process-wide, and a leaked 4096-byte limit breaks every later
		// file write in this process — including the testing framework's
		// exit-time testlog write, which surfaced as a package FAIL despite
		// all tests passing (round 26; CI run 38816451227).
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

// FAILURE: a write that fails part-way leaves its partial file in tmp/, where
// Quota counts it forever (nothing cleans tmp/).
func TestAudit5724Failure(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	big := bytes.Repeat([]byte("x"), 200000)
	audit5724Limit(t, 4096)
	_, err := s.Deliver("example.com", "u", "INBOX", big)
	_, err2 := s.DeliverWithFlags("example.com", "u", "INBOX", big, "S")
	if !errors.Is(err, syscall.EFBIG) || !errors.Is(err2, syscall.EFBIG) {
		t.Fatalf("INVALID: expected EFBIG, got %v / %v", err, err2)
	}
	left := audit5724Tmp(t, s)
	used, _, _ := s.Quota("example.com", "u")
	t.Logf("EXPECTED: no tmp leftovers, quota 0 ACTUAL: %v quota=%d", left, used)
	if len(left) != 0 {
		t.Fatalf("DEFECT F5724: %d partial file(s) left in tmp/", len(left))
	}
}
