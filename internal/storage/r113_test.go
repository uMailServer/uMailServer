package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// F5950: a second Open must wait for the file lock, not fail instantly.
func TestF5950_OpenWaitsForLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	a, err := OpenDatabase(p)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(300 * time.Millisecond); _ = a.Close() }()
	b, err := OpenDatabase(p)
	if err != nil {
		t.Fatalf("second open failed instead of waiting: %v", err)
	}
	_ = b.Close()
}

// F5951: store racing with delete of identical content must never error.
func TestF5951_StoreVsDeleteRace(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	data := []byte("same content")
	id, _ := s.StoreMessage("u", data)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.DeleteMessage("u", id)
			}
		}
	}()
	for i := 0; i < 3000; i++ {
		if _, err := s.StoreMessage("u", data); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// F5952: orphaned temp files from a crashed store are reaped.
func TestF5952_StaleTempReaped(t *testing.T) {
	s, _ := NewMessageStore(t.TempDir())
	data := []byte("hello")
	id, _ := s.StoreMessage("u", data)
	dir := filepath.Join(s.basePath, "u", id[:2], id[2:4])
	stale := filepath.Join(dir, ".tmp-"+id+"-old")
	fresh := filepath.Join(dir, ".tmp-"+id+"-new")
	for _, f := range []string{stale, fresh} {
		_ = os.WriteFile(f, []byte("x"), 0o600)
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(stale, old, old)
	if _, err := s.StoreMessage("u", data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale temp not removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh temp must be kept")
	}
}
