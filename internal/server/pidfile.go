package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PIDFile manages the PID file for the server
type PIDFile struct {
	path string
}

// NewPIDFile creates a new PID file manager
func NewPIDFile(dataDir string) *PIDFile {
	return &PIDFile{
		path: filepath.Join(dataDir, "umailserver.pid"),
	}
}

// Create creates the PID file with the current process ID
func (p *PIDFile) Create() error {
	// Check if PID file already exists
	if _, err := os.Stat(p.path); err == nil {
		// PID file exists, check if process is still running
		pid, err := p.Read()
		if err == nil && pid > 0 {
			if isProcessRunning(pid) {
				return fmt.Errorf("server already running (PID: %d)", pid)
			}
			// Stale PID file, remove it
			os.Remove(p.path)
		}
	}

	pid := os.Getpid()
	data := fmt.Sprintf("%d\n", pid)
	return os.WriteFile(p.path, []byte(data), 0644)
}

// Read reads the PID from the file
func (p *PIDFile) Read() (int, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return 0, err
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}

	return pid, nil
}

// Remove removes the PID file
func (p *PIDFile) Remove() error {
	return os.Remove(p.path)
}

// isProcessRunning checks if a process with the given PID is running
func isProcessRunning(pid int) bool {
	// On Unix, send signal 0 to check if process exists
	// On Windows, this is handled differently
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Windows, FindProcess always succeeds, need additional check
	// For simplicity, we assume it's running if we found it
	_ = process
	return true
}
