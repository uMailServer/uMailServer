package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// --- Open error paths ---

func TestOpenMkdirFail(t *testing.T) {
	// On Windows, creating a directory inside a file should fail.
	// Create a file, then try to Open a database where the parent is that file.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "afile")
	// Create a regular file
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}
	// Try to open a db under that file (e.g. afile/test.db)
	dbPath := filepath.Join(filePath, "test.db")
	_, err := Open(dbPath)
	if err == nil {
		t.Error("expected error when opening database with invalid directory path")
	}
}

func TestOpenBboltFail(t *testing.T) {
	// Open a database at a path where bbolt cannot open (e.g. path is a directory)
	tmpDir := t.TempDir()
	// Pass the directory itself as the db path; bbolt should fail to open a directory
	_, err := Open(tmpDir)
	if err == nil {
		t.Error("expected error when opening database at directory path")
	}
}

// --- Bucket not found error paths ---

// helperDB opens a fresh database for testing
func helperDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// nonExistentBucket is a bucket name that is never created by initBuckets.
const nonExistentBucket = "nonexistent_bucket_test"

func TestDeleteBucketNotFound(t *testing.T) {
	database := helperDB(t)
	err := database.Delete(nonExistentBucket, "somekey")
	if err == nil {
		t.Error("expected error when deleting from non-existent bucket")
	}
}

func TestExistsBucketNotFound(t *testing.T) {
	database := helperDB(t)
	exists := database.Exists(nonExistentBucket, "somekey")
	if exists {
		t.Error("expected false for Exists on non-existent bucket")
	}
}

func TestPutBucketNotFound(t *testing.T) {
	database := helperDB(t)
	err := database.Put(nonExistentBucket, "somekey", "value")
	if err == nil {
		t.Error("expected error when putting to non-existent bucket")
	}
}

func TestGetBucketNotFound(t *testing.T) {
	database := helperDB(t)
	var result string
	err := database.Get(nonExistentBucket, "somekey", &result)
	if err == nil {
		t.Error("expected error when getting from non-existent bucket")
	}
}

func TestListKeysBucketNotFound(t *testing.T) {
	database := helperDB(t)
	_, err := database.ListKeys(nonExistentBucket)
	if err == nil {
		t.Error("expected error when listing keys in non-existent bucket")
	}
}

func TestForEachBucketNotFound(t *testing.T) {
	database := helperDB(t)
	err := database.ForEach(nonExistentBucket, func(key string, value []byte) error {
		return nil
	})
	if err == nil {
		t.Error("expected error when iterating non-existent bucket")
	}
}

func TestForEachPrefixBucketNotFound(t *testing.T) {
	database := helperDB(t)
	err := database.ForEachPrefix(nonExistentBucket, "prefix", func(key string, value []byte) error {
		return nil
	})
	if err == nil {
		t.Error("expected error when iterating non-existent bucket with prefix")
	}
}

func TestForEachPrefixCallbackError(t *testing.T) {
	database := helperDB(t)

	// Put some data so we can iterate over it
	if err := database.Put(BucketAccounts, "test.com/user1", AccountData{Email: "user1@test.com"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	err := database.ForEachPrefix(BucketAccounts, "test.com/", func(key string, value []byte) error {
		return fmt.Errorf("callback error")
	})
	if err == nil {
		t.Error("expected error from ForEachPrefix callback")
	}
}

// --- initBuckets error path ---
// initBuckets only fails if bolt.Update fails or CreateBucketIfNotExists fails.
// We can trigger this by closing the db first, then calling initBuckets.

func TestInitBucketsOnClosedDB(t *testing.T) {
	database := helperDB(t)
	database.Close()

	err := database.initBuckets()
	if err == nil {
		t.Error("expected error calling initBuckets on closed database")
	}
}

// --- IsBlocked expired entry ---

func TestIsBlockedExpiredEntry(t *testing.T) {
	database := helperDB(t)

	// Block an IP with a very short expiry (1 nanosecond in the past)
	ip := "10.20.30.40"
	err := database.BlockIP(ip, "testing expiry", "auto", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("BlockIP failed: %v", err)
	}

	// Wait briefly for the expiry to pass
	time.Sleep(2 * time.Millisecond)

	// Now IsBlocked should auto-unblock
	blocked, entry := database.IsBlocked(ip)
	if blocked {
		t.Error("expected IP to not be blocked after expiry")
	}
	if entry != nil {
		t.Error("expected nil entry for expired block")
	}

	// Verify the entry was actually deleted
	blocked2, _ := database.IsBlocked(ip)
	if blocked2 {
		t.Error("expected IP to remain unblocked (entry should be deleted)")
	}
}

// --- ListDomains with corrupt data (unmarshal error) ---

func TestListDomainsUnmarshalError(t *testing.T) {
	database := helperDB(t)

	// Put invalid JSON directly into the domains bucket
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDomains))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		return b.Put([]byte("bad-domain"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("setup: put raw value: %v", err)
	}

	_, err = database.ListDomains()
	if err == nil {
		t.Error("expected error from ListDomains with corrupt data")
	}
}

// --- ListAccountsByDomain with corrupt data ---

func TestListAccountsByDomainUnmarshalError(t *testing.T) {
	database := helperDB(t)

	// Put invalid JSON directly into the accounts bucket with proper prefix
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		return b.Put([]byte("badomain.com/baduser"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("setup: put raw value: %v", err)
	}

	_, err = database.ListAccountsByDomain("badomain.com")
	if err == nil {
		t.Error("expected error from ListAccountsByDomain with corrupt data")
	}
}

// --- ListAccountsByDomain empty domain ---

func TestListAccountsByDomainEmpty(t *testing.T) {
	database := helperDB(t)

	accounts, err := database.ListAccountsByDomain("nonexistent-empty.com")
	if err != nil {
		t.Fatalf("ListAccountsByDomain on empty domain: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(accounts))
	}
}

// --- GetPendingQueue edge cases ---

func TestGetPendingQueueEmpty(t *testing.T) {
	database := helperDB(t)

	entries, err := database.GetPendingQueue(time.Now())
	if err != nil {
		t.Fatalf("GetPendingQueue on empty queue: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetPendingQueueFutureRetry(t *testing.T) {
	database := helperDB(t)

	// Enqueue an entry with a future NextRetry
	entry := &QueueEntry{
		ID:        "future-retry",
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Status:    "pending",
		NextRetry: time.Now().Add(1 * time.Hour), // future
	}
	if err := database.Enqueue(entry); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	entries, err := database.GetPendingQueue(time.Now())
	if err != nil {
		t.Fatalf("GetPendingQueue: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 pending entries (future retry), got %d", len(entries))
	}
}

func TestGetPendingQueueNonPendingStatus(t *testing.T) {
	database := helperDB(t)

	// Enqueue an entry with status "delivered" (not "pending")
	entry := &QueueEntry{
		ID:        "delivered-msg",
		From:      "sender@example.com",
		To:        []string{"recipient@example.com"},
		Status:    "delivered",
		NextRetry: time.Now().Add(-1 * time.Hour), // past
	}
	if err := database.Enqueue(entry); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	entries, err := database.GetPendingQueue(time.Now().Add(2 * time.Hour))
	if err != nil {
		t.Fatalf("GetPendingQueue: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-pending status, got %d", len(entries))
	}
}

func TestGetPendingQueueCorruptData(t *testing.T) {
	database := helperDB(t)

	// Put invalid JSON in the queue bucket
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketQueue))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		return b.Put([]byte("corrupt-entry"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("setup: put raw value: %v", err)
	}

	_, err = database.GetPendingQueue(time.Now())
	if err == nil {
		t.Error("expected error from GetPendingQueue with corrupt data")
	}
}

// --- GetUIDValidity / GetUIDNext error paths ---

func TestGetUIDValidityPutFail(t *testing.T) {
	database := helperDB(t)
	database.Close()

	// After closing, Put should fail
	_, err := database.GetUIDValidity("x.com", "user", "INBOX")
	if err == nil {
		t.Error("expected error calling GetUIDValidity on closed database")
	}
}

func TestGetUIDNextPutFail(t *testing.T) {
	database := helperDB(t)
	database.Close()

	// After closing, Put should fail
	_, err := database.GetUIDNext("x.com", "user", "INBOX")
	if err == nil {
		t.Error("expected error calling GetUIDNext on closed database")
	}
}

// --- Put marshal error ---
// Already tested in existing coverage_test.go but let's also cover Put on closed db

func TestPutOnClosedDB(t *testing.T) {
	database := helperDB(t)
	database.Close()

	err := database.Put(BucketAccounts, "key", "value")
	if err == nil {
		t.Error("expected error when putting to closed database")
	}
}

func TestGetOnClosedDB(t *testing.T) {
	database := helperDB(t)
	database.Close()

	var result string
	err := database.Get(BucketAccounts, "key", &result)
	if err == nil {
		t.Error("expected error when getting from closed database")
	}
}

// --- ListKeys on empty bucket ---

func TestListKeysEmptyBucket(t *testing.T) {
	database := helperDB(t)

	keys, err := database.ListKeys(BucketDomains)
	if err != nil {
		t.Fatalf("ListKeys on empty bucket: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys in empty bucket, got %d", len(keys))
	}
}

// --- ForEach on empty bucket ---

func TestForEachEmptyBucket(t *testing.T) {
	database := helperDB(t)

	count := 0
	err := database.ForEach(BucketSpam, func(key string, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach on empty bucket: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 items in empty bucket, got %d", count)
	}
}

// --- Exists on existing key ---

func TestExistsExistingKey(t *testing.T) {
	database := helperDB(t)

	if err := database.Put(BucketMetrics, "check-key", "value"); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if !database.Exists(BucketMetrics, "check-key") {
		t.Error("expected Exists to return true for existing key")
	}
}

// --- ForEachPrefix with empty prefix (should match all entries) ---

func TestForEachPrefixEmptyPrefix(t *testing.T) {
	database := helperDB(t)

	// Create two accounts in different domains
	if err := database.CreateAccount(&AccountData{Email: "a@dom1.com", Domain: "dom1.com", LocalPart: "a"}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if err := database.CreateAccount(&AccountData{Email: "b@dom2.com", Domain: "dom2.com", LocalPart: "b"}); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	count := 0
	err := database.ForEachPrefix(BucketAccounts, "", func(key string, value []byte) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix with empty prefix: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries with empty prefix, got %d", count)
	}
}

// --- Open with deeply nested path (directory creation) ---

func TestOpenDeepPath(t *testing.T) {
	tmpDir := t.TempDir()
	deepPath := tmpDir + "/a/b/c/d/test.db"
	database, err := Open(deepPath)
	if err != nil {
		t.Fatalf("Open with deep path failed: %v", err)
	}
	database.Close()
}

// --- Get with unmarshal error ---

func TestGetUnmarshalError(t *testing.T) {
	database := helperDB(t)

	// Put raw bytes directly into a bucket
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDomains))
		return b.Put([]byte("badkey"), []byte("not-json"))
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	var result DomainData
	err = database.Get(BucketDomains, "badkey", &result)
	if err == nil {
		t.Error("expected unmarshal error")
	}
}

// --- ForEach corrupt data in callback ---

func TestForEachCallbackError(t *testing.T) {
	database := helperDB(t)

	if err := database.Put(BucketDomains, "d1.com", DomainData{Name: "d1.com"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	err := database.ForEach(BucketDomains, func(key string, value []byte) error {
		return fmt.Errorf("stop iteration")
	})
	if err == nil || err.Error() != "stop iteration" {
		t.Errorf("expected 'stop iteration' error, got %v", err)
	}
}

// --- BlockIP with no expiry (duration = 0, ExpiresAt stays zero) ---

func TestBlockIPNoExpiryNotExpired(t *testing.T) {
	database := helperDB(t)

	ip := "172.16.0.1"
	if err := database.BlockIP(ip, "permanent", "manual", 0); err != nil {
		t.Fatalf("BlockIP failed: %v", err)
	}

	// Should still be blocked even after some time
	blocked, entry := database.IsBlocked(ip)
	if !blocked {
		t.Error("expected permanently blocked IP to remain blocked")
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if !entry.ExpiresAt.IsZero() {
		t.Errorf("expected zero ExpiresAt for permanent block, got %v", entry.ExpiresAt)
	}
}

// --- Corrupted blocklist entry (bad json) handled by IsBlocked ---

func TestIsBlockedCorruptEntry(t *testing.T) {
	database := helperDB(t)

	ip := "172.16.99.99"
	// Put bad JSON in the blocklist
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketBlocklist))
		return b.Put([]byte(ip), []byte("not-json"))
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// IsBlocked should return false, nil when Get fails (json unmarshal error)
	blocked, entry := database.IsBlocked(ip)
	if blocked {
		t.Error("expected not blocked for corrupt entry")
	}
	if entry != nil {
		t.Error("expected nil entry for corrupt entry")
	}
}

// --- Open on existing file (re-open) ---

func TestOpenExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/reopen.db"

	// Create and close
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	database.Close()

	// Re-open
	database2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	database2.Close()
}

// --- initBuckets with all buckets already existing (idempotent) ---

func TestInitBucketsIdempotent(t *testing.T) {
	database := helperDB(t)

	// Call initBuckets again - all buckets already exist
	if err := database.initBuckets(); err != nil {
		t.Errorf("initBuckets on existing buckets should not fail: %v", err)
	}
}

// --- Test ForEachPrefix with multiple entries having same prefix ---

func TestForEachPrefixMultipleEntries(t *testing.T) {
	database := helperDB(t)

	// Create multiple accounts in the same domain
	for i := 0; i < 5; i++ {
		err := database.CreateAccount(&AccountData{
			Email:     fmt.Sprintf("user%d@multi.com", i),
			Domain:    "multi.com",
			LocalPart: fmt.Sprintf("user%d", i),
		})
		if err != nil {
			t.Fatalf("CreateAccount %d: %v", i, err)
		}
	}

	// Also create one in a different domain
	if err := database.CreateAccount(&AccountData{
		Email: "other@other.com", Domain: "other.com", LocalPart: "other",
	}); err != nil {
		t.Fatalf("CreateAccount other: %v", err)
	}

	count := 0
	err := database.ForEachPrefix(BucketAccounts, "multi.com/", func(key string, value []byte) error {
		var acc AccountData
		if err := json.Unmarshal(value, &acc); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 accounts for multi.com, got %d", count)
	}
}

// --- Ensure bbolt database direct deletion of bucket triggers error paths ---
// This tests that the functions handle "bucket not found" correctly
// by deleting a known bucket and then calling functions that need it.

func TestFunctionsWithDeletedBucket(t *testing.T) {
	database := helperDB(t)

	// Delete the metrics bucket to trigger "bucket not found" paths
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketMetrics))
		if b != nil {
			return tx.DeleteBucket([]byte(BucketMetrics))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: delete bucket: %v", err)
	}

	// Put should fail
	if err := database.Put(BucketMetrics, "key", "val"); err == nil {
		t.Error("expected Put to fail with deleted bucket")
	}

	// Get should fail
	var s string
	if err := database.Get(BucketMetrics, "key", &s); err == nil {
		t.Error("expected Get to fail with deleted bucket")
	}

	// Delete should fail
	if err := database.Delete(BucketMetrics, "key"); err == nil {
		t.Error("expected Delete to fail with deleted bucket")
	}

	// Exists should return false
	if database.Exists(BucketMetrics, "key") {
		t.Error("expected Exists=false with deleted bucket")
	}

	// ListKeys should fail
	if _, err := database.ListKeys(BucketMetrics); err == nil {
		t.Error("expected ListKeys to fail with deleted bucket")
	}

	// ForEach should fail
	if err := database.ForEach(BucketMetrics, func(key string, value []byte) error { return nil }); err == nil {
		t.Error("expected ForEach to fail with deleted bucket")
	}

	// ForEachPrefix should fail
	if err := database.ForEachPrefix(BucketMetrics, "p", func(key string, value []byte) error { return nil }); err == nil {
		t.Error("expected ForEachPrefix to fail with deleted bucket")
	}
}

// --- Put with various value types ---

func TestPutWithDifferentTypes(t *testing.T) {
	database := helperDB(t)

	// Put a string
	if err := database.Put(BucketMetrics, "str-key", "hello"); err != nil {
		t.Errorf("Put string: %v", err)
	}

	// Put an int
	if err := database.Put(BucketMetrics, "int-key", 42); err != nil {
		t.Errorf("Put int: %v", err)
	}

	// Put a map
	if err := database.Put(BucketMetrics, "map-key", map[string]string{"a": "b"}); err != nil {
		t.Errorf("Put map: %v", err)
	}

	// Retrieve and verify
	var s string
	if err := database.Get(BucketMetrics, "str-key", &s); err != nil {
		t.Errorf("Get string: %v", err)
	}
	if s != "hello" {
		t.Errorf("expected 'hello', got '%s'", s)
	}

	var n int
	if err := database.Get(BucketMetrics, "int-key", &n); err != nil {
		t.Errorf("Get int: %v", err)
	}
	if n != 42 {
		t.Errorf("expected 42, got %d", n)
	}
}

// --- Test db.Close on already-closed database ---
// bbolt supports double-close but returns err on the second close.

func TestDoubleClose(t *testing.T) {
	database := helperDB(t)
	if err := database.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should also work (bbolt handles it)
	_ = database.Close()
}

// --- Delete on already-deleted key ---

func TestDeleteAlreadyDeletedKey(t *testing.T) {
	database := helperDB(t)

	if err := database.Put(BucketMetrics, "temp-key", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := database.Delete(BucketMetrics, "temp-key"); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	// Delete again (key already gone, bbolt Delete on nil key returns nil)
	if err := database.Delete(BucketMetrics, "temp-key"); err != nil {
		t.Errorf("second Delete of absent key should not error: %v", err)
	}
}

// --- Open with options (default timeout) ---

func TestOpenDefaultOptions(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := Open(tmpDir + "/default.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Verify the underlying bbolt db has the expected path
	boltDB := db.BoltDB()
	if boltDB == nil {
		t.Fatal("BoltDB() returned nil")
	}
	stats := boltDB.Stats()
	// Just verify stats are accessible (not all zeros would indicate an open db)
	_ = stats
}

// --- ForEachPrefix with exact prefix match ---

func TestForEachPrefixExactMatch(t *testing.T) {
	database := helperDB(t)

	// Create entries where one key is a prefix of another
	if err := database.Put(BucketMetrics, "abc", "val1"); err != nil {
		t.Fatalf("Put abc: %v", err)
	}
	if err := database.Put(BucketMetrics, "abcd", "val2"); err != nil {
		t.Fatalf("Put abcd: %v", err)
	}
	if err := database.Put(BucketMetrics, "abce", "val3"); err != nil {
		t.Fatalf("Put abce: %v", err)
	}
	if err := database.Put(BucketMetrics, "xyz", "val4"); err != nil {
		t.Fatalf("Put xyz: %v", err)
	}

	var matched []string
	err := database.ForEachPrefix(BucketMetrics, "abc", func(key string, value []byte) error {
		matched = append(matched, key)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachPrefix: %v", err)
	}
	if len(matched) != 3 {
		t.Errorf("expected 3 matches for prefix 'abc', got %d: %v", len(matched), matched)
	}
}

// --- Test that GetPendingQueue only returns pending entries with past NextRetry ---

func TestGetPendingQueueMixedStatuses(t *testing.T) {
	database := helperDB(t)

	now := time.Now()

	// pending + past retry -> should be returned
	if err := database.Enqueue(&QueueEntry{
		ID: "pending-past", Status: "pending",
		NextRetry: now.Add(-1 * time.Hour),
		From: "a@b.com", To: []string{"c@d.com"},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// pending + future retry -> should NOT be returned
	if err := database.Enqueue(&QueueEntry{
		ID: "pending-future", Status: "pending",
		NextRetry: now.Add(1 * time.Hour),
		From: "a@b.com", To: []string{"c@d.com"},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// delivered + past retry -> should NOT be returned
	if err := database.Enqueue(&QueueEntry{
		ID: "delivered-past", Status: "delivered",
		NextRetry: now.Add(-1 * time.Hour),
		From: "a@b.com", To: []string{"c@d.com"},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// sending + past retry -> should NOT be returned
	if err := database.Enqueue(&QueueEntry{
		ID: "sending-past", Status: "sending",
		NextRetry: now.Add(-1 * time.Hour),
		From: "a@b.com", To: []string{"c@d.com"},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entries, err := database.GetPendingQueue(now)
	if err != nil {
		t.Fatalf("GetPendingQueue: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 pending entry, got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].ID != "pending-past" {
		t.Errorf("expected 'pending-past' entry, got %s", entries[0].ID)
	}
}

// --- ListAliasesByDomain with corrupt data (triggers json.Unmarshal returning nil) ---

func TestListAliasesByDomainCorruptData(t *testing.T) {
	database := helperDB(t)

	// Put corrupt JSON in the aliases bucket with the correct prefix
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAliases))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		return b.Put([]byte("corrupt.com:badalias"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// ListAliasesByDomain silently skips corrupt entries (returns nil in callback)
	aliases, err := database.ListAliasesByDomain("corrupt.com")
	if err != nil {
		t.Fatalf("ListAliasesByDomain with corrupt data: %v", err)
	}
	// Corrupt entries are skipped, so we should get 0 aliases
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases (corrupt data skipped), got %d", len(aliases))
	}
}

// --- GetMailboxACL with corrupt data (triggers json.Unmarshal returning nil) ---

func TestGetMailboxACLCorruptData(t *testing.T) {
	database := helperDB(t)

	// Put corrupt JSON in the ACL bucket with the correct prefix
	err := database.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketACL))
		if b == nil {
			return fmt.Errorf("bucket not found")
		}
		return b.Put([]byte("testuser/badbox/badident"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// GetMailboxACL silently skips corrupt entries
	entries, err := database.GetMailboxACL("testuser", "badbox")
	if err != nil {
		t.Fatalf("GetMailboxACL with corrupt data: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (corrupt data skipped), got %d", len(entries))
	}
}

// --- Verify bbolt import is used ---
var _ = (*bbolt.DB)(nil)
