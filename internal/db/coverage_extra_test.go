package db

import (
	"fmt"
	"testing"
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
