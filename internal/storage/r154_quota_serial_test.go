package storage

import (
	"bytes"
	"testing"
)

// raceGate runs op in a goroutine from inside a writer's critical section and
// reports whether op completed before the writer finished (true = op was NOT
// serialized with the writer). No sleeps: op either signals lock contention
// (serialized) or completes first.
func raceGate(s *MessageStore, op func()) (hook func(), unserialized func() bool, wait func()) {
	contended := make(chan struct{}, 1)
	s.onLockContended = func() { contended <- struct{}{} }
	done := make(chan struct{})
	var first bool
	hook = func() {
		go func() { op(); close(done) }()
		select {
		case <-contended:
		case <-done:
			first = true
		}
	}
	return hook, func() bool { return first }, func() { <-done }
}

// F6360: plain StoreMessage (SMTP/IMAP APPEND) must not interleave inside the
// quota check-then-store critical section.
func TestR154_PlainStoreSerializesWithQuotaStore(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	const user = "u@example.com"
	hook, unser, wait := raceGate(s, func() { _, _ = s.StoreMessage(user, bytes.Repeat([]byte("b"), 60)) })
	s.afterQuotaCheck = hook
	if _, err := s.StoreMessageWithQuota(user, bytes.Repeat([]byte("a"), 60), 100); err != nil {
		t.Fatal(err)
	}
	wait()
	if unser() {
		t.Fatal("plain StoreMessage ran inside quota critical section")
	}
}

// F6361: DeleteMessage must not remove a blob between a quota-path dedupe hit
// and the caller receiving that ID (the caller would reference a missing body).
func TestR154_DeleteSerializesWithDedupeHit(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	const user = "u@example.com"
	data := bytes.Repeat([]byte("a"), 60)
	id, err := s.StoreMessage(user, data)
	if err != nil {
		t.Fatal(err)
	}
	hook, unser, wait := raceGate(s, func() { _ = s.DeleteMessage(user, id) })
	s.afterQuotaCheck = hook
	got, err := s.StoreMessageWithQuota(user, data, 100)
	if err != nil || got != id {
		t.Fatalf("got %q %v", got, err)
	}
	wait()
	if unser() {
		t.Fatal("DeleteMessage ran between dedupe hit and return")
	}
}
