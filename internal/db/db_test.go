package db

import (
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
			Name:           "example.com",
			MaxAccounts:    100,
			MaxMailboxSize: 5 * 1024 * 1024 * 1024,
			DKIMSelector:   "default",
			IsActive:       true,
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
}

func TestAccountKey(t *testing.T) {
	key := AccountKey("example.com", "user")
	expected := "example.com/user"
	if key != expected {
		t.Errorf("expected %s, got %s", expected, key)
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
		Name:           "update-test.com",
		MaxAccounts:    100,
		MaxMailboxSize: 5 * 1024 * 1024 * 1024,
		DKIMSelector:   "default",
		IsActive:       true,
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

func TestAliasOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	t.Run("GetAlias_NotFound", func(t *testing.T) {
		_, err := db.GetAlias("example.com", "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent alias")
		}
	})

	t.Run("GetAliasAndResolve", func(t *testing.T) {
		alias := &AliasData{
			Alias:     "alias@example.com",
			Target:    "user@example.com",
			Domain:    "example.com",
			IsActive:  true,
			CreatedAt: time.Now(),
		}

		key := "example.com:alias"
		if err := db.Put(BucketAliases, key, alias); err != nil {
			t.Fatalf("Put alias failed: %v", err)
		}

		// Test GetAlias
		retrieved, err := db.GetAlias("example.com", "alias")
		if err != nil {
			t.Fatalf("GetAlias failed: %v", err)
		}
		if retrieved.Alias != alias.Alias {
			t.Errorf("expected alias %s, got %s", alias.Alias, retrieved.Alias)
		}
		if retrieved.Target != alias.Target {
			t.Errorf("expected target %s, got %s", alias.Target, retrieved.Target)
		}

		// Test ResolveAlias
		target, err := db.ResolveAlias("example.com", "alias")
		if err != nil {
			t.Fatalf("ResolveAlias failed: %v", err)
		}
		if target != alias.Target {
			t.Errorf("expected target %s, got %s", alias.Target, target)
		}
	})

	t.Run("ResolveAlias_Inactive", func(t *testing.T) {
		alias := &AliasData{
			Alias:     "inactive@example.com",
			Target:    "user@example.com",
			Domain:    "example.com",
			IsActive:  false,
			CreatedAt: time.Now(),
		}

		key := "example.com:inactive"
		if err := db.Put(BucketAliases, key, alias); err != nil {
			t.Fatalf("Put alias failed: %v", err)
		}

		target, err := db.ResolveAlias("example.com", "inactive")
		if err != nil {
			t.Fatalf("ResolveAlias failed: %v", err)
		}
		if target != "" {
			t.Errorf("expected empty target for inactive alias, got %s", target)
		}
	})

	t.Run("ResolveAlias_NotFound", func(t *testing.T) {
		_, err := db.ResolveAlias("example.com", "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent alias")
		}
	})
}
