package db

import (
	"testing"
)

// =======================================================================
// Open (81.8%) and initBuckets (83.3%) - cover additional error paths.
//
// The uncovered branches in Open are the initBuckets failure + bolt.Close() path.
// The uncovered branch in initBuckets is the CreateBucketIfNotExists error path.
// Both are triggered by a closed/invalid bbolt database.
// =======================================================================

// TestOpen_InitBucketsFailureOnClosedBolt tests Open's error handling when
// the underlying bolt database has issues during bucket creation.
// This is tricky to test directly since Open always calls initBuckets.
// Instead we test that initBuckets fails properly on a closed database.
func TestOpen_InitBucketsFailureOnClosedBolt_Cov3(t *testing.T) {
	database := helperDB(t)
	database.Close()

	err := database.initBuckets()
	if err == nil {
		t.Error("Expected error calling initBuckets on closed database")
	}
}

// TestOpen_WithEmptyPath tests Open with a path that requires directory creation.
func TestOpen_WithEmptyPath_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	// Open with a path that includes multiple levels of directories to create
	dbPath := tmpDir + "/a/b/c/d/e/f/test.db"
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open with deep path: %v", err)
	}
	database.Close()
}

// TestOpen_ReopenMultipleTimes tests that opening and closing the database
// multiple times works correctly.
func TestOpen_ReopenMultipleTimes_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/multi.db"

	for i := 0; i < 3; i++ {
		database, err := Open(dbPath)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		// Verify buckets exist
		if err := database.initBuckets(); err != nil {
			t.Errorf("initBuckets %d: %v", i, err)
		}
		database.Close()
	}
}

// TestOpen_MkdirAllAlreadyExists tests that MkdirAll works when directory already exists.
func TestOpen_MkdirAllAlreadyExists_Cov3(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	database.Close()

	// Open again - directory already exists
	database2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Second Open: %v", err)
	}
	database2.Close()
}
