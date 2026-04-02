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

	t.Run("SessionOperations", func(t *testing.T) {
		session := &SessionData{
			ID:       "session-123",
			Type:     "imap",
			User:     "test@example.com",
			Domain:   "example.com",
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

// --- Alias Operations ---

func TestAliasOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_alias.db"

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	t.Run("CreateAndGetAlias", func(t *testing.T) {
		alias := &AliasData{
			Alias:    "info@example.com",
			Target:   "admin@example.com",
			Domain:   "example.com",
			IsActive: true,
		}

		if err := database.CreateAlias(alias); err != nil {
			t.Fatalf("CreateAlias failed: %v", err)
		}

		retrieved, err := database.GetAlias("example.com", "info")
		if err != nil {
			t.Fatalf("GetAlias failed: %v", err)
		}
		if retrieved.Target != "admin@example.com" {
			t.Errorf("expected target admin@example.com, got %s", retrieved.Target)
		}
		if !retrieved.IsActive {
			t.Error("expected IsActive=true")
		}
		if retrieved.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set")
		}
	})

	t.Run("UpdateAlias", func(t *testing.T) {
		alias := &AliasData{
			Alias:    "sales@example.com",
			Target:   "team@example.com",
			Domain:   "example.com",
			IsActive: true,
		}
		if err := database.CreateAlias(alias); err != nil {
			t.Fatalf("CreateAlias failed: %v", err)
		}

		alias.Target = "newteam@example.com"
		if err := database.UpdateAlias(alias); err != nil {
			t.Fatalf("UpdateAlias failed: %v", err)
		}

		retrieved, err := database.GetAlias("example.com", "sales")
		if err != nil {
			t.Fatalf("GetAlias failed: %v", err)
		}
		if retrieved.Target != "newteam@example.com" {
			t.Errorf("expected target newteam@example.com, got %s", retrieved.Target)
		}
	})

	t.Run("DeleteAlias", func(t *testing.T) {
		alias := &AliasData{
			Alias:    "tmp@example.com",
			Target:   "admin@example.com",
			Domain:   "example.com",
			IsActive: true,
		}
		if err := database.CreateAlias(alias); err != nil {
			t.Fatalf("CreateAlias failed: %v", err)
		}

		if err := database.DeleteAlias("example.com", "tmp"); err != nil {
			t.Fatalf("DeleteAlias failed: %v", err)
		}

		_, err := database.GetAlias("example.com", "tmp")
		if err == nil {
			t.Error("expected error after deleting alias")
		}
	})

	t.Run("ListAliasesByDomain", func(t *testing.T) {
		aliases, err := database.ListAliasesByDomain("example.com")
		if err != nil {
			t.Fatalf("ListAliasesByDomain failed: %v", err)
		}
		if len(aliases) < 2 {
			t.Errorf("expected at least 2 aliases, got %d", len(aliases))
		}
	})

	t.Run("ListAliasesEmptyDomain", func(t *testing.T) {
		aliases, err := database.ListAliasesByDomain("nonexistent.com")
		if err != nil {
			t.Fatalf("ListAliasesByDomain failed: %v", err)
		}
		if len(aliases) != 0 {
			t.Errorf("expected 0 aliases, got %d", len(aliases))
		}
	})

	t.Run("ResolveAlias", func(t *testing.T) {
		target, err := database.ResolveAlias("example.com", "info")
		if err != nil {
			t.Fatalf("ResolveAlias failed: %v", err)
		}
		if target != "admin@example.com" {
			t.Errorf("expected target admin@example.com, got %s", target)
		}
	})

	t.Run("ResolveAliasInactive", func(t *testing.T) {
		alias := &AliasData{
			Alias:    "inactive@example.com",
			Target:   "admin@example.com",
			Domain:   "example.com",
			IsActive: false,
		}
		if err := database.CreateAlias(alias); err != nil {
			t.Fatalf("CreateAlias failed: %v", err)
		}

		target, err := database.ResolveAlias("example.com", "inactive")
		if err != nil {
			t.Fatalf("ResolveAlias failed: %v", err)
		}
		if target != "" {
			t.Errorf("expected empty target for inactive alias, got %s", target)
		}
	})

	t.Run("ResolveAliasNonExistent", func(t *testing.T) {
		target, err := database.ResolveAlias("example.com", "nonexistent")
		if err != nil {
			// Non-existent alias returns error from GetAlias
			return
		}
		if target != "" {
			t.Errorf("expected empty target, got %s", target)
		}
	})

	t.Run("GetAliasNotFound", func(t *testing.T) {
		_, err := database.GetAlias("nonexistent.com", "nope")
		if err == nil {
			t.Error("expected error for non-existent alias")
		}
	})
}

// --- ACL Operations ---

func TestACLOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_acl.db"

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	user := "testuser"
	mailbox := "INBOX"

	t.Run("SetAndGetACL", func(t *testing.T) {
		rights := []string{"l", "r", "s", "w"}
		if err := database.SetMailboxACL(user, mailbox, "user2", rights); err != nil {
			t.Fatalf("SetMailboxACL failed: %v", err)
		}

		entries, err := database.GetMailboxACL(user, mailbox)
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 ACL entry, got %d", len(entries))
		}
		if entries[0].Identifier != "user2" {
			t.Errorf("expected identifier user2, got %s", entries[0].Identifier)
		}
		if len(entries[0].Rights) != 4 {
			t.Errorf("expected 4 rights, got %d", len(entries[0].Rights))
		}
	})

	t.Run("GetACLNotFound", func(t *testing.T) {
		entries, err := database.GetMailboxACL("nouser", "nomailbox")
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("SetMultipleACL", func(t *testing.T) {
		if err := database.SetMailboxACL(user, mailbox, "user3", []string{"l", "r"}); err != nil {
			t.Fatalf("SetMailboxACL failed: %v", err)
		}

		entries, err := database.GetMailboxACL(user, mailbox)
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		if len(entries) != 2 {
			t.Errorf("expected 2 ACL entries, got %d", len(entries))
		}
	})

	t.Run("DeleteACL", func(t *testing.T) {
		if err := database.DeleteMailboxACL(user, mailbox, "user2"); err != nil {
			t.Fatalf("DeleteMailboxACL failed: %v", err)
		}

		entries, err := database.GetMailboxACL(user, mailbox)
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("expected 1 ACL entry after delete, got %d", len(entries))
		}
		if entries[0].Identifier != "user3" {
			t.Errorf("expected remaining identifier user3, got %s", entries[0].Identifier)
		}
	})

	t.Run("GetACLEmpty", func(t *testing.T) {
		entries, err := database.GetMailboxACL("otheruser", "OtherBox")
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 ACL entries, got %d", len(entries))
		}
	})

	t.Run("UpdateACL", func(t *testing.T) {
		if err := database.SetMailboxACL(user, mailbox, "user3", []string{"l", "r", "w", "i", "p"}); err != nil {
			t.Fatalf("SetMailboxACL update failed: %v", err)
		}

		entries, err := database.GetMailboxACL(user, mailbox)
		if err != nil {
			t.Fatalf("GetMailboxACL failed: %v", err)
		}
		for _, e := range entries {
			if e.Identifier == "user3" && len(e.Rights) != 5 {
				t.Errorf("expected 5 rights after update, got %d", len(e.Rights))
			}
		}
	})
}

// --- Subscription Operations ---

func TestSubscriptionOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test_subs.db"

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	user := "testuser"

	t.Run("SubscribeAndCheck", func(t *testing.T) {
		if err := database.Subscribe(user, "INBOX"); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		subscribed, err := database.IsSubscribed(user, "INBOX")
		if err != nil {
			t.Fatalf("IsSubscribed failed: %v", err)
		}
		if !subscribed {
			t.Error("expected INBOX to be subscribed")
		}
	})

	t.Run("NotSubscribed", func(t *testing.T) {
		subscribed, err := database.IsSubscribed(user, "Archive")
		if err != nil {
			t.Fatalf("IsSubscribed failed: %v", err)
		}
		if subscribed {
			t.Error("expected Archive to not be subscribed")
		}
	})

	t.Run("ListSubscriptions", func(t *testing.T) {
		if err := database.Subscribe(user, "Sent"); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
		if err := database.Subscribe(user, "Drafts"); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}

		subs, err := database.ListSubscriptions(user)
		if err != nil {
			t.Fatalf("ListSubscriptions failed: %v", err)
		}
		if len(subs) != 3 {
			t.Errorf("expected 3 subscriptions, got %d", len(subs))
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		if err := database.Unsubscribe(user, "Sent"); err != nil {
			t.Fatalf("Unsubscribe failed: %v", err)
		}

		subscribed, err := database.IsSubscribed(user, "Sent")
		if err != nil {
			t.Fatalf("IsSubscribed failed: %v", err)
		}
		if subscribed {
			t.Error("expected Sent to not be subscribed after unsubscribe")
		}

		subs, err := database.ListSubscriptions(user)
		if err != nil {
			t.Fatalf("ListSubscriptions failed: %v", err)
		}
		if len(subs) != 2 {
			t.Errorf("expected 2 subscriptions after unsubscribe, got %d", len(subs))
		}
	})

	t.Run("ListSubscriptionsEmpty", func(t *testing.T) {
		subs, err := database.ListSubscriptions("otheruser")
		if err != nil {
			t.Fatalf("ListSubscriptions failed: %v", err)
		}
		if len(subs) != 0 {
			t.Errorf("expected 0 subscriptions for unknown user, got %d", len(subs))
		}
	})

	t.Run("CaseInsensitiveUser", func(t *testing.T) {
		if err := database.Subscribe("TestUser", "INBOX"); err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
		subscribed, err := database.IsSubscribed("testuser", "INBOX")
		if err != nil {
			t.Fatalf("IsSubscribed failed: %v", err)
		}
		if !subscribed {
			t.Error("expected case-insensitive subscription to work")
		}
	})
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
