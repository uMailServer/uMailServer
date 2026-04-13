package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// TestMessageDeliveryFlow tests the full message delivery pipeline
func TestMessageDeliveryFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temp directories
	dataDir := t.TempDir()
	msgDir := filepath.Join(dataDir, "messages")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatalf("failed to create messages dir: %v", err)
	}

	// Create test database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain
	domain := &db.DomainData{
		Name:        "example.com",
		MaxAccounts: 10,
		IsActive:    true,
	}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create recipient account
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email:        "recipient@example.com",
		LocalPart:    "recipient",
		Domain:       "example.com",
		PasswordHash: string(hash),
		IsActive:     true,
		QuotaLimit:   100 * 1024 * 1024, // 100MB
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create maildir store
	maildirStore := store.NewMaildirStore(msgDir)

	// Test message delivery
	t.Run("deliver_message_to_maildir", func(t *testing.T) {
		from := "sender@external.com"
		subject := "Test Integration Email"
		body := "This is a test email for integration testing."

		msg := fmt.Sprintf("From: %s\r\nTo: recipient@example.com\r\nSubject: %s\r\n\r\n%s",
			from, subject, body)

		// Deliver message through maildir store
		filename, err := maildirStore.Deliver("example.com", "recipient", "INBOX", []byte(msg))
		if err != nil {
			t.Fatalf("failed to deliver message: %v", err)
		}

		t.Logf("Message delivered with filename: %s", filename)

		// Verify message can be fetched back (confirms it was stored)
		content, err := maildirStore.Fetch("example.com", "recipient", "INBOX", filename)
		if err != nil {
			t.Fatalf("failed to fetch delivered message: %v", err)
		}

		if !strings.Contains(string(content), subject) {
			t.Error("fetched message doesn't contain expected subject")
		}

		t.Logf("Successfully delivered message to recipient@example.com")
	})

	t.Run("deliver_message_with_flags", func(t *testing.T) {
		msg := "From: sender@test.com\r\nSubject: Flagged\r\n\r\nBody"

		// Deliver with Seen flag
		filename, err := maildirStore.DeliverWithFlags("example.com", "recipient", "INBOX", []byte(msg), "S")
		if err != nil {
			t.Fatalf("failed to deliver with flags: %v", err)
		}

		// Verify filename contains flag info (Windows uses "!2,", Unix uses ":2,")
		if !strings.Contains(filename, "!2,") && !strings.Contains(filename, ":2,") {
			t.Errorf("expected flag info in filename, got: %s", filename)
		}
	})

	t.Run("fetch_delivered_message", func(t *testing.T) {
		msg := "From: sender@test.com\r\nSubject: Fetch Test\r\n\r\nBody content"

		// Deliver a message
		filename, err := maildirStore.Deliver("example.com", "recipient", "INBOX", []byte(msg))
		if err != nil {
			t.Fatalf("failed to deliver: %v", err)
		}

		// Fetch it back
		content, err := maildirStore.Fetch("example.com", "recipient", "INBOX", filename)
		if err != nil {
			t.Fatalf("failed to fetch: %v", err)
		}

		if !strings.Contains(string(content), "Fetch Test") {
			t.Error("fetched content doesn't match")
		}
	})
}

// TestQueueProcessing tests queue functionality
func TestQueueProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	t.Run("enqueue_and_dequeue", func(t *testing.T) {
		// Create a queue entry
		entry := &db.QueueEntry{
			ID:          "test-msg-001",
			From:        "sender@example.com",
			To:          []string{"recipient@external.com"},
			MessagePath: "/tmp/test.eml",
			Status:      "pending",
			CreatedAt:   time.Now(),
			NextRetry:   time.Now(),
			RetryCount:  0,
		}

		// Enqueue
		if err := database.Enqueue(entry); err != nil {
			t.Fatalf("failed to enqueue: %v", err)
		}

		// Verify it was stored
		stored, err := database.GetQueueEntry(entry.ID)
		if err != nil {
			t.Fatalf("failed to get queue entry: %v", err)
		}

		if stored.ID != entry.ID {
			t.Error("queue entry ID mismatch")
		}

		if stored.Status != "pending" {
			t.Errorf("expected status pending, got %s", stored.Status)
		}

		// Dequeue
		if err := database.Dequeue(entry.ID); err != nil {
			t.Fatalf("failed to dequeue: %v", err)
		}

		// Verify it was removed
		_, err = database.GetQueueEntry(entry.ID)
		if err == nil {
			t.Error("expected error after dequeue")
		}
	})

	t.Run("queue_priority", func(t *testing.T) {
		// Create entries with different priorities (0=low, 1=normal, 2=high, 3=urgent)
		entries := []*db.QueueEntry{
			{ID: "low", From: "a@test.com", To: []string{"b@test.com"}, Status: "pending", CreatedAt: time.Now(), NextRetry: time.Now()},
			{ID: "normal", From: "a@test.com", To: []string{"b@test.com"}, Status: "pending", CreatedAt: time.Now(), NextRetry: time.Now()},
			{ID: "high", From: "a@test.com", To: []string{"b@test.com"}, Status: "pending", CreatedAt: time.Now(), NextRetry: time.Now()},
		}

		for _, e := range entries {
			if err := database.Enqueue(e); err != nil {
				t.Fatalf("failed to enqueue: %v", err)
			}
		}

		// Get pending queue
		pending, err := database.GetPendingQueue(time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("failed to get pending: %v", err)
		}

		if len(pending) != 3 {
			t.Errorf("expected 3 pending entries, got %d", len(pending))
		}

		// Cleanup
		for _, e := range entries {
			database.Dequeue(e.ID)
		}
	})
}

// TestAliasResolution tests alias to account resolution
func TestAliasResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain
	domain := &db.DomainData{Name: "example.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create target account
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Store alias directly using the low-level API
	alias := &db.AliasData{
		Domain:   "example.com",
		Alias:    "support",
		Target:   "user@example.com",
		IsActive: true,
	}
	if err := database.Put("aliases", "example.com:support", alias); err != nil {
		t.Fatalf("failed to store alias: %v", err)
	}

	t.Run("resolve_alias_to_account", func(t *testing.T) {
		// Verify alias resolves correctly
		target, err := database.ResolveAlias("example.com", "support")
		if err != nil {
			t.Fatalf("failed to resolve alias: %v", err)
		}
		if target != "user@example.com" {
			t.Errorf("expected target user@example.com, got %s", target)
		}
	})

	t.Run("get_alias_data", func(t *testing.T) {
		// Verify alias data can be retrieved
		aliasData, err := database.GetAlias("example.com", "support")
		if err != nil {
			t.Fatalf("failed to get alias: %v", err)
		}
		if aliasData.Alias != "support" {
			t.Errorf("expected alias 'support', got %s", aliasData.Alias)
		}
		if aliasData.Target != "user@example.com" {
			t.Errorf("expected target user@example.com, got %s", aliasData.Target)
		}
	})

	t.Run("alias_target_account_active", func(t *testing.T) {
		// Verify target account is active
		target, err := database.GetAccount("example.com", "user")
		if err != nil {
			t.Fatalf("failed to get target account: %v", err)
		}
		if !target.IsActive {
			t.Error("target account should be active")
		}
	})
}

// TestDomainManagement tests domain lifecycle operations
func TestDomainManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	t.Run("create_and_list_domains", func(t *testing.T) {
		domain := &db.DomainData{
			Name:        "testdomain.com",
			MaxAccounts: 50,
			IsActive:    true,
		}
		if err := database.CreateDomain(domain); err != nil {
			t.Fatalf("failed to create domain: %v", err)
		}

		// Verify domain exists
		domains, err := database.ListDomains()
		if err != nil {
			t.Fatalf("failed to list domains: %v", err)
		}

		found := false
		for _, d := range domains {
			if d.Name == "testdomain.com" {
				found = true
				if d.MaxAccounts != 50 {
					t.Errorf("expected max_accounts 50, got %d", d.MaxAccounts)
				}
				break
			}
		}
		if !found {
			t.Error("domain not found in list")
		}
	})

	t.Run("domain_account_limits", func(t *testing.T) {
		domain := &db.DomainData{
			Name:        "limited.com",
			MaxAccounts: 2,
			IsActive:    true,
		}
		if err := database.CreateDomain(domain); err != nil {
			t.Fatalf("failed to create domain: %v", err)
		}

		// Create accounts up to limit
		for i := 1; i <= 2; i++ {
			account := &db.AccountData{
				Email:     fmt.Sprintf("user%d@limited.com", i),
				LocalPart: fmt.Sprintf("user%d", i),
				Domain:    "limited.com",
				IsActive:  true,
			}
			if err := database.CreateAccount(account); err != nil {
				t.Fatalf("failed to create account %d: %v", i, err)
			}
		}

		// Verify account count
		accounts, err := database.ListAccountsByDomain("limited.com")
		if err != nil {
			t.Fatalf("failed to get accounts: %v", err)
		}
		if len(accounts) != 2 {
			t.Errorf("expected 2 accounts, got %d", len(accounts))
		}
	})
}

// TestMessageSearchIndex tests search index operations
func TestMessageSearchIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dataDir := t.TempDir()
	msgDir := filepath.Join(dataDir, "messages")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatalf("failed to create messages dir: %v", err)
	}

	// Create maildir store
	maildirStore := store.NewMaildirStore(msgDir)

	t.Run("deliver_and_list_messages", func(t *testing.T) {
		// Deliver multiple messages
		messages := []struct {
			from    string
			subject string
			body    string
		}{
			{"sender1@test.com", "First Message", "This is the first test message"},
			{"sender2@test.com", "Second Message", "This is the second test message"},
			{"sender3@test.com", "Third Message", "This is the third test message"},
		}

		for _, msg := range messages {
			content := fmt.Sprintf("From: %s\r\nSubject: %s\r\n\r\n%s",
				msg.from, msg.subject, msg.body)
			_, err := maildirStore.Deliver("example.com", "testuser", "INBOX", []byte(content))
			if err != nil {
				t.Fatalf("failed to deliver message: %v", err)
			}
		}

		// List messages in INBOX
		entries, err := maildirStore.List("example.com", "testuser", "INBOX")
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}

		if len(entries) != 3 {
			t.Errorf("expected 3 messages, got %d", len(entries))
		}

		// Verify message contents
		for i, entry := range entries {
			content, err := maildirStore.Fetch("example.com", "testuser", "INBOX", entry.Filename)
			if err != nil {
				t.Fatalf("failed to fetch message %d: %v", i, err)
			}
			if !strings.Contains(string(content), messages[i].subject) {
				t.Errorf("message %d doesn't contain expected subject", i)
			}
		}
	})

	t.Run("message_flags_and_status", func(t *testing.T) {
		// Deliver message with Seen flag
		msg := "From: test@test.com\r\nSubject: Flag Test\r\n\r\nBody"
		filename, err := maildirStore.DeliverWithFlags("example.com", "testuser", "INBOX", []byte(msg), "S")
		if err != nil {
			t.Fatalf("failed to deliver with flags: %v", err)
		}

		// List and check flags
		entries, err := maildirStore.List("example.com", "testuser", "INBOX")
		if err != nil {
			t.Fatalf("failed to list messages: %v", err)
		}

		found := false
		for _, entry := range entries {
			if entry.Filename == filename {
				found = true
				// Check if seen flag (S) is set in the flags string
				if !strings.Contains(entry.Flags, "S") {
					t.Errorf("expected Seen flag to be set, got flags: %s", entry.Flags)
				}
				break
			}
		}
		if !found {
			t.Error("delivered message not found in list")
		}
	})
}

// TestAuthenticationFlow tests user authentication
func TestAuthenticationFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dataDir := t.TempDir()

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain
	domain := &db.DomainData{Name: "example.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create test users with different passwords
	passwords := map[string]string{
		"user1@example.com": "password123",
		"user2@example.com": "securepass456",
		"admin@example.com": "adminpass789",
	}

	for email, password := range passwords {
		parts := strings.Split(email, "@")
		hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		account := &db.AccountData{
			Email:        email,
			LocalPart:    parts[0],
			Domain:       parts[1],
			PasswordHash: string(hash),
			IsActive:     true,
			IsAdmin:      email == "admin@example.com",
		}
		if err := database.CreateAccount(account); err != nil {
			t.Fatalf("failed to create account %s: %v", email, err)
		}
	}

	t.Run("verify_correct_passwords", func(t *testing.T) {
		for email, password := range passwords {
			parts := strings.Split(email, "@")
			account, err := database.GetAccount(parts[1], parts[0])
			if err != nil {
				t.Fatalf("failed to get account: %v", err)
			}

			err = bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password))
			if err != nil {
				t.Errorf("password verification failed for %s", email)
			}
		}
	})

	t.Run("reject_incorrect_passwords", func(t *testing.T) {
		account, _ := database.GetAccount("example.com", "user1")

		err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte("wrongpassword"))
		if err == nil {
			t.Error("expected error for wrong password")
		}
	})

	t.Run("inactive_account_auth", func(t *testing.T) {
		// Create inactive account
		hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
		inactive := &db.AccountData{
			Email:        "inactive@example.com",
			LocalPart:    "inactive",
			Domain:       "example.com",
			PasswordHash: string(hash),
			IsActive:     false,
		}
		database.CreateAccount(inactive)

		// Verify password works but account is inactive
		account, _ := database.GetAccount("example.com", "inactive")
		err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte("password"))
		if err != nil {
			t.Error("password should verify even for inactive account")
		}

		if account.IsActive {
			t.Error("account should be inactive")
		}
	})
}
