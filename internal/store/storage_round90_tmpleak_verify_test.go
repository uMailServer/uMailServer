//go:build linux

package store

import (
	"bytes"
	"testing"
)

func TestVerify5724Reproduction(t *testing.T) { TestAudit5724Failure(t) }

func TestVerify5724Edges(t *testing.T) {
	s := NewMaildirStore(t.TempDir())
	if _, err := s.DeliverWithFlags("example.com", "u", "INBOX", []byte("ok"), "S"); err != nil {
		t.Fatal(err)
	}
	if n := audit5724Tmp(t, s); len(n) != 0 {
		t.Fatalf("success left tmp %v", n)
	}
	restore := audit5724Limit(t, 1024)
	_, err := s.Deliver("example.com", "u", "INBOX", bytes.Repeat([]byte("y"), 50000))
	restore()
	if err == nil {
		t.Fatal("expected failure")
	}
	if n := audit5724Tmp(t, s); len(n) != 0 {
		t.Fatalf("failure left tmp %v", n)
	}
	if used, _, _ := s.Quota("example.com", "u"); used != 2 {
		t.Fatalf("quota %d want 2 (only the good message)", used)
	}
}
