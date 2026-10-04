package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func unchangedWatcherFixture(t *testing.T) *Watcher {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if e := os.WriteFile(p, []byte("server:\n  hostname: test.example.com\n"), 0600); e != nil {
		t.Fatal(e)
	}
	w := NewWatcher(p, nil, nil)
	h, e := w.fileHash()
	if e != nil {
		t.Fatal(e)
	}
	w.lastHash = h
	return w
}
func TestWatcherSerialUnchangedChecks(t *testing.T) {
	w := unchangedWatcherFixture(t)
	for j := 0; j < 20; j++ {
		if w.check() {
			t.Fatal("unchanged content changed")
		}
	}
	t.Log("CONTROL EXPECTED: serial unchanged checks race-free ACTUAL: pass")
}
func TestWatcherConcurrentUnchangedChecks(t *testing.T) {
	w := unchangedWatcherFixture(t)
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(8)
	done.Add(8)
	for j := 0; j < 8; j++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			for k := 0; k < 100; k++ {
				if w.check() {
					t.Error("unchanged content changed")
				}
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	t.Log("EXPECTED: overlapping unchanged checks race-free ACTUAL: race detector result")
}
func TestWatcherTrackingStateAndRemovedFile(t *testing.T) {
	w := unchangedWatcherFixture(t)
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-start
		w.mutex.Lock()
		w.lastHash = "changed"
		w.lastModTime = time.Time{}
		w.mutex.Unlock()
		close(done)
	}()
	close(start)
	<-done
	if !w.check() {
		t.Fatal("changed hash not detected")
	}
	h, e := w.fileHash()
	if e != nil {
		t.Fatal(e)
	}
	w.mutex.Lock()
	w.lastHash = h
	w.mutex.Unlock()
	if w.check() {
		t.Fatal("restored hash changed")
	}
	if e := os.Remove(w.path); e != nil {
		t.Fatal(e)
	}
	if w.check() {
		t.Fatal("removed file changed")
	}
}
