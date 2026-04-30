package db

import (
	"testing"
	"time"
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

// TestEnqueueWithLimit_Basic tests EnqueueWithLimit success and limit enforcement.
func TestEnqueueWithLimit_Basic_Cov3(t *testing.T) {
	database := helperDB(t)
	defer database.Close()

	entry := &QueueEntry{
		ID:        "q1",
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Status:    "pending",
		CreatedAt: time.Now(),
		NextRetry: time.Now(),
	}

	// Enqueue should succeed
	if err := database.EnqueueWithLimit(entry, 10); err != nil {
		t.Fatalf("EnqueueWithLimit failed: %v", err)
	}

	// Verify entry exists
	got, err := database.GetQueueEntry("q1")
	if err != nil {
		t.Fatalf("GetQueueEntry failed: %v", err)
	}
	if got.ID != "q1" {
		t.Errorf("expected ID q1, got %s", got.ID)
	}
}

// TestEnqueueWithLimit_QueueFull tests EnqueueWithLimit when max size is reached.
func TestEnqueueWithLimit_QueueFull_Cov3(t *testing.T) {
	database := helperDB(t)
	defer database.Close()

	// Enqueue one entry with maxSize=1
	entry1 := &QueueEntry{
		ID:        "q1",
		From:      "a@example.com",
		To:        []string{"b@example.com"},
		Status:    "pending",
		CreatedAt: time.Now(),
		NextRetry: time.Now(),
	}
	if err := database.EnqueueWithLimit(entry1, 1); err != nil {
		t.Fatalf("first EnqueueWithLimit failed: %v", err)
	}

	// Second enqueue should fail because queue is full
	entry2 := &QueueEntry{
		ID:        "q2",
		From:      "c@example.com",
		To:        []string{"d@example.com"},
		Status:    "pending",
		CreatedAt: time.Now(),
		NextRetry: time.Now(),
	}
	if err := database.EnqueueWithLimit(entry2, 1); err == nil {
		t.Error("expected error when queue is full")
	}
}

// TestEnqueueWithLimit_SetsCreatedAt tests that EnqueueWithLimit sets CreatedAt if zero.
func TestEnqueueWithLimit_SetsCreatedAt_Cov3(t *testing.T) {
	database := helperDB(t)
	defer database.Close()

	entry := &QueueEntry{
		ID:     "q3",
		From:   "sender@example.com",
		To:     []string{"recipient@example.com"},
		Status: "pending",
		// CreatedAt intentionally left zero
		NextRetry: time.Now(),
	}

	if err := database.EnqueueWithLimit(entry, 10); err != nil {
		t.Fatalf("EnqueueWithLimit failed: %v", err)
	}

	if entry.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}
