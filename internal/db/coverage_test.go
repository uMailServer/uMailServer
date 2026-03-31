package db

import (
	"fmt"
	"testing"
	"time"
)

func TestDBPutGetDeleteCycle(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Put with marshal error - use a channel which cannot be marshaled
	type BadStruct struct {
		Ch chan int
	}
	err = database.Put(BucketAccounts, "bad", BadStruct{Ch: make(chan int)})
	if err == nil {
		t.Error("expected error for non-marshalable value")
	}

	// Get non-existent key
	var result string
	err = database.Get(BucketAccounts, "nonexistent", &result)
	if err == nil {
		t.Error("expected error for non-existent key")
	}

	// Exists on non-existent key
	exists := database.Exists(BucketAccounts, "nonexistent")
	if exists {
		t.Error("expected false for non-existent key")
	}

	// Delete non-existent key
	err = database.Delete(BucketAccounts, "nonexistent")
	if err != nil {
		t.Errorf("Delete non-existent should not error: %v", err)
	}
}

func TestDBForEach(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// ForEach on empty bucket
	count := 0
	database.ForEach(BucketDomains, func(key string, value []byte) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 items in empty bucket, got %d", count)
	}

	// Create domain and iterate
	database.CreateDomain(&DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateDomain(&DomainData{Name: "test.org", MaxAccounts: 5})

	domainCount := 0
	database.ForEach(BucketDomains, func(key string, value []byte) error {
		domainCount++
		return nil
	})
	if domainCount != 2 {
		t.Errorf("expected 2 domains, got %d", domainCount)
	}

	// ForEach with error callback
	err = database.ForEach(BucketDomains, func(key string, value []byte) error {
		return fmt.Errorf("callback error")
	})
	if err == nil {
		t.Error("expected error from callback")
	}
}

func TestDBForEachPrefix(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Create accounts with same domain
	database.CreateAccount(&AccountData{Email: "user1@example.com", Domain: "example.com", PasswordHash: "hash"})
	database.CreateAccount(&AccountData{Email: "admin@test.org", Domain: "test.org", PasswordHash: "hash"})

	count := 0
	database.ForEachPrefix(BucketAccounts, "example.com/", func(key string, value []byte) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("expected 1 account for example.com, got %d", count)
	}

	// Empty prefix
	emptyCount := 0
	database.ForEachPrefix(BucketAccounts, "nonexistent.com/", func(key string, value []byte) error {
		emptyCount++
		return nil
	})
	if emptyCount != 0 {
		t.Errorf("expected 0 accounts for nonexistent.com, got %d", emptyCount)
	}
}

func TestDBSessionOperations(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Create session
	session := &SessionData{
		ID:       "sess123",
		Type:     "imap",
		User:     "user@example.com",
		Domain:   "example.com",
		RemoteIP: "127.0.0.1",
	}
	err = database.CreateSession(session)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Get session
	retrieved, err := database.GetSession("sess123")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.User != "user@example.com" {
		t.Errorf("expected user@example.com, got %s", retrieved.User)
	}

	// Get non-existent session
	_, err = database.GetSession("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent session")
	}

	// Delete session
	err = database.DeleteSession("sess123")
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
}

func TestDBBlocklistOperations(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Block IP
	err = database.BlockIP("192.168.1.1", "spam", "auto", time.Hour)
	if err != nil {
		t.Fatalf("BlockIP failed: %v", err)
	}

	// Check blocked
	blocked, entry := database.IsBlocked("192.168.1.1")
	if !blocked {
		t.Error("expected IP to be blocked")
	}
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Reason != "spam" {
		t.Errorf("expected reason spam, got %s", entry.Reason)
	}

	// Check non-blocked IP
	blocked, _ = database.IsBlocked("10.0.0.1")
	if blocked {
		t.Error("expected IP to not be blocked")
	}

	// Block with no expiry
	err = database.BlockIP("10.0.0.2", "abuse", "manual", 0)
	if err != nil {
		t.Fatalf("BlockIP (no expiry) failed: %v", err)
	}
	blocked, _ = database.IsBlocked("10.0.0.2")
	if !blocked {
		t.Error("expected IP to be blocked")
	}

	// Block with short future expiry then wait for it to pass
	// (we cannot test negative duration since BlockIP ignores duration <= 0)

	// Unblock
	err = database.Unblock("192.168.1.1")
	if err != nil {
		t.Fatalf("Unblock failed: %v", err)
	}
	blocked, _ = database.IsBlocked("192.168.1.1")
	if blocked {
		t.Error("expected IP to be unblocked")
	}
}

func TestDBGetDomainNotFound(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	_, err = database.GetDomain("nonexistent.com")
	if err == nil {
		t.Error("expected error for non-existent domain")
	}
}

func TestDBListKeys(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	database.CreateDomain(&DomainData{Name: "a.com", MaxAccounts: 1})
	database.CreateDomain(&DomainData{Name: "b.com", MaxAccounts: 1})

	keys, err := database.ListKeys(BucketDomains)
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestDBAliasOperations(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Create alias
	alias := &AliasData{
		Alias:    "info@example.com",
		Target:   "admin@example.com",
		Domain:   "example.com",
		IsActive: true,
	}
	err = database.CreateAlias(alias)
	if err != nil {
		t.Fatalf("CreateAlias failed: %v", err)
	}

	// Get alias
	retrieved, err := database.GetAlias("example.com", "info")
	if err != nil {
		t.Fatalf("GetAlias failed: %v", err)
	}
	if retrieved.Target != "admin@example.com" {
		t.Errorf("expected target admin@example.com, got %s", retrieved.Target)
	}

	// List aliases by domain
	aliases, err := database.ListAliasesByDomain("example.com")
	if err != nil {
		t.Fatalf("ListAliasesByDomain failed: %v", err)
	}
	if len(aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(aliases))
	}

	// Delete alias
	err = database.DeleteAlias("example.com", "info")
	if err != nil {
		t.Fatalf("DeleteAlias failed: %v", err)
	}

	// Verify deleted
	_, err = database.GetAlias("example.com", "info")
	if err == nil {
		t.Error("expected error for deleted alias")
	}
}

func TestDBMailboxACLOperations(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	user := "user@example.com"
	mailbox := "INBOX"
	identifier := "shared@example.com"
	rights := []string{"l", "r", "s", "w"}

	err = database.SetMailboxACL(user, mailbox, identifier, rights)
	if err != nil {
		t.Fatalf("SetMailboxACL failed: %v", err)
	}

	retrieved, err := database.GetMailboxACL(user, mailbox)
	if err != nil {
		t.Fatalf("GetMailboxACL failed: %v", err)
	}
	_ = retrieved // Just verify no error

	// Get non-existent ACL for nonexistent user
	_, err = database.GetMailboxACL("nobody@example.com", "INBOX")
	// May or may not error depending on implementation
	_ = err
}

func TestDBUIDOperations(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	domain := "example.com"
	user := "testuser"
	folder := "INBOX"

	// Get UIDValidity (should auto-create)
	validity, err := database.GetUIDValidity(domain, user, folder)
	if err != nil {
		t.Fatalf("GetUIDValidity failed: %v", err)
	}
	if validity == 0 {
		t.Error("expected non-zero UID validity")
	}

	// Get same validity again
	validity2, err := database.GetUIDValidity(domain, user, folder)
	if err != nil {
		t.Fatalf("GetUIDValidity (2nd) failed: %v", err)
	}
	if validity != validity2 {
		t.Errorf("expected same validity, got %d then %d", validity, validity2)
	}

	// Get UIDNext
	uid1, err := database.GetUIDNext(domain, user, folder)
	if err != nil {
		t.Fatalf("GetUIDNext failed: %v", err)
	}
	if uid1 == 0 {
		t.Error("expected non-zero UID")
	}

	// Next call should increment
	uid2, err := database.GetUIDNext(domain, user, folder)
	if err != nil {
		t.Fatalf("GetUIDNext (2nd) failed: %v", err)
	}
	if uid2 <= uid1 {
		t.Errorf("expected incrementing UID, got %d then %d", uid1, uid2)
	}
}
