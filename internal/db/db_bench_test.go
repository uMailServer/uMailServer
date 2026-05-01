package db

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// BenchmarkCreateAccount measures account creation performance
func BenchmarkCreateAccount(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create domain
	domain := &DomainData{Name: "bench.com", MaxAccounts: 10000, IsActive: true}
	if err := db.CreateDomain(domain); err != nil {
		b.Fatalf("failed to create domain: %v", err)
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		account := &AccountData{
			Email:        fmt.Sprintf("user%d@bench.com", i),
			LocalPart:    fmt.Sprintf("user%d", i),
			Domain:       "bench.com",
			PasswordHash: string(hash),
			IsActive:     true,
			QuotaLimit:   100 * 1024 * 1024,
		}
		if err := db.CreateAccount(account); err != nil {
			b.Fatalf("create account failed: %v", err)
		}
	}
}

// BenchmarkGetAccount measures account lookup performance
func BenchmarkGetAccount(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create domain
	domain := &DomainData{Name: "bench.com", MaxAccounts: 10000, IsActive: true}
	if err := db.CreateDomain(domain); err != nil {
		b.Fatalf("failed to create domain: %v", err)
	}

	// Pre-create accounts
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	for i := 0; i < 1000; i++ {
		account := &AccountData{
			Email:        fmt.Sprintf("user%d@bench.com", i),
			LocalPart:    fmt.Sprintf("user%d", i),
			Domain:       "bench.com",
			PasswordHash: string(hash),
			IsActive:     true,
		}
		if err := db.CreateAccount(account); err != nil {
			b.Fatalf("setup create account failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetAccount("bench.com", "user500")
		if err != nil {
			b.Fatalf("get account failed: %v", err)
		}
	}
}

// BenchmarkPasswordVerify measures bcrypt password verification
func BenchmarkPasswordVerify(b *testing.B) {
	password := "password123"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := bcrypt.CompareHashAndPassword(hash, []byte(password))
		if err != nil {
			b.Fatalf("password verify failed: %v", err)
		}
	}
}

// BenchmarkPasswordVerifyHighCost measures bcrypt with high cost
func BenchmarkPasswordVerifyHighCost(b *testing.B) {
	password := "password123"
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost+10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := bcrypt.CompareHashAndPassword(hash, []byte(password))
		if err != nil {
			b.Fatalf("password verify failed: %v", err)
		}
	}
}

// BenchmarkEnqueue measures queue entry creation
func BenchmarkEnqueue(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	baseEntry := &QueueEntry{
		From:        "sender@example.com",
		To:          []string{"recipient@example.com"},
		MessagePath: "/tmp/test.eml",
		Status:      "pending",
		CreatedAt:   time.Now(),
		NextRetry:   time.Now(),
		RetryCount:  0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := *baseEntry
		entry.ID = fmt.Sprintf("msg-%d", i)
		if err := db.Enqueue(&entry); err != nil {
			b.Fatalf("enqueue failed: %v", err)
		}
	}
}

// BenchmarkGetPendingQueue measures pending queue retrieval
func BenchmarkGetPendingQueue(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Pre-populate queue
	for i := 0; i < 1000; i++ {
		entry := &QueueEntry{
			ID:          fmt.Sprintf("msg-%d", i),
			From:        "sender@example.com",
			To:          []string{"recipient@example.com"},
			MessagePath: "/tmp/test.eml",
			Status:      "pending",
			CreatedAt:   time.Now(),
			NextRetry:   time.Now(),
			RetryCount:  0,
		}
		if err := db.Enqueue(entry); err != nil {
			b.Fatalf("setup enqueue failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetPendingQueue(time.Now().Add(time.Hour))
		if err != nil {
			b.Fatalf("get pending queue failed: %v", err)
		}
	}
}

// BenchmarkGetPendingQueueRealistic models a healthier production queue: most
// entries are in a terminal state (delivered/bounced/failed) lingering until
// cleanup, with only a small minority actually pending. The sweeper hits this
// shape every 30s, so the cost of skipping non-pending entries dominates.
func BenchmarkGetPendingQueueRealistic(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// 1000 entries: 50 pending, 950 in terminal states (skewed roughly the way
	// a queue with sticky completed entries would look).
	statuses := []string{"delivered", "bounced", "failed", "delivered", "delivered"}
	for i := 0; i < 1000; i++ {
		status := "pending"
		if i >= 50 {
			status = statuses[i%len(statuses)]
		}
		entry := &QueueEntry{
			ID:          fmt.Sprintf("msg-%d", i),
			From:        "sender@example.com",
			To:          []string{"recipient@example.com"},
			MessagePath: "/tmp/test.eml",
			Status:      status,
			CreatedAt:   time.Now(),
			NextRetry:   time.Now(),
			RetryCount:  0,
		}
		if err := db.Enqueue(entry); err != nil {
			b.Fatalf("setup enqueue failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetPendingQueue(time.Now().Add(time.Hour))
		if err != nil {
			b.Fatalf("get pending queue failed: %v", err)
		}
	}
}

// BenchmarkListDomains measures domain listing
func BenchmarkListDomains(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Pre-create domains
	for i := 0; i < 100; i++ {
		domain := &DomainData{
			Name:        fmt.Sprintf("domain%d.com", i),
			MaxAccounts: 100,
			IsActive:    true,
		}
		if err := db.CreateDomain(domain); err != nil {
			b.Fatalf("setup create domain failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ListDomains()
		if err != nil {
			b.Fatalf("list domains failed: %v", err)
		}
	}
}

// BenchmarkListAccountsByDomain measures account listing by domain
func BenchmarkListAccountsByDomain(b *testing.B) {
	tempDir := b.TempDir()
	db, err := Open(tempDir + "/bench.db")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Create domain
	domain := &DomainData{Name: "bench.com", MaxAccounts: 10000, IsActive: true}
	if err := db.CreateDomain(domain); err != nil {
		b.Fatalf("failed to create domain: %v", err)
	}

	// Pre-create accounts
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	for i := 0; i < 1000; i++ {
		account := &AccountData{
			Email:        fmt.Sprintf("user%d@bench.com", i),
			LocalPart:    fmt.Sprintf("user%d", i),
			Domain:       "bench.com",
			PasswordHash: string(hash),
			IsActive:     true,
		}
		if err := db.CreateAccount(account); err != nil {
			b.Fatalf("setup create account failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ListAccountsByDomain("bench.com")
		if err != nil {
			b.Fatalf("list accounts failed: %v", err)
		}
	}
}
