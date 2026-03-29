package db

import (
	"fmt"
	"testing"
	"time"
)

func TestDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	t.Run("AccountOperations", func(t *testing.T) {
		account := &AccountData{
			Email:        "test@example.com",
			LocalPart:    "test",
			Domain:       "example.com",
			PasswordHash: "argon2:...",
			QuotaLimit:   5 * 1024 * 1024 * 1024, // 5GB
			IsActive:     true,
		}

		// Create
		if err := db.CreateAccount(account); err != nil {
			t.Fatalf("CreateAccount failed: %v", err)
		}

		// Get
		retrieved, err := db.GetAccount("example.com", "test")
		if err != nil {
			t.Fatalf("GetAccount failed: %v", err)
		}
		if retrieved.Email != account.Email {
			t.Errorf("expected email %s, got %s", account.Email, retrieved.Email)
		}

		// Update
		retrieved.QuotaUsed = 1024
		if err := db.UpdateAccount(retrieved); err != nil {
			t.Fatalf("UpdateAccount failed: %v", err)
		}

		// List
		accounts, err := db.ListAccountsByDomain("example.com")
		if err != nil {
			t.Fatalf("ListAccountsByDomain failed: %v", err)
		}
		if len(accounts) != 1 {
			t.Errorf("expected 1 account, got %d", len(accounts))
		}

		// Delete
		if err := db.DeleteAccount("example.com", "test"); err != nil {
			t.Fatalf("DeleteAccount failed: %v", err)
		}

		_, err = db.GetAccount("example.com", "test")
		if err == nil {
			t.Error("expected error after delete")
		}
	})

	t.Run("DomainOperations", func(t *testing.T) {
		domain := &DomainData{
			Name:          "example.com",
			MaxAccounts:   100,
			MaxMailboxSize: 5 * 1024 * 1024 * 1024,
			DKIMSelector:  "default",
			IsActive:      true,
		}

		// Create
		if err := db.CreateDomain(domain); err != nil {
			t.Fatalf("CreateDomain failed: %v", err)
		}

		// Get
		retrieved, err := db.GetDomain("example.com")
		if err != nil {
			t.Fatalf("GetDomain failed: %v", err)
		}
		if retrieved.Name != domain.Name {
			t.Errorf("expected name %s, got %s", domain.Name, retrieved.Name)
		}

		// List
		domains, err := db.ListDomains()
		if err != nil {
			t.Fatalf("ListDomains failed: %v", err)
		}
		if len(domains) != 1 {
			t.Errorf("expected 1 domain, got %d", len(domains))
		}

		// Delete
		if err := db.DeleteDomain("example.com"); err != nil {
			t.Fatalf("DeleteDomain failed: %v", err)
		}
	})

	t.Run("QueueOperations", func(t *testing.T) {
		entry := &QueueEntry{
			ID:          "msg-123",
			From:        "sender@example.com",
			To:          []string{"recipient@example.com"},
			MessagePath: "/tmp/msg-123",
			Status:      "pending",
			NextRetry:   time.Now(),
		}

		// Enqueue
		if err := db.Enqueue(entry); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}

		// Get
		retrieved, err := db.GetQueueEntry("msg-123")
		if err != nil {
			t.Fatalf("GetQueueEntry failed: %v", err)
		}
		if retrieved.ID != entry.ID {
			t.Errorf("expected ID %s, got %s", entry.ID, retrieved.ID)
		}

		// GetPendingQueue
		pending, err := db.GetPendingQueue(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("GetPendingQueue failed: %v", err)
		}
		if len(pending) != 1 {
			t.Errorf("expected 1 pending entry, got %d", len(pending))
		}

		// Dequeue
		if err := db.Dequeue("msg-123"); err != nil {
			t.Fatalf("Dequeue failed: %v", err)
		}

		_, err = db.GetQueueEntry("msg-123")
		if err == nil {
			t.Error("expected error after dequeue")
		}
	})

	t.Run("SessionOperations", func(t *testing.T) {
		session := &SessionData{
			ID:     "session-123",
			Type:   "imap",
			User:   "test@example.com",
			Domain: "example.com",
			RemoteIP: "192.168.1.1",
		}

		// Create
		if err := db.CreateSession(session); err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Get
		retrieved, err := db.GetSession("session-123")
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}
		if retrieved.ID != session.ID {
			t.Errorf("expected ID %s, got %s", session.ID, retrieved.ID)
		}

		// Delete
		if err := db.DeleteSession("session-123"); err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}
	})

	t.Run("BlocklistOperations", func(t *testing.T) {
		// Block IP
		if err := db.BlockIP("192.168.1.100", "Too many failed login attempts", "auto", time.Hour); err != nil {
			t.Fatalf("BlockIP failed: %v", err)
		}

		// Check blocked
		blocked, entry := db.IsBlocked("192.168.1.100")
		if !blocked {
			t.Error("expected IP to be blocked")
		}
		if entry == nil {
			t.Error("expected block entry")
		}

		// Unblock
		if err := db.Unblock("192.168.1.100"); err != nil {
			t.Fatalf("Unblock failed: %v", err)
		}

		// Check not blocked
		blocked, _ = db.IsBlocked("192.168.1.100")
		if blocked {
			t.Error("expected IP to not be blocked")
		}
	})

	t.Run("UIDOperations", func(t *testing.T) {
		domain := "uid-test.com"
		user := "uiduser"
		folder := "INBOX"

		// Get UID validity (should generate new one)
		validity1, err := db.GetUIDValidity(domain, user, folder)
		if err != nil {
			t.Fatalf("GetUIDValidity failed: %v", err)
		}
		if validity1 == 0 {
			t.Error("expected non-zero validity")
		}

		// Get again should return same validity
		validity2, err := db.GetUIDValidity(domain, user, folder)
		if err != nil {
			t.Fatalf("GetUIDValidity failed: %v", err)
		}
		if validity2 != validity1 {
			t.Error("expected same validity value")
		}

		// Get UID next - should return increasing values
		uid1, err := db.GetUIDNext(domain, user, folder)
		if err != nil {
			t.Fatalf("GetUIDNext failed: %v", err)
		}

		uid2, err := db.GetUIDNext(domain, user, folder)
		if err != nil {
			t.Fatalf("GetUIDNext failed: %v", err)
		}

		if uid2 <= uid1 {
			t.Errorf("expected UID to increase: %d -> %d", uid1, uid2)
		}
	})

	t.Run("PutGetDelete", func(t *testing.T) {
		type TestData struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}

		data := TestData{Name: "test", Value: 42}

		// Put
		if err := db.Put(BucketMetrics, "test-key", data); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Get
		var retrieved TestData
		if err := db.Get(BucketMetrics, "test-key", &retrieved); err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Name != data.Name || retrieved.Value != data.Value {
			t.Error("retrieved data doesn't match")
		}

		// Exists
		if !db.Exists(BucketMetrics, "test-key") {
			t.Error("expected key to exist")
		}

		// Delete
		if err := db.Delete(BucketMetrics, "test-key"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if db.Exists(BucketMetrics, "test-key") {
			t.Error("expected key to not exist after delete")
		}
	})

	t.Run("ListKeys", func(t *testing.T) {
		type TestData struct{ Value int }

		// Add some keys
		for i := 0; i < 3; i++ {
			if err := db.Put(BucketMetrics, fmt.Sprintf("key-%d", i), TestData{Value: i}); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		keys, err := db.ListKeys(BucketMetrics)
		if err != nil {
			t.Fatalf("ListKeys failed: %v", err)
		}

		// Should have at least 3 keys (might have more from other tests)
		if len(keys) < 3 {
			t.Errorf("expected at least 3 keys, got %d", len(keys))
		}

		// Clean up
		for i := 0; i < 3; i++ {
			db.Delete(BucketMetrics, fmt.Sprintf("key-%d", i))
		}
	})
}

func TestAccountKey(t *testing.T) {
	key := AccountKey("example.com", "user")
	expected := "example.com/user"
	if key != expected {
		t.Errorf("expected %s, got %s", expected, key)
	}
}

// TestBoltDB tests the BoltDB getter
func TestBoltDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Test that BoltDB returns the underlying database
	boltDB := db.BoltDB()
	if boltDB == nil {
		t.Error("BoltDB() returned nil")
	}
}

// TestUpdateDomain tests updating a domain
func TestUpdateDomain(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Create a domain
	domain := &DomainData{
		Name:          "update-test.com",
		MaxAccounts:   100,
		MaxMailboxSize: 5 * 1024 * 1024 * 1024,
		DKIMSelector:  "default",
		IsActive:      true,
	}

	if err := db.CreateDomain(domain); err != nil {
		t.Fatalf("CreateDomain failed: %v", err)
	}

	// Update the domain
	domain.MaxAccounts = 200
	domain.DKIMSelector = "updated"

	if err := db.UpdateDomain(domain); err != nil {
		t.Fatalf("UpdateDomain failed: %v", err)
	}

	// Verify the update
	retrieved, err := db.GetDomain("update-test.com")
	if err != nil {
		t.Fatalf("GetDomain failed: %v", err)
	}
	if retrieved.MaxAccounts != 200 {
		t.Errorf("expected MaxAccounts 200, got %d", retrieved.MaxAccounts)
	}
	if retrieved.DKIMSelector != "updated" {
		t.Errorf("expected DKIMSelector 'updated', got %s", retrieved.DKIMSelector)
	}
	if retrieved.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

// TestUpdateQueueEntry tests updating a queue entry
func TestUpdateQueueEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Create a queue entry
	entry := &QueueEntry{
		ID:          "update-queue-test",
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "/tmp/update-test",
		Status:      "pending",
		NextRetry:   time.Now(),
		RetryCount:  0,
	}

	if err := db.Enqueue(entry); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Update the entry
	entry.Status = "retrying"
	entry.RetryCount = 1
	entry.NextRetry = time.Now().Add(time.Minute)

	if err := db.UpdateQueueEntry(entry); err != nil {
		t.Fatalf("UpdateQueueEntry failed: %v", err)
	}

	// Verify the update
	retrieved, err := db.GetQueueEntry("update-queue-test")
	if err != nil {
		t.Fatalf("GetQueueEntry failed: %v", err)
	}
	if retrieved.Status != "retrying" {
		t.Errorf("expected Status 'retrying', got %s", retrieved.Status)
	}
	if retrieved.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %d", retrieved.RetryCount)
	}
}
