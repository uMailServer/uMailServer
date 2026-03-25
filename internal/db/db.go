package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"
)

// Bucket names
const (
	BucketAccounts     = "accounts"
	BucketDomains      = "domains"
	BucketQueue        = "queue"
	BucketSessions     = "sessions"
	BucketRateLimits   = "ratelimits"
	BucketSpam         = "spam"
	BucketBlocklist    = "blocklist"
	BucketMetrics      = "metrics"
	BucketUIDValidity  = "uidvalidity"
	BucketUIDNext      = "uidnext"
	BucketMessageMeta  = "messagemeta"
	BucketIndex        = "index"
	BucketContacts     = "contacts"
	BucketAliases      = "aliases"
)

// DB wraps bbolt database
type DB struct {
	bolt *bbolt.DB
}

// AccountData holds account information
type AccountData struct {
	Email          string    `json:"email"`
	LocalPart      string    `json:"local_part"`
	Domain         string    `json:"domain"`
	PasswordHash   string    `json:"password_hash"`
	TOTPSecret     string    `json:"totp_secret,omitempty"`
	TOTPEnabled    bool      `json:"totp_enabled"`
	QuotaUsed      int64     `json:"quota_used"`
	QuotaLimit     int64     `json:"quota_limit"`
	MaxMessageSize int64     `json:"max_message_size"`
	IsAdmin        bool      `json:"is_admin"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastLoginAt    time.Time `json:"last_login_at,omitempty"`
}

// DomainData holds domain information
type DomainData struct {
	Name          string            `json:"name"`
	MaxAccounts   int               `json:"max_accounts"`
	MaxMailboxSize int64            `json:"max_mailbox_size"`
	DKIMSelector  string            `json:"dkim_selector"`
	DKIMPublicKey string            `json:"dkim_public_key,omitempty"`
	DKIMPrivateKey string           `json:"dkim_private_key,omitempty"`
	Settings      map[string]string `json:"settings,omitempty"`
	IsActive      bool              `json:"is_active"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// QueueEntry holds message queue information
type QueueEntry struct {
	ID          string    `json:"id"`
	From        string    `json:"from"`
	To          []string  `json:"to"`
	MessagePath string    `json:"message_path"`
	CreatedAt   time.Time `json:"created_at"`
	NextRetry   time.Time `json:"next_retry"`
	RetryCount  int       `json:"retry_count"`
	LastError   string    `json:"last_error"`
	Status      string    `json:"status"` // pending, sending, failed, delivered
}

// SessionData holds active session information
type SessionData struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // smtp, imap, http
	User      string    `json:"user"`
	Domain    string    `json:"domain"`
	RemoteIP  string    `json:"remote_ip"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RateLimitData holds rate limiting counters
type RateLimitData struct {
	Key       string    `json:"key"` // ip:port or user@domain
	Count     int       `json:"count"`
	WindowStart time.Time `json:"window_start"`
	LastRequest time.Time `json:"last_request"`
}

// BlockEntry holds blocklist entries
type BlockEntry struct {
	Key       string    `json:"key"` // IP or domain
	Type      string    `json:"type"` // ip, domain
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Source    string    `json:"source"` // manual, auto
}

// MessageMeta holds message metadata
type MessageMeta struct {
	UID         uint32    `json:"uid"`
	Filename    string    `json:"filename"`
	Folder      string    `json:"folder"`
	Size        int64     `json:"size"`
	Date        time.Time `json:"date"`
	Flags       string    `json:"flags"`
	From        string    `json:"from"`
	To          []string  `json:"to"`
	Subject     string    `json:"subject"`
	MessageID   string    `json:"message_id"`
	HasAttachment bool  `json:"has_attachment"`
}

// ContactData holds contact information
type ContactData struct {
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	User        string    `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
}

// AliasData holds email alias information
type AliasData struct {
	Alias       string    `json:"alias"`       // alias@domain
	Target      string    `json:"target"`      // user@domain
	Domain      string    `json:"domain"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// Open opens or creates the database
func Open(path string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	bolt, err := bbolt.Open(path, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &DB{bolt: bolt}

	// Initialize buckets
	if err := db.initBuckets(); err != nil {
		bolt.Close()
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	return db, nil
}

// Close closes the database
func (d *DB) Close() error {
	return d.bolt.Close()
}

// initBuckets creates all required buckets
func (d *DB) initBuckets() error {
	buckets := []string{
		BucketAccounts,
		BucketDomains,
		BucketQueue,
		BucketSessions,
		BucketRateLimits,
		BucketSpam,
		BucketBlocklist,
		BucketMetrics,
		BucketUIDValidity,
		BucketUIDNext,
		BucketMessageMeta,
		BucketIndex,
		BucketContacts,
		BucketAliases,
	}

	return d.bolt.Update(func(tx *bbolt.Tx) error {
		for _, name := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", name, err)
			}
		}
		return nil
	})
}

// Put stores a value in a bucket
func (d *DB) Put(bucket string, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}
		return b.Put([]byte(key), data)
	})
}

// Get retrieves a value from a bucket
func (d *DB) Get(bucket string, key string, dest interface{}) error {
	return d.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}

		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("key not found: %s", key)
		}

		return json.Unmarshal(data, dest)
	})
}

// Delete removes a key from a bucket
func (d *DB) Delete(bucket string, key string) error {
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}
		return b.Delete([]byte(key))
	})
}

// Exists checks if a key exists in a bucket
func (d *DB) Exists(bucket string, key string) bool {
	var exists bool
	err := d.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}
		exists = b.Get([]byte(key)) != nil
		return nil
	})
	if err != nil {
		return false
	}
	return exists
}

// ListKeys returns all keys in a bucket
func (d *DB) ListKeys(bucket string) ([]string, error) {
	var keys []string
	err := d.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}

		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			keys = append(keys, string(k))
		}
		return nil
	})

	return keys, err
}

// ForEach iterates over all entries in a bucket
func (d *DB) ForEach(bucket string, fn func(key string, value []byte) error) error {
	return d.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if err := fn(string(k), v); err != nil {
				return err
			}
		}
		return nil
	})
}

// ForEachPrefix iterates over entries with a given prefix
func (d *DB) ForEachPrefix(bucket string, prefix string, fn func(key string, value []byte) error) error {
	return d.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", bucket)
		}

		c := b.Cursor()
		prefixBytes := []byte(prefix)
		for k, v := c.Seek(prefixBytes); k != nil && len(k) >= len(prefixBytes) && string(k[:len(prefixBytes)]) == prefix; k, v = c.Next() {
			if err := fn(string(k), v); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- Account Operations ---

// AccountKey returns the database key for an account
func AccountKey(domain, localPart string) string {
	return fmt.Sprintf("%s/%s", domain, localPart)
}

// CreateAccount creates a new account
func (d *DB) CreateAccount(account *AccountData) error {
	if account.CreatedAt.IsZero() {
		account.CreatedAt = time.Now()
	}
	account.UpdatedAt = time.Now()

	key := AccountKey(account.Domain, account.LocalPart)
	return d.Put(BucketAccounts, key, account)
}

// GetAccount retrieves an account
func (d *DB) GetAccount(domain, localPart string) (*AccountData, error) {
	var account AccountData
	key := AccountKey(domain, localPart)
	if err := d.Get(BucketAccounts, key, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// UpdateAccount updates an existing account
func (d *DB) UpdateAccount(account *AccountData) error {
	account.UpdatedAt = time.Now()
	key := AccountKey(account.Domain, account.LocalPart)
	return d.Put(BucketAccounts, key, account)
}

// DeleteAccount removes an account
func (d *DB) DeleteAccount(domain, localPart string) error {
	key := AccountKey(domain, localPart)
	return d.Delete(BucketAccounts, key)
}

// ListAccountsByDomain returns all accounts in a domain
func (d *DB) ListAccountsByDomain(domain string) ([]*AccountData, error) {
	var accounts []*AccountData
	prefix := domain + "/"

	err := d.ForEachPrefix(BucketAccounts, prefix, func(key string, value []byte) error {
		var account AccountData
		if err := json.Unmarshal(value, &account); err != nil {
			return err
		}
		accounts = append(accounts, &account)
		return nil
	})

	return accounts, err
}

// --- Domain Operations ---

// CreateDomain creates a new domain
func (d *DB) CreateDomain(domain *DomainData) error {
	if domain.CreatedAt.IsZero() {
		domain.CreatedAt = time.Now()
	}
	domain.UpdatedAt = time.Now()

	return d.Put(BucketDomains, domain.Name, domain)
}

// GetDomain retrieves a domain
func (d *DB) GetDomain(name string) (*DomainData, error) {
	var domain DomainData
	if err := d.Get(BucketDomains, name, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// UpdateDomain updates an existing domain
func (d *DB) UpdateDomain(domain *DomainData) error {
	domain.UpdatedAt = time.Now()
	return d.Put(BucketDomains, domain.Name, domain)
}

// DeleteDomain removes a domain
func (d *DB) DeleteDomain(name string) error {
	return d.Delete(BucketDomains, name)
}

// ListDomains returns all domains
func (d *DB) ListDomains() ([]*DomainData, error) {
	var domains []*DomainData

	err := d.ForEach(BucketDomains, func(key string, value []byte) error {
		var domain DomainData
		if err := json.Unmarshal(value, &domain); err != nil {
			return err
		}
		domains = append(domains, &domain)
		return nil
	})

	return domains, err
}

// --- Queue Operations ---

// Enqueue adds a message to the queue
func (d *DB) Enqueue(entry *QueueEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	return d.Put(BucketQueue, entry.ID, entry)
}

// GetQueueEntry retrieves a queue entry
func (d *DB) GetQueueEntry(id string) (*QueueEntry, error) {
	var entry QueueEntry
	if err := d.Get(BucketQueue, id, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// UpdateQueueEntry updates a queue entry
func (d *DB) UpdateQueueEntry(entry *QueueEntry) error {
	return d.Put(BucketQueue, entry.ID, entry)
}

// Dequeue removes a message from the queue
func (d *DB) Dequeue(id string) error {
	return d.Delete(BucketQueue, id)
}

// GetPendingQueue returns entries ready for delivery
func (d *DB) GetPendingQueue(now time.Time) ([]*QueueEntry, error) {
	var entries []*QueueEntry

	err := d.ForEach(BucketQueue, func(key string, value []byte) error {
		var entry QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			return err
		}

		if entry.Status == "pending" && entry.NextRetry.Before(now) {
			entries = append(entries, &entry)
		}
		return nil
	})

	return entries, err
}

// --- UID Operations (for IMAP) ---

// GetUIDValidity returns the UID validity for a folder
func (d *DB) GetUIDValidity(domain, user, folder string) (uint32, error) {
	key := fmt.Sprintf("%s/%s/%s", domain, user, folder)
	var validity uint32

	err := d.Get(BucketUIDValidity, key, &validity)
	if err != nil {
		// Generate new validity
		validity = uint32(time.Now().Unix())
		if err := d.Put(BucketUIDValidity, key, validity); err != nil {
			return 0, err
		}
	}

	return validity, nil
}

// GetUIDNext returns the next UID for a folder
func (d *DB) GetUIDNext(domain, user, folder string) (uint32, error) {
	key := fmt.Sprintf("%s/%s/%s", domain, user, folder)
	var next uint32

	err := d.Get(BucketUIDNext, key, &next)
	if err != nil {
		next = 1
	}

	// Increment and store
	next++
	if err := d.Put(BucketUIDNext, key, next); err != nil {
		return 0, err
	}

	return next, nil
}

// --- Session Operations ---

// CreateSession creates a new session
func (d *DB) CreateSession(session *SessionData) error {
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	return d.Put(BucketSessions, session.ID, session)
}

// GetSession retrieves a session
func (d *DB) GetSession(id string) (*SessionData, error) {
	var session SessionData
	if err := d.Get(BucketSessions, id, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteSession removes a session
func (d *DB) DeleteSession(id string) error {
	return d.Delete(BucketSessions, id)
}

// --- Blocklist Operations ---

// BlockIP adds an IP to the blocklist
func (d *DB) BlockIP(ip string, reason, source string, duration time.Duration) error {
	entry := BlockEntry{
		Key:       ip,
		Type:      "ip",
		Reason:    reason,
		CreatedAt: time.Now(),
		Source:    source,
	}

	if duration > 0 {
		entry.ExpiresAt = time.Now().Add(duration)
	}

	return d.Put(BucketBlocklist, ip, entry)
}

// IsBlocked checks if an IP is blocked
func (d *DB) IsBlocked(ip string) (bool, *BlockEntry) {
	var entry BlockEntry
	if err := d.Get(BucketBlocklist, ip, &entry); err != nil {
		return false, nil
	}

	// Check if expired
	if !entry.ExpiresAt.IsZero() && entry.ExpiresAt.Before(time.Now()) {
		// Auto-unblock
		d.Delete(BucketBlocklist, ip)
		return false, nil
	}

	return true, &entry
}

// Unblock removes an IP from the blocklist
func (d *DB) Unblock(ip string) error {
	return d.Delete(BucketBlocklist, ip)
}
