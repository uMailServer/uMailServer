//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestWatcherReloadKeepsMidLoadEditPending is a regression test for F5291:
// an edit that lands after reload's Load has read the file must not be
// recorded as applied. TLS cert/key FIFOs block Validate until the test
// opens their write ends, giving deterministic ordering without sleeps.
func TestWatcherReloadKeepsMidLoadEditPending(t *testing.T) {
	d := t.TempDir()
	cfgPath := filepath.Join(d, "config.yaml")
	certFIFO, keyFIFO := filepath.Join(d, "cert.fifo"), filepath.Join(d, "key.fifo")
	for _, p := range []string{certFIFO, keyFIFO} {
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Skipf("mkfifo unavailable: %v", err)
		}
	}
	write := func(host string) {
		body := "server:\n  hostname: " + host + "\n  data_dir: " + filepath.Join(d, "data") +
			"\ntls:\n  cert_file: " + certFIFO + "\n  key_file: " + keyFIFO + "\n"
		if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gate := func(p string) {
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	reload := func(w *Watcher, mid func()) {
		done := make(chan struct{})
		go func() { w.reload(); close(done) }()
		gate(certFIFO) // Load has already read the file once this returns
		mid()
		gate(keyFIFO)
		<-done
	}

	write("a.example.com")
	w := NewWatcher(cfgPath, nil, nil)
	w.lastHash = "stale"
	reload(w, func() { write("b.example.com") })
	if got := w.GetCurrentConfig().Server.Hostname; got != "a.example.com" {
		t.Fatalf("loaded %q, want a.example.com", got)
	}
	if !w.check() {
		t.Fatal("edit made during reload was recorded as applied and will never be loaded")
	}

	reload(w, func() {})
	if got := w.GetCurrentConfig().Server.Hostname; got != "b.example.com" {
		t.Fatalf("follow-up reload loaded %q, want b.example.com", got)
	}
	if w.check() {
		t.Fatal("unchanged file reported as changed after reload")
	}
}
