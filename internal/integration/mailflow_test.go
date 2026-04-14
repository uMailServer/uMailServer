package integration

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/store"
	"github.com/umailserver/umailserver/internal/webhook"
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

// TestSMTPAuthentication tests SMTP AUTH LOGIN and PLAIN mechanisms
func TestSMTPAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip on Windows due to port binding issues
	if runtime.GOOS == "windows" {
		t.Skip("Skipping SMTP server test on Windows")
	}

	dataDir := t.TempDir()

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain and account
	domain := &db.DomainData{Name: "smtp.test", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email:        "user@smtp.test",
		LocalPart:    "user",
		Domain:       "smtp.test",
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create SMTP server
	smtpConfig := &smtp.Config{
		Hostname:       "smtp.test",
		MaxMessageSize: 10 * 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		RequireAuth:    true,
		IsSubmission:   true,
	}
	smtpServer := smtp.NewServer(smtpConfig, nil)

	// Set auth handler
	smtpServer.SetAuthHandler(func(username, password string) (bool, error) {
		parts := strings.Split(username, "@")
		if len(parts) != 2 {
			return false, nil
		}
		acc, err := database.GetAccount(parts[1], parts[0])
		if err != nil {
			return false, err
		}
		if !acc.IsActive {
			return false, nil
		}
		err = bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(password))
		return err == nil, nil
	})

	// Find free port and start server
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	go smtpServer.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port))
	time.Sleep(100 * time.Millisecond) // Wait for server to start

	defer smtpServer.Stop()

	t.Run("auth_login_success", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		// Read greeting
		line, _ := reader.ReadString('\n')
		if !strings.HasPrefix(line, "220") {
			t.Fatalf("expected 220 greeting, got: %s", line)
		}

		// EHLO
		fmt.Fprintf(writer, "EHLO test.client\r\n")
		writer.Flush()

		// Read EHLO response
		for {
			line, _ = reader.ReadString('\n')
			if strings.HasPrefix(line, "250 ") {
				break
			}
		}

		// AUTH LOGIN
		fmt.Fprintf(writer, "AUTH LOGIN\r\n")
		writer.Flush()
		line, _ = reader.ReadString('\n')
		if !strings.HasPrefix(line, "334") {
			t.Fatalf("expected 334, got: %s", line)
		}

		// Send username (base64 "user@smtp.test")
		fmt.Fprintf(writer, "dXNlckBzbXRwLnRlc3Q=\r\n")
		writer.Flush()
		line, _ = reader.ReadString('\n')
		if !strings.HasPrefix(line, "334") {
			t.Fatalf("expected 334, got: %s", line)
		}

		// Send password (base64 "testpass123")
		fmt.Fprintf(writer, "dGVzdHBhc3MxMjM=\r\n")
		writer.Flush()
		line, _ = reader.ReadString('\n')
		if !strings.HasPrefix(line, "235") {
			t.Fatalf("expected 235 auth success, got: %s", line)
		}

		// QUIT
		fmt.Fprintf(writer, "QUIT\r\n")
		writer.Flush()
	})

	t.Run("auth_login_failure_wrong_password", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		fmt.Fprintf(writer, "EHLO test.client\r\n")
		writer.Flush()
		for {
			line, _ := reader.ReadString('\n')
			if strings.HasPrefix(line, "250 ") {
				break
			}
		}

		fmt.Fprintf(writer, "AUTH LOGIN\r\n")
		writer.Flush()
		reader.ReadString('\n')

		fmt.Fprintf(writer, "dXNlckBzbXRwLnRlc3Q=\r\n") // user@smtp.test
		writer.Flush()
		reader.ReadString('\n')

		fmt.Fprintf(writer, "d3JvbmdwYXNzd29yZA==\r\n") // wrongpassword
		writer.Flush()
		line, _ := reader.ReadString('\n')
		if !strings.HasPrefix(line, "535") {
			t.Fatalf("expected 535 auth failure, got: %s", line)
		}
	})

	t.Run("mail_without_auth_fails", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		fmt.Fprintf(writer, "EHLO test.client\r\n")
		writer.Flush()
		for {
			line, _ := reader.ReadString('\n')
			if strings.HasPrefix(line, "250 ") {
				break
			}
		}

		// Try MAIL FROM without auth
		fmt.Fprintf(writer, "MAIL FROM:<user@smtp.test>\r\n")
		writer.Flush()
		line, _ := reader.ReadString('\n')
		if !strings.HasPrefix(line, "530") && !strings.HasPrefix(line, "550") && !strings.HasPrefix(line, "503") {
			t.Fatalf("expected error for MAIL without auth, got: %s", line)
		}
	})
}

// TestIMAPAuthentication tests IMAP LOGIN and mailbox operations
func TestIMAPAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip on Windows due to port binding issues
	if runtime.GOOS == "windows" {
		t.Skip("Skipping IMAP server test on Windows")
	}

	dataDir := t.TempDir()

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain and account
	domain := &db.DomainData{Name: "imap.test", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("imappass123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email:        "user@imap.test",
		LocalPart:    "user",
		Domain:       "imap.test",
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create mailstore
	mailstorePath := filepath.Join(dataDir, "imap.db")
	mailstore, err := imap.NewBboltMailstore(mailstorePath)
	if err != nil {
		t.Fatalf("failed to create mailstore: %v", err)
	}
	defer mailstore.Close()

	// Ensure default mailboxes
	mailstore.EnsureDefaultMailboxes("user@imap.test")

	// Find free port and create server with explicit address
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	imapConfig := &imap.Config{
		Addr:      fmt.Sprintf("127.0.0.1:%d", port),
		TLSConfig: nil,
		Logger:    nil,
	}
	imapServer := imap.NewServer(imapConfig, mailstore)

	// Set auth handler
	imapServer.SetAuthFunc(func(username, password string) (bool, error) {
		parts := strings.Split(username, "@")
		if len(parts) != 2 {
			return false, nil
		}
		acc, err := database.GetAccount(parts[1], parts[0])
		if err != nil {
			return false, err
		}
		if !acc.IsActive {
			return false, nil
		}
		err = bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(password))
		return err == nil, nil
	})

	// Start server
	go imapServer.Start()
	time.Sleep(100 * time.Millisecond)

	defer imapServer.Stop()

	t.Run("imap_login_success", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		// Read greeting
		line, _ := reader.ReadString('\n')
		if !strings.HasPrefix(line, "* OK") {
			t.Fatalf("expected * OK greeting, got: %s", line)
		}

		// Login
		fmt.Fprintf(writer, "A1 LOGIN \"user@imap.test\" \"imappass123\"\r\n")
		writer.Flush()
		line, _ = reader.ReadString('\n')
		if !strings.Contains(line, "A1 OK") {
			t.Fatalf("expected A1 OK, got: %s", line)
		}

		// Logout
		fmt.Fprintf(writer, "A2 LOGOUT\r\n")
		writer.Flush()
	})

	t.Run("imap_login_failure", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		fmt.Fprintf(writer, "A1 LOGIN \"user@imap.test\" \"wrongpassword\"\r\n")
		writer.Flush()
		line, _ := reader.ReadString('\n')
		if !strings.Contains(line, "A1 NO") {
			t.Fatalf("expected A1 NO, got: %s", line)
		}
	})

	t.Run("imap_list_mailboxes", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		// Login
		fmt.Fprintf(writer, "A1 LOGIN \"user@imap.test\" \"imappass123\"\r\n")
		writer.Flush()
		reader.ReadString('\n')

		// List mailboxes
		fmt.Fprintf(writer, "A2 LIST \"\" \"*\"\r\n")
		writer.Flush()

		var foundInbox bool
		for {
			line, _ := reader.ReadString('\n')
			if strings.Contains(line, "INBOX") {
				foundInbox = true
			}
			if strings.HasPrefix(line, "A2 OK") {
				break
			}
		}

		if !foundInbox {
			t.Error("INBOX not found in mailbox list")
		}

		fmt.Fprintf(writer, "A3 LOGOUT\r\n")
		writer.Flush()
	})

	t.Run("imap_select_inbox", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		// Login
		fmt.Fprintf(writer, "A1 LOGIN \"user@imap.test\" \"imappass123\"\r\n")
		writer.Flush()
		reader.ReadString('\n')

		// Select INBOX
		fmt.Fprintf(writer, "A2 SELECT INBOX\r\n")
		writer.Flush()

		var success bool
		for {
			line, _ := reader.ReadString('\n')
			if strings.HasPrefix(line, "A2 OK") {
				success = true
				break
			}
			if strings.HasPrefix(line, "A2 NO") {
				t.Fatalf("SELECT failed: %s", line)
			}
		}

		if !success {
			t.Error("SELECT INBOX failed")
		}

		fmt.Fprintf(writer, "A3 LOGOUT\r\n")
		writer.Flush()
	})
}

// TestWebhookDelivery tests webhook event triggering and delivery
func TestWebhookDelivery(t *testing.T) {
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

	// Create webhook manager
	webhookMgr := webhook.NewManager(database, "test-secret")
	webhookMgr.SetAllowPrivateIP(true) // Allow localhost for testing

	t.Run("webhook_event_delivery", func(t *testing.T) {
		// Create test server to receive webhook
		var receivedEvent string
		var receivedData string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedEvent = r.Header.Get("X-Webhook-Event")
			// Read body
			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			receivedData = string(buf[:n])
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Register webhook via HTTP handler
		body := fmt.Sprintf(`{"url": "%s", "events": ["mail.received"]}`, server.URL)
		req := httptest.NewRequest("POST", "/webhooks", strings.NewReader(body))
		w := httptest.NewRecorder()
		webhookMgr.HTTPHandler(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Trigger event
		webhookMgr.Trigger("mail.received", map[string]string{
			"from": "sender@example.com",
			"to":   "recipient@test.com",
		})

		// Wait for delivery
		time.Sleep(300 * time.Millisecond)

		if receivedEvent != "mail.received" {
			t.Errorf("expected event 'mail.received', got '%s'", receivedEvent)
		}
		if !strings.Contains(receivedData, "sender@example.com") {
			t.Error("webhook data doesn't contain sender")
		}
	})

	t.Run("webhook_event_filtering", func(t *testing.T) {
		receivedEvents := make(map[string]bool)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			event := r.Header.Get("X-Webhook-Event")
			receivedEvents[event] = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Register webhook that only subscribes to mail.sent
		body := fmt.Sprintf(`{"url": "%s", "events": ["mail.sent"]}`, server.URL)
		req := httptest.NewRequest("POST", "/webhooks", strings.NewReader(body))
		w := httptest.NewRecorder()
		webhookMgr.HTTPHandler(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}

		// Trigger multiple events
		webhookMgr.Trigger("mail.received", nil)
		webhookMgr.Trigger("mail.sent", nil)
		webhookMgr.Trigger("delivery.failed", nil)

		time.Sleep(300 * time.Millisecond)

		if receivedEvents["mail.received"] {
			t.Error("should not have received mail.received event")
		}
		if !receivedEvents["mail.sent"] {
			t.Error("should have received mail.sent event")
		}
		if receivedEvents["delivery.failed"] {
			t.Error("should not have received delivery.failed event")
		}
	})

	t.Run("webhook_signature_with_secret", func(t *testing.T) {
		var receivedSig string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedSig = r.Header.Get("X-Webhook-Signature")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		body := fmt.Sprintf(`{"url": "%s", "events": ["*"]}`, server.URL)
		req := httptest.NewRequest("POST", "/webhooks", strings.NewReader(body))
		w := httptest.NewRecorder()
		webhookMgr.HTTPHandler(w, req)

		webhookMgr.Trigger("test.event", map[string]string{"key": "value"})

		time.Sleep(300 * time.Millisecond)

		if receivedSig == "" {
			t.Error("expected X-Webhook-Signature header when secret is configured")
		}
	})

	t.Run("webhook_list_endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/webhooks", nil)
		w := httptest.NewRecorder()
		webhookMgr.HTTPHandler(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		if !strings.Contains(w.Body.String(), "webhooks") {
			t.Error("response should contain 'webhooks' key")
		}
	})
}

// TestFullMailFlow tests the complete mail flow from SMTP submission to IMAP retrieval
func TestFullMailFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip on Windows due to port binding issues
	if runtime.GOOS == "windows" {
		t.Skip("Skipping full mail flow test on Windows")
	}

	dataDir := t.TempDir()
	msgDir := filepath.Join(dataDir, "messages")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		t.Fatalf("failed to create messages dir: %v", err)
	}

	// Create database
	dbPath := filepath.Join(dataDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create domain and account
	domain := &db.DomainData{Name: "fullflow.test", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("flowpass123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email:        "user@fullflow.test",
		LocalPart:    "user",
		Domain:       "fullflow.test",
		PasswordHash: string(hash),
		IsActive:     true,
		QuotaLimit:   100 * 1024 * 1024,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create maildir store
	maildirStore := store.NewMaildirStore(msgDir)

	// Create IMAP mailstore
	mailstorePath := filepath.Join(dataDir, "imap.db")
	mailstore, err := imap.NewBboltMailstore(mailstorePath)
	if err != nil {
		t.Fatalf("failed to create mailstore: %v", err)
	}
	defer mailstore.Close()

	mailstore.EnsureDefaultMailboxes("user@fullflow.test")

	// Create IMAP server with explicit port
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	imapPort := ln2.Addr().(*net.TCPAddr).Port
	ln2.Close()

	imapConfig := &imap.Config{
		Addr:      fmt.Sprintf("127.0.0.1:%d", imapPort),
		TLSConfig: nil,
		Logger:    nil,
	}
	imapServer := imap.NewServer(imapConfig, mailstore)
	imapServer.SetAuthFunc(func(username, password string) (bool, error) {
		parts := strings.Split(username, "@")
		if len(parts) != 2 {
			return false, nil
		}
		acc, err := database.GetAccount(parts[1], parts[0])
		if err != nil {
			return false, err
		}
		if !acc.IsActive {
			return false, nil
		}
		err = bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(password))
		return err == nil, nil
	})

	go imapServer.Start()
	time.Sleep(100 * time.Millisecond)
	defer imapServer.Stop()

	// Create SMTP server
	smtpConfig := &smtp.Config{
		Hostname:       "fullflow.test",
		MaxMessageSize: 10 * 1024 * 1024,
		MaxRecipients:  100,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		RequireAuth:    true,
		IsSubmission:   true,
	}
	smtpServer := smtp.NewServer(smtpConfig, nil)

	// Set up delivery that goes to both queue and maildir
	smtpServer.SetAuthHandler(func(username, password string) (bool, error) {
		parts := strings.Split(username, "@")
		if len(parts) != 2 {
			return false, nil
		}
		acc, err := database.GetAccount(parts[1], parts[0])
		if err != nil {
			return false, err
		}
		if !acc.IsActive {
			return false, nil
		}
		err = bcrypt.CompareHashAndPassword([]byte(acc.PasswordHash), []byte(password))
		return err == nil, nil
	})

	smtpServer.SetDeliveryHandler(func(from string, to []string, data []byte) error {
		// Deliver to maildir for local recipients
		for _, recipient := range to {
			if strings.HasSuffix(recipient, "@fullflow.test") {
				localPart := strings.Split(recipient, "@")[0]
				_, err := maildirStore.Deliver("fullflow.test", localPart, "INBOX", data)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})

	// Find port and start SMTP server
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	smtpPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	go smtpServer.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", smtpPort))
	time.Sleep(100 * time.Millisecond)
	defer smtpServer.Stop()

	t.Run("submit_via_smtp_retrieve_via_imap", func(t *testing.T) {
		// Step 1: Submit email via SMTP
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", smtpPort))
		if err != nil {
			t.Fatalf("failed to connect to SMTP: %v", err)
		}

		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)

		reader.ReadString('\n') // greeting

		fmt.Fprintf(writer, "EHLO test.client\r\n")
		writer.Flush()
		for {
			line, _ := reader.ReadString('\n')
			if strings.HasPrefix(line, "250 ") {
				break
			}
		}

		// Authenticate
		fmt.Fprintf(writer, "AUTH LOGIN\r\n")
		writer.Flush()
		reader.ReadString('\n')
		fmt.Fprintf(writer, "dXNlckBmdWxsZmxvdy50ZXN0\r\n") // user@fullflow.test
		writer.Flush()
		reader.ReadString('\n')
		fmt.Fprintf(writer, "Zmxvd3Bhc3MxMjM=\r\n") // flowpass123
		writer.Flush()
		reader.ReadString('\n')

		// Send message
		fmt.Fprintf(writer, "MAIL FROM:<user@fullflow.test>\r\n")
		writer.Flush()
		reader.ReadString('\n')

		fmt.Fprintf(writer, "RCPT TO:<user@fullflow.test>\r\n")
		writer.Flush()
		reader.ReadString('\n')

		fmt.Fprintf(writer, "DATA\r\n")
		writer.Flush()
		reader.ReadString('\n')

		subject := "Integration Test Email " + time.Now().Format(time.RFC3339)
		fmt.Fprintf(writer, "From: user@fullflow.test\r\n")
		fmt.Fprintf(writer, "To: user@fullflow.test\r\n")
		fmt.Fprintf(writer, "Subject: %s\r\n", subject)
		fmt.Fprintf(writer, "\r\n")
		fmt.Fprintf(writer, "This is a test message from the full flow integration test.\r\n")
		fmt.Fprintf(writer, ".\r\n")
		writer.Flush()
		reader.ReadString('\n')

		fmt.Fprintf(writer, "QUIT\r\n")
		writer.Flush()
		conn.Close()

		// Step 2: Wait for delivery
		time.Sleep(200 * time.Millisecond)

		// Step 3: Retrieve via IMAP
		imapConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", imapPort))
		if err != nil {
			t.Fatalf("failed to connect to IMAP: %v", err)
		}
		defer imapConn.Close()

		imapReader := bufio.NewReader(imapConn)
		imapWriter := bufio.NewWriter(imapConn)

		imapReader.ReadString('\n') // greeting

		// Login
		fmt.Fprintf(imapWriter, "A1 LOGIN \"user@fullflow.test\" \"flowpass123\"\r\n")
		imapWriter.Flush()
		imapReader.ReadString('\n')

		// Select INBOX
		fmt.Fprintf(imapWriter, "A2 SELECT INBOX\r\n")
		imapWriter.Flush()

		var msgCount int
		for {
			line, _ := imapReader.ReadString('\n')
			if strings.Contains(line, "EXISTS") {
				// Parse message count
				fmt.Sscanf(line, "* %d EXISTS", &msgCount)
			}
			if strings.HasPrefix(line, "A2 OK") {
				break
			}
		}

		if msgCount == 0 {
			t.Error("no messages found in INBOX after SMTP delivery")
		}

		// Logout
		fmt.Fprintf(imapWriter, "A3 LOGOUT\r\n")
		imapWriter.Flush()
	})
}
