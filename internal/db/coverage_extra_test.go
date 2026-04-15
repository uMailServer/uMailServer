package db

import (
	"fmt"
	"testing"
	"time"
)

func TestDBForEach(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// ForEach on empty bucket
	count := 0
	_ = database.ForEach(BucketDomains, func(key string, value []byte) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 items in empty bucket, got %d", count)
	}

	// Create domain and iterate
	_ = database.CreateDomain(&DomainData{Name: "example.com", MaxAccounts: 10})
	_ = database.CreateDomain(&DomainData{Name: "test.org", MaxAccounts: 5})

	domainCount := 0
	_ = database.ForEach(BucketDomains, func(key string, value []byte) error {
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
	_ = database.CreateAccount(&AccountData{Email: "user1@example.com", Domain: "example.com", PasswordHash: "hash"})
	_ = database.CreateAccount(&AccountData{Email: "admin@test.org", Domain: "test.org", PasswordHash: "hash"})

	count := 0
	_ = database.ForEachPrefix(BucketAccounts, "example.com/", func(key string, value []byte) error {
		count++
		return nil
	})
	if count != 1 {
		t.Errorf("expected 1 account for example.com, got %d", count)
	}

	// Empty prefix
	emptyCount := 0
	_ = database.ForEachPrefix(BucketAccounts, "nonexistent.com/", func(key string, value []byte) error {
		emptyCount++
		return nil
	})
	if emptyCount != 0 {
		t.Errorf("expected 0 accounts for nonexistent.com, got %d", emptyCount)
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

// TestStoreRevokedToken tests token revocation storage
func TestStoreRevokedToken(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	tokenHash := "testtokenhash123"
	expiry := time.Now().Add(1 * time.Hour)

	// Store revoked token
	err = database.StoreRevokedToken(tokenHash, expiry)
	if err != nil {
		t.Errorf("StoreRevokedToken failed: %v", err)
	}

	// Verify token is revoked
	revoked, err := database.IsTokenRevoked(tokenHash)
	if err != nil {
		t.Errorf("IsTokenRevoked failed: %v", err)
	}
	if !revoked {
		t.Error("expected token to be revoked")
	}

	// Check non-existent token
	notRevoked, err := database.IsTokenRevoked("nonexistenttoken")
	if err != nil {
		t.Errorf("IsTokenRevoked failed: %v", err)
	}
	if notRevoked {
		t.Error("expected non-existent token to not be revoked")
	}
}

// TestIsTokenRevokedWithExpiredToken tests expired token cleanup
func TestIsTokenRevokedWithExpiredToken(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Store expired token
	expiredHash := "expiredtokenhash"
	expiredTime := time.Now().Add(-1 * time.Hour)
	err = database.StoreRevokedToken(expiredHash, expiredTime)
	if err != nil {
		t.Errorf("StoreRevokedToken failed: %v", err)
	}

	// Check expired token - should be removed and return not revoked
	revoked, err := database.IsTokenRevoked(expiredHash)
	if err != nil {
		t.Errorf("IsTokenRevoked failed: %v", err)
	}
	if revoked {
		t.Error("expected expired token to be cleaned up and not revoked")
	}
}

// TestCleanupRevokedTokens tests explicit cleanup of expired tokens
func TestCleanupRevokedTokens(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Store mix of valid and expired tokens
	validHash := "validtoken"
	expiredHash := "expiredtoken"

	err = database.StoreRevokedToken(validHash, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Errorf("StoreRevokedToken failed: %v", err)
	}

	err = database.StoreRevokedToken(expiredHash, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Errorf("StoreRevokedToken failed: %v", err)
	}

	// Cleanup
	err = database.CleanupRevokedTokens()
	if err != nil {
		t.Errorf("CleanupRevokedTokens failed: %v", err)
	}

	// Verify expired token is removed
	revoked, _ := database.IsTokenRevoked(expiredHash)
	if revoked {
		t.Error("expected expired token to be removed after cleanup")
	}

	// Verify valid token still exists
	validRevoked, _ := database.IsTokenRevoked(validHash)
	if !validRevoked {
		t.Error("expected valid token to still be revoked after cleanup")
	}
}

// TestAliasOperations tests alias CRUD operations
func TestAliasOperationsExtra(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Create a domain first
	_ = database.CreateDomain(&DomainData{Name: "example.com", MaxAccounts: 10})

	// Test CreateAlias
	alias := &AliasData{
		Alias:     "info@example.com",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	err = database.CreateAlias(alias)
	if err != nil {
		t.Errorf("CreateAlias failed: %v", err)
	}

	// Test ListAliases
	aliases, err := database.ListAliases()
	if err != nil {
		t.Errorf("ListAliases failed: %v", err)
	}
	if len(aliases) != 1 {
		t.Errorf("expected 1 alias, got %d", len(aliases))
	}
	if len(aliases) > 0 && aliases[0].Alias != "info@example.com" {
		t.Errorf("expected alias info@example.com, got %s", aliases[0].Alias)
	}

	// Test UpdateAlias
	alias.Target = "admin@example.com"
	err = database.UpdateAlias(alias)
	if err != nil {
		t.Errorf("UpdateAlias failed: %v", err)
	}

	// Test DeleteAlias - pass full alias (info@example.com), not just local part
	err = database.DeleteAlias("example.com", "info@example.com")
	if err != nil {
		t.Errorf("DeleteAlias failed: %v", err)
	}

	// Verify deletion
	deletedAliases, _ := database.ListAliases()
	if len(deletedAliases) != 0 {
		t.Errorf("expected 0 aliases after deletion, got %d", len(deletedAliases))
	}
}

// TestListAliasesEmpty tests listing aliases when none exist
func TestListAliasesEmpty(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// List aliases for empty database
	aliases, err := database.ListAliases()
	if err != nil {
		t.Errorf("ListAliases failed: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases for empty database, got %d", len(aliases))
	}
}

// TestIncrementQuota tests quota increment functionality
func TestIncrementQuotaExtra(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Create domain and account
	_ = database.CreateDomain(&DomainData{Name: "example.com", MaxAccounts: 10})
	_ = database.CreateAccount(&AccountData{Email: "user@example.com", LocalPart: "user", Domain: "example.com", PasswordHash: "hash"})

	// Increment quota by 1000 bytes
	err = database.IncrementQuota("example.com", "user", 1000)
	if err != nil {
		t.Errorf("IncrementQuota failed: %v", err)
	}

	// Increment again
	err = database.IncrementQuota("example.com", "user", 500)
	if err != nil {
		t.Errorf("IncrementQuota failed: %v", err)
	}
}

// TestIncrementQuotaNonExistentAccount tests increment for non-existent account
func TestIncrementQuotaNonExistentAccount(t *testing.T) {
	database, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer database.Close()

	// Try to increment quota for non-existent account - should return error
	err = database.IncrementQuota("example.com", "nonexistent", 1000)
	if err == nil {
		t.Error("expected error for non-existent account")
	}
}
