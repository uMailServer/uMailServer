//go:build linux

package storage

import (
	"bytes"
	"syscall"
	"testing"
)

// TestStoreMessagePartialWriteNotPublished is the regression test for F5096:
// StoreMessage wrote straight to the content-addressed final path, so a write
// that failed part-way (disk full, quota, crash) left a truncated body there.
// The next delivery of the same content hit the "already exists" dedup branch,
// reported success, and the message stayed truncated forever.
func TestStoreMessagePartialWriteNotPublished(t *testing.T) {
	ms, err := NewMessageStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("Subject: hello\r\n\r\nbody line of a real message\r\n"), 4096)

	// RLIMIT_FSIZE makes writes past 4 KiB fail with EFBIG (Go ignores SIGXFSZ).
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skipf("getrlimit: %v", err)
	}
	lim := old
	lim.Cur = 4096
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Skipf("setrlimit: %v", err)
	}
	_, firstErr := ms.StoreMessage("u@example.com", data)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatalf("restore rlimit: %v", err)
	}
	if firstErr == nil {
		t.Fatal("store under a 4 KiB file-size limit should fail")
	}

	id, err := ms.StoreMessage("u@example.com", data)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	got, err := ms.ReadMessage("u@example.com", id)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored body is %d/%d bytes (err=%v)", len(got), len(data), err)
	}
}
