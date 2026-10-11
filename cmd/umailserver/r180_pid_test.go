//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/umailserver/umailserver/internal/server"
)

// F6580: a PID file naming a live but unrelated process (PID reuse) must be
// treated as stale so stop/restart never signal it.
func TestReadLivePIDRejectsReusedPID(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Skip("no /proc")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "umailserver.pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pf := server.NewPIDFile(dir)
	_, running, stale := readLivePID(pf)
	if running || !stale {
		t.Fatalf("unrelated live pid treated as server: running=%v stale=%v", running, stale)
	}
	if !processIsServer(os.Getpid()) {
		t.Fatal("test binary should look like umailserver")
	}
}
