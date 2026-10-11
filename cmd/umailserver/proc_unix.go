//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// processAlive reports whether pid exists (signal 0 probe).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	// EPERM means the process exists but belongs to someone else.
	return err == nil || err == syscall.EPERM
}

// processIsServer reports whether pid looks like a umailserver process, so a
// PID file left behind by a crashed server whose PID was reused by an
// unrelated process is never signalled (F6580). Where /proc is unavailable
// the check cannot be made and the PID is trusted.
func processIsServer(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(data) == 0 {
		// Unreadable (no /proc) or empty (kernel thread / zombie): only a
		// readable, non-matching cmdline proves a different program.
		_, statErr := os.Stat("/proc/self")
		return statErr != nil
	}
	argv0 := data
	if i := bytes.IndexByte(data, 0); i >= 0 {
		argv0 = data[:i]
	}
	name := filepath.Base(string(argv0))
	if strings.Contains(name, "umailserver") {
		return true
	}
	if self, err := os.Executable(); err == nil && name == filepath.Base(self) {
		return true
	}
	return false
}
