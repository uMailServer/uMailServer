package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/bbolt"

	"github.com/umailserver/umailserver/internal/db/migrations"
)

// Bucket names
const (
	BucketAccounts      = "accounts"
	BucketDomains       = "domains"
	BucketQueue         = "queue"
	BucketSpam          = "spam"
	BucketMetrics       = "metrics"
	BucketMessageMeta   = "messagemeta"
	BucketIndex         = "index"
	BucketAliases       = "aliases"
	BucketContacts      = "contacts"
	BucketFilters       = "filters"
	BucketRevokedTokens = "revoked_tokens"
)

// DB wraps bbolt database
type DB struct {
	bolt *bbolt.DB
}

// AccountData holds account information
type AccountData struct {
	Email            string    `json:"email"`
	LocalPart        string    `json:"local_part"`
	Domain           string    `json:"domain"`
	PasswordHash     string    `json:"password_hash"`
	APOPHash         string    `json:"apop_hash,omitempty"` // SHA-256(password) for APOP authentication
	TOTPSecret       string    `json:"totp_secret,omitempty"`
	TOTPEnabled      bool      `json:"totp_enabled"`
	TOTPLastUsedStep int64     `json:"totp_last_used_step,omitempty"`
	QuotaUsed        int64     `json:"quota_used"`
	QuotaLimit       int64     `json:"quota_limit"`
	MaxMessageSize   int64     `json:"max_message_size"`
	ForwardTo        string    `json:"forward_to,omitempty"`
	ForwardKeepCopy  bool      `json:"forward_keep_copy"`
	SieveScript      string    `json:"sieve_script,omitempty"`
	VacationSettings string    `json:"vacation_settings,omitempty"`
	IsAdmin          bool      `json:"is_admin"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastLoginAt      time.Time `json:"last_login_at,omitempty"`
}

// DomainData holds domain information
type DomainData struct {
	Name           string            `json:"name"`
	MaxAccounts    int               `json:"max_accounts"`
	MaxMailboxSize int64             `json:"max_mailbox_size"`
	DKIMSelector   string            `json:"dkim_selector"`
	DKIMPublicKey  string            `json:"dkim_public_key,omitempty"`
	DKIMPrivateKey string            `json:"dkim_private_key,omitempty"`
	Settings       map[string]string `json:"settings,omitempty"`
	CatchAllTarget string            `json:"catch_all_target,omitempty"`
	IsActive       bool              `json:"is_active"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// QueuePriority represents message priority levels
type QueuePriority int

const (
	PriorityLow    QueuePriority = 0
	PriorityNormal QueuePriority = 1
	PriorityHigh   QueuePriority = 2
	PriorityUrgent QueuePriority = 3
)

func (p QueuePriority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "normal"
	}
}

// QueueEntry holds message queue information
type QueueEntry struct {
	ID          string        `json:"id"`
	From        string        `json:"from"`
	To          []string      `json:"to"`
	MessagePath string        `json:"message_path"`
	CreatedAt   time.Time     `json:"created_at"`
	NextRetry   time.Time     `json:"next_retry"`
	RetryCount  int           `json:"retry_count"`
	LastError   string        `json:"last_error"`
	Status      string        `json:"status"`   // pending, sending, failed, delivered, bounced
	Priority    QueuePriority `json:"priority"` // 0=low, 1=normal, 2=high, 3=urgent
	// DSN fields
	Notify DSNNotify `json:"notify"` // DSN notification preferences (NEVER, SUCCESS, FAILURE, DELAY)
	Ret    DSNRet    `json:"ret"`    // What to return in DSN (FULL or HDRS)
}

// DSNNotify represents delivery status notification preferences
type DSNNotify int32

// DSNRet represents what to return in DSN
type DSNRet int32

// AliasData holds email alias information
type AliasData struct {
	Alias     string    `json:"alias"`  // alias@domain
	Target    string    `json:"target"` // user@domain
	Domain    string    `json:"domain"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// Open opens or creates the database
func Open(path string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	bolt, err := bbolt.Open(path, 0o600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// On POSIX systems, bbolt applies the mode at the syscall level so umask
	// is respected. However, we also explicitly chmod the file after opening
	// to ensure strict permissions even if umask is unusually permissive.
	// On Windows this is a no-op (Chmod returns nil).
	if err := os.Chmod(path, 0o600); err != nil {
		_ = bolt.Close()
		return nil, fmt.Errorf("failed to set database file permissions: %w", err)
	}

	closeOnErr := true
	defer func() {
		if closeOnErr {
			_ = bolt.Close()
		}
	}()

	db := &DB{bolt: bolt}

	// Initialize buckets
	if err := db.initBuckets(); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	closeOnErr = false
	return db, nil
}

// Close closes the database
func (d *DB) Close() error {
	return d.bolt.Close()
}

// BoltDB returns the underlying bbolt.DB for advanced operations
func (d *DB) BoltDB() *bbolt.DB {
	return d.bolt
}

// RunMigrations runs all pending database migrations
func (d *DB) RunMigrations() error {
	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(d.bolt, registry)
	return migrator.Migrate()
}

// initBuckets creates all required buckets
func (d *DB) initBuckets() error {
	buckets := []string{
		BucketAccounts,
		BucketDomains,
		BucketQueue,
		BucketSpam,
		BucketMetrics,
		BucketMessageMeta,
		BucketIndex,
		BucketContacts,
		BucketAliases,
		BucketFilters,
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

// IncrementQuota atomically adds delta to an account's QuotaUsed inside a bbolt transaction.
// It returns an error if the account does not exist, quota would be exceeded, or int64 overflow.
func (d *DB) IncrementQuota(domain, localPart string, delta int64) error {
	key := AccountKey(domain, localPart)
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("key not found: %s", key)
		}
		var account AccountData
		if err := json.Unmarshal(data, &account); err != nil {
			return err
		}
		if account.QuotaLimit > 0 && account.QuotaUsed+delta > account.QuotaLimit {
			return fmt.Errorf("quota exceeded for user: %s", key)
		}
		if delta > 0 && account.QuotaUsed > math.MaxInt64-delta {
			return fmt.Errorf("quota overflow for user: %s", key)
		}
		account.QuotaUsed += delta
		account.UpdatedAt = time.Now()
		newData, err := json.Marshal(&account)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		return b.Put([]byte(key), newData)
	})
}

// StoreRevokedToken persists a revoked token hash with its expiry time.
func (d *DB) StoreRevokedToken(tokenHash string, expiry time.Time) error {
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(BucketRevokedTokens))
		if err != nil {
			return err
		}
		data, _ := json.Marshal(expiry)
		return b.Put([]byte(tokenHash), data)
	})
}

// IsTokenRevoked checks whether a token hash is in the persistent revocation list.
// It also performs lazy cleanup of expired entries.
func (d *DB) IsTokenRevoked(tokenHash string) (bool, error) {
	var revoked bool
	var toDelete []string
	now := time.Now()
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketRevokedTokens))
		if b == nil {
			return nil
		}
		data := b.Get([]byte(tokenHash))
		if data == nil {
			return nil
		}
		var expiry time.Time
		if err := json.Unmarshal(data, &expiry); err != nil {
			// Invalid entry - delete it
			toDelete = append(toDelete, tokenHash)
			return nil
		}
		if now.After(expiry) {
			toDelete = append(toDelete, tokenHash)
			return nil
		}
		revoked = true
		return nil
	})
	if err != nil {
		return false, err
	}
	// Cleanup expired/invalid entries in a separate transaction
	if len(toDelete) > 0 {
		_ = d.bolt.Update(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(BucketRevokedTokens))
			if b == nil {
				return nil
			}
			for _, h := range toDelete {
				_ = b.Delete([]byte(h))
			}
			return nil
		})
	}
	return revoked, nil
}

// CleanupRevokedTokens removes all expired token revocation entries.
func (d *DB) CleanupRevokedTokens() error {
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketRevokedTokens))
		if b == nil {
			return nil
		}
		now := time.Now()
		return b.ForEach(func(k, v []byte) error {
			var expiry time.Time
			if err := json.Unmarshal(v, &expiry); err != nil || now.After(expiry) {
				_ = b.Delete(k)
			}
			return nil
		})
	})
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

// EnqueueWithLimit adds a message to the queue only if the total number of
// entries in the queue bucket is below maxSize. The count and insert are
// performed inside a single bbolt transaction so the check is atomic.
func (d *DB) EnqueueWithLimit(entry *QueueEntry, maxSize int) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketQueue))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketQueue)
		}

		// Count existing entries
		count := 0
		_ = b.ForEach(func(_, _ []byte) error {
			count++
			return nil
		})
		if count >= maxSize {
			return fmt.Errorf("queue is full (max %d entries)", maxSize)
		}

		return b.Put([]byte(entry.ID), data)
	})
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

// pendingStatusMarker is the byte sequence json.Marshal produces for
// `Status: "pending"` on a QueueEntry. The encoder is whitespace-free, so
// exact substring matching is reliable for entries written via db.Put.
var pendingStatusMarker = []byte(`"status":"pending"`)

// errInvalidQueueEntry signals corruption in the queue bucket. It's a
// package-level sentinel so the GetPendingQueue hot loop doesn't allocate
// per non-pending entry — any per-call error string with %q would force the
// key argument to escape on every iteration, costing ~1 alloc/entry.
var errInvalidQueueEntry = errors.New("queue entry has invalid JSON")

// GetPendingQueue returns entries ready for delivery. It is called on every
// queue sweep tick, so the bucket scan is hot — we substring-match the
// status marker before paying for a full json.Unmarshal of the entry, which
// skips the ~12 allocs/entry decode cost for non-pending rows.
func (d *DB) GetPendingQueue(now time.Time) ([]*QueueEntry, error) {
	var entries []*QueueEntry

	err := d.ForEach(BucketQueue, func(key string, value []byte) error {
		if !bytes.Contains(value, pendingStatusMarker) {
			// Cheap sniff: real entries always begin with `{` from json.Marshal.
			// Anything else is corruption and is surfaced rather than silently skipped.
			if len(value) == 0 || value[0] != '{' {
				return errInvalidQueueEntry
			}
			return nil
		}
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

// GetAlias retrieves an alias by domain and local part
func (d *DB) GetAlias(domain, localPart string) (*AliasData, error) {
	key := domain + ":" + strings.ToLower(localPart)
	var alias AliasData
	if err := d.Get(BucketAliases, key, &alias); err != nil {
		return nil, err
	}
	return &alias, nil
}

// ResolveAlias resolves an alias to its target address
func (d *DB) ResolveAlias(domain, localPart string) (string, error) {
	alias, err := d.GetAlias(domain, localPart)
	if err != nil {
		return "", err
	}
	if alias == nil || !alias.IsActive {
		return "", nil
	}
	return alias.Target, nil
}

// ListAliases returns all aliases
func (d *DB) ListAliases() ([]*AliasData, error) {
	var aliases []*AliasData
	err := d.ForEach(BucketAliases, func(key string, value []byte) error {
		var alias AliasData
		if err := json.Unmarshal(value, &alias); err != nil {
			return err
		}
		aliases = append(aliases, &alias)
		return nil
	})
	return aliases, err
}

// CreateAlias creates a new alias
func (d *DB) CreateAlias(alias *AliasData) error {
	if alias.CreatedAt.IsZero() {
		alias.CreatedAt = time.Now()
	}
	key := alias.Domain + ":" + strings.ToLower(alias.Alias)
	return d.Put(BucketAliases, key, alias)
}

// UpdateAlias updates an existing alias
func (d *DB) UpdateAlias(alias *AliasData) error {
	key := alias.Domain + ":" + strings.ToLower(alias.Alias)
	return d.Put(BucketAliases, key, alias)
}

// DeleteAlias removes an alias
func (d *DB) DeleteAlias(domain, localPart string) error {
	key := domain + ":" + strings.ToLower(localPart)
	return d.Delete(BucketAliases, key)
}
