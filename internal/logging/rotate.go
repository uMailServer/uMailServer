package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RotatingWriter is an io.WriteCloser that writes to a log file and rotates
// it when it reaches a maximum size. Old log files are retained up to a
// maximum count and age.
type RotatingWriter struct {
	filename   string
	maxSize    int64 // bytes
	maxBackups int
	maxAge     time.Duration

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewRotatingWriter creates a new RotatingWriter.
// maxSizeMB: maximum file size in megabytes before rotation
// maxBackups: maximum number of old files to keep
// maxAgeDays: maximum age of old files in days
func NewRotatingWriter(filename string, maxSizeMB, maxBackups, maxAgeDays int) (*RotatingWriter, error) {
	w := &RotatingWriter{
		filename:   filename,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
		maxAge:     time.Duration(maxAgeDays) * 24 * time.Hour,
	}

	// Ensure log directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open or create the log file
	if err := w.open(); err != nil {
		return nil, err
	}

	// Clean up old backups on startup
	w.cleanup()

	return w, nil
}

// open opens the log file for appending, creating it if necessary.
func (w *RotatingWriter) open() error {
	info, err := os.Stat(w.filename)
	if err == nil {
		w.size = info.Size()
	} else {
		w.size = 0
	}

	f, err := os.OpenFile(w.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	w.file = f
	return nil
}

// Write implements io.Writer.
func (w *RotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if rotation is needed
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close implements io.Closer.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// rotate closes the current file and renames it to a backup name.
func (w *RotatingWriter) rotate() error {
	// Close current file
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	// Rename current file to backup name with timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s.%s", w.filename, timestamp)

	if _, err := os.Stat(w.filename); err == nil {
		if err := os.Rename(w.filename, backupName); err != nil {
			return fmt.Errorf("failed to rotate log file: %w", err)
		}
	}

	// Open new file
	if err := w.open(); err != nil {
		return err
	}

	// Clean up old backups in background
	go w.cleanup()

	return nil
}

// cleanup removes old backup files based on maxBackups and maxAge.
func (w *RotatingWriter) cleanup() {
	if w.maxBackups <= 0 && w.maxAge <= 0 {
		return
	}

	// Find all backup files
	pattern := w.filename + ".*"
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}

	if len(matches) == 0 {
		return
	}

	// Get file info for sorting
	type fileInfo struct {
		path string
		info os.FileInfo
	}

	var files []fileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: match, info: info})
	}

	// Sort by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].info.ModTime().Before(files[j].info.ModTime())
	})

	now := time.Now()
	var kept int

	// Process from newest to oldest for maxBackups logic
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		shouldDelete := false

		// Check age
		if w.maxAge > 0 && now.Sub(f.info.ModTime()) > w.maxAge {
			shouldDelete = true
		}

		// Check backup count (keep newest maxBackups)
		if !shouldDelete && w.maxBackups > 0 {
			if kept >= w.maxBackups {
				shouldDelete = true
			} else {
				kept++
			}
		}

		if shouldDelete {
			os.Remove(f.path)
		}
	}
}

// GetLogWriter returns an io.WriteCloser for the configured output.
// If filename is "stdout", "stderr", or empty, it returns the respective os.File.
// Otherwise, it returns a RotatingWriter with the specified settings.
func GetLogWriter(output string, maxSizeMB, maxBackups, maxAgeDays int) (io.WriteCloser, error) {
	switch strings.ToLower(output) {
	case "stdout", "":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	default:
		return NewRotatingWriter(output, maxSizeMB, maxBackups, maxAgeDays)
	}
}
