//go:build !windows

package cli

// R275 F7490: Backup must skip sockets instead of failing.

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
)

func TestR275F7490BackupSkipsSocket(t *testing.T) {
	d := t.TempDir()
	for _, sub := range []string{"mail", "config"} {
		if err := os.MkdirAll(filepath.Join(d, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(d, "mail", "a.eml"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "config", "c.yaml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(d, "mail", "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	cfg := &config.Config{}
	cfg.Server.DataDir = d
	out := t.TempDir()
	bm := NewBackupManager(cfg)
	if err := bm.Backup(out); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("want 1 backup, got %d", len(entries))
	}
	f, err := os.Open(filepath.Join(out, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gr)
	got := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = true
	}
	for _, want := range []string{"messages/mail/a.eml", "config/config/c.yaml"} {
		if !got[want] {
			t.Errorf("archive lacks %s: %v", want, got)
		}
	}
	for name := range got {
		if filepath.Base(name) == "s.sock" {
			t.Errorf("archive contains non-regular %s", name)
		}
	}
	if err := bm.Verify(filepath.Join(out, entries[0].Name())); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}
