//go:build !windows

package main

import (
	"os"
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
