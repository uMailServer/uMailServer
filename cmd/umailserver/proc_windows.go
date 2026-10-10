//go:build windows

package main

import "os"

// processAlive reports whether pid exists; os.FindProcess fails on Windows
// when the process does not exist.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
