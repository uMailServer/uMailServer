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

// Create creates the PID file with the current process ID.
// It uses O_EXCL to atomically create the file, eliminating the TOCTOU race.
func (p *PIDFile) Create() error {
	pid := os.Getpid()
	data := fmt.Sprintf("%d\n", pid)

	// Attempt atomic creation with O_EXCL
	file, err := os.OpenFile(p.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, writeErr := file.WriteString(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}

	// If file already exists, check for stale PID file
	if os.IsExist(err) {
		existingPID, readErr := p.Read()
		if readErr == nil && existingPID > 0 {
			if isProcessRunning(existingPID) {
				return fmt.Errorf("server already running (PID: %d)", existingPID)
			}
		}
		// Stale PID file, remove and retry atomically
		_ = os.Remove(p.path)
		file, err = os.OpenFile(p.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, writeErr := file.WriteString(data)
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
	}

	return fmt.Errorf("failed to create PID file: %w", err)
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
