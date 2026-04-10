package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- rotatingWriter coverage ---

func TestRotatingWriter_Rotate(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	// Create with small maxSize to trigger rotation
	r, err := newRotatingWriter(logPath, 1, 3, 1)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}
	defer r.Close()

	// Write enough to exceed 1MB maxSize
	data := make([]byte, 1024*1024+100)
	for i := range data {
		data[i] = 'x'
	}

	_, err = r.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	r.Close()

	// Check that backup file was created
	files, _ := filepath.Glob(logPath + ".*")
	if len(files) == 0 {
		t.Error("Expected rotation to create backup file")
	}
}

func TestRotatingWriter_Cleanup(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	r, err := newRotatingWriter(logPath, 1, 2, 0)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}
	defer r.Close()

	// Create some backup files manually with timestamps older than now
	for i := 0; i < 5; i++ {
		backupPath := logPath + ".20240101-00000" + string(rune('0'+i))
		os.WriteFile(backupPath, []byte("old log"), 0644)
		// Set old mod time
		os.Chtimes(backupPath, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	}

	// Trigger cleanup - removes oldest when maxBackups is set
	r.cleanup()

	// Cleanup removes one oldest file at a time when maxBackups exceeded
	// Initial 5 files - should now be 4 after one cleanup pass
	files, _ := filepath.Glob(logPath + ".*")
	if len(files) != 4 {
		t.Errorf("Expected 4 backups after cleanup, got %d", len(files))
	}
}

func TestRotatingWriter_CleanupMaxBackupsZero(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	// maxBackups=0 and maxAge=0 means no cleanup
	r, err := newRotatingWriter(logPath, 1, 0, 0)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}
	defer r.Close()

	// Should not panic
	r.cleanup()
}

func TestRotatingWriter_WriteMultipleRotations(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	// Create with 1MB max size
	r, err := newRotatingWriter(logPath, 1, 3, 1)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}

	// Write data to trigger multiple rotations
	for i := 0; i < 3; i++ {
		data := make([]byte, 1024*1024+100)
		for j := range data {
			data[j] = byte('A' + i)
		}
		_, err = r.Write(data)
		if err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}
	r.Close()

	// Should have multiple backup files due to 3 full rotations
	files, _ := filepath.Glob(logPath + ".*")
	if len(files) < 1 {
		t.Errorf("Expected at least 1 backup file, got %d", len(files))
	}
}
