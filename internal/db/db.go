package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.etcd.io/bbolt"
	bbolterrors "go.etcd.io/bbolt/errors"

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

	lastRevokedPurge atomic.Int64 // unix ns of the last expired-revocation sweep
}

// AccountData holds account information
// ErrAccountExists is returned by CreateAccount when an account with the
// same domain/local part already exists.
var ErrAccountExists = errors.New("account already exists")

// ErrDomainExists is returned by CreateDomain when a domain with the same
// name already exists.
var ErrDomainExists = errors.New("domain already exists")

// ErrAliasExists is returned by CreateAlias when an alias with the same
// domain/local part already exists.
var ErrAliasExists = errors.New("alias already exists")

// ErrDomainNotFound is returned by CreateAccountInDomain when the account's
// domain is not hosted.
var ErrDomainNotFound = errors.New("domain not found")

// ErrDomainAccountLimit is returned by CreateAccountInDomain when the domain
// already holds MaxAccounts accounts.
var ErrDomainAccountLimit = errors.New("domain account limit reached")

// ErrQueueEntryNotFound is returned by UpdateQueueEntry when the entry no
// longer exists.
var ErrQueueEntryNotFound = errors.New("queue entry not found")

// ErrAccountNotFound is returned by UpdateAccount when the account no longer
// exists (F6080).
var ErrAccountNotFound = errors.New("account not found")

// ErrAliasNotFound is returned by UpdateAlias when the alias no longer
// exists (F6081).
var ErrAliasNotFound = errors.New("alias not found")

// ErrAliasConflict is returned by CreateAlias when the alias would shadow an
// existing mailbox or point at itself (F6083).
var ErrAliasConflict = errors.New("alias conflicts with a mailbox or targets itself")

// ErrInvalidName is returned when a domain, local part or alias is empty or
// contains a character that collides with the key scheme (F6085).
var ErrInvalidName = errors.New("invalid name")

// validName rejects empty names and names holding the key separators.
func validName(s string, forbidden string) bool {
	return s != "" && !strings.ContainsAny(s, forbidden)
}

func validAccountNames(domain, local string) error {
	if !validName(domain, "/:") || !validName(local, "/") {
		return fmt.Errorf("%w: %q/%q", ErrInvalidName, domain, local)
	}
	return nil
}

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
	// TokensValidAfter (unix nanoseconds, 0 = unset) is the session
	// revocation cut-off (F6250): a JWT issued before it is no longer
	// accepted. It only ever moves forward (UpdateAccount/MutateAccount keep
	// the stored value when a stale snapshot carries a smaller one).
	TokensValidAfter int64 `json:"tokens_valid_after,omitempty"`
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
	ClaimedAt   time.Time     `json:"claimed_at,omitempty"` // when ClaimQueueEntry moved it to "sending"
	Status      string        `json:"status"`               // pending, sending, failed, delivered, bounced
	Priority    QueuePriority `json:"priority"`             // 0=low, 1=normal, 2=high, 3=urgent
	// DSN fields
	Notify DSNNotify `json:"notify"`          // DSN notification preferences (NEVER, SUCCESS, FAILURE, DELAY)
	Ret    DSNRet    `json:"ret"`             // What to return in DSN (FULL or HDRS)
	EnvID  string    `json:"envid,omitempty"` // RFC 3461 ENVID (xtext) of the original transaction
	ORCPT  string    `json:"orcpt,omitempty"` // RFC 3461 ORCPT ("addr-type;xtext") of this recipient
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
		if errors.Is(err, bbolterrors.ErrTimeout) {
			// F6565: name the file and the cause instead of a bare "timeout".
			return nil, fmt.Errorf("failed to open database %s: file is locked by another process (is another uMailServer instance running?): %w", path, err)
		}
		return nil, fmt.Errorf("failed to open database %s: %w", path, err)
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

	// F6620: refuse a database written by a newer binary before touching it.
	if err := db.checkSchemaCompatible(); err != nil {
		return nil, fmt.Errorf("cannot open database %s: %w", path, err)
	}

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

// RunMigrations runs all pending database migrations atomically. It returns
// migrations.ErrSchemaTooNew when the file was written by a newer binary.
func (d *DB) RunMigrations() error {
	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	migrator := migrations.NewMigrator(d.bolt, registry)
	return migrator.Migrate()
}

func (d *DB) checkSchemaCompatible() error {
	registry := migrations.NewRegistry()
	migrations.InitMigrations(registry)
	return migrations.NewMigrator(d.bolt, registry).CheckCompatible()
}

// Backup writes a consistent point-in-time snapshot of the whole database
// (every bucket, including revoked_tokens and the migration records) to w.
// It uses a read transaction, so it is safe while the server is writing and
// the result is a complete bbolt file that Open accepts (F6626).
func (d *DB) Backup(w io.Writer) error {
	return d.bolt.View(func(tx *bbolt.Tx) error {
		_, err := tx.WriteTo(w)
		return err
	})
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
		BucketRevokedTokens, // F6625: created at Open, not lazily on first revoke
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

// iterPageSize bounds how many entries are copied out of one read
// transaction before the callbacks run.
const iterPageSize = 256

// iterate walks bucket keys (optionally restricted to prefix) in pages. Each
// page is copied inside a short read transaction and the callbacks run AFTER
// it ends, so a callback may freely call back into the DB (a bbolt Update
// inside View can deadlock on mmap growth), may mutate the bucket being
// iterated, and may retain key/value (F6628). Entries are not a single
// snapshot: concurrent writers may be seen between pages.
func (d *DB) iterate(bucket, prefix string, fn func(key string, value []byte) error) error {
	type kv struct{ k, v []byte }
	p := []byte(prefix)
	var after []byte // last key delivered
	for {
		page := make([]kv, 0, 16)
		err := d.bolt.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket([]byte(bucket))
			if b == nil {
				return fmt.Errorf("bucket not found: %s", bucket)
			}
			c := b.Cursor()
			var k, v []byte
			if after == nil {
				k, v = c.Seek(p)
			} else if k, v = c.Seek(after); k != nil && bytes.Equal(k, after) {
				k, v = c.Next()
			}
			for ; k != nil && bytes.HasPrefix(k, p) && len(page) < iterPageSize; k, v = c.Next() {
				page = append(page, kv{append([]byte(nil), k...), append([]byte(nil), v...)})
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, e := range page {
			if err := fn(string(e.k), e.v); err != nil {
				return err
			}
		}
		if len(page) < iterPageSize {
			return nil
		}
		after = page[len(page)-1].k
	}
}

// ForEach iterates over all entries in a bucket. The callback runs outside
// any bbolt transaction, so it may call other DB methods.
func (d *DB) ForEach(bucket string, fn func(key string, value []byte) error) error {
	return d.iterate(bucket, "", fn)
}

// ForEachPrefix iterates over entries with a given prefix (see ForEach).
func (d *DB) ForEachPrefix(bucket string, prefix string, fn func(key string, value []byte) error) error {
	return d.iterate(bucket, prefix, fn)
}

// --- Account Operations ---

// AccountKey returns the database key for an account
func AccountKey(domain, localPart string) string {
	return fmt.Sprintf("%s/%s", domain, localPart)
}

// CreateAccount creates a new account
func (d *DB) CreateAccount(account *AccountData) error {
	if err := validAccountNames(account.Domain, account.LocalPart); err != nil {
		return err
	}
	if account.CreatedAt.IsZero() {
		account.CreatedAt = time.Now()
	}
	account.UpdatedAt = time.Now()

	key := AccountKey(account.Domain, account.LocalPart)
	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	// Atomic check-and-put in a single transaction: a duplicate create
	// returns ErrAccountExists instead of silently overwriting the stored
	// account (last-write-wins on the same key). F5165: an address that
	// differs only by case is the same mailbox and is a duplicate too.
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		if hasKeyFold(b, []byte(key)) || accountShadowsAlias(tx, account.Domain, account.LocalPart) {
			return ErrAccountExists
		}
		return b.Put([]byte(key), data)
	})
}

// CreateAccountInDomain creates a new account in a hosted domain. Unlike
// CreateAccount, the domain's existence and its MaxAccounts limit (0 =
// unlimited) are checked in the same transaction as the insert, so parallel
// creates cannot exceed the limit (F5510) and a create racing DeleteDomain
// cannot leave an orphan account of a deleted domain (F5511). It returns
// ErrDomainNotFound, ErrDomainAccountLimit or ErrAccountExists.
func (d *DB) CreateAccountInDomain(account *AccountData) error {
	if err := validAccountNames(account.Domain, account.LocalPart); err != nil {
		return err
	}
	if account.CreatedAt.IsZero() {
		account.CreatedAt = time.Now()
	}
	account.UpdatedAt = time.Now()

	key := AccountKey(account.Domain, account.LocalPart)
	data, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	return d.bolt.Update(func(tx *bbolt.Tx) error {
		domains := tx.Bucket([]byte(BucketDomains))
		if domains == nil {
			return fmt.Errorf("bucket not found: %s", BucketDomains)
		}
		raw := domains.Get([]byte(account.Domain))
		if raw == nil {
			return ErrDomainNotFound
		}
		var domain DomainData
		if err := json.Unmarshal(raw, &domain); err != nil {
			return fmt.Errorf("failed to unmarshal domain: %w", err)
		}
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		if hasKeyFold(b, []byte(key)) || accountShadowsAlias(tx, account.Domain, account.LocalPart) {
			return ErrAccountExists
		}
		if domain.MaxAccounts > 0 {
			prefix := []byte(AccountKey(account.Domain, ""))
			count := 0
			c := b.Cursor()
			for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
				count++
			}
			if count >= domain.MaxAccounts {
				return ErrDomainAccountLimit
			}
		}
		return b.Put([]byte(key), data)
	})
}

// accountShadowsAlias reports whether an alias with the same address exists
// (F6084): a mailbox and an alias for one address make delivery ambiguous.
func accountShadowsAlias(tx *bbolt.Tx, domain, local string) bool {
	ab := tx.Bucket([]byte(BucketAliases))
	return ab != nil && hasKeyFold(ab, []byte(domain+":"+local))
}

// hasKeyFold reports whether bucket b holds key under any letter case.
// Domains are case-insensitive (RFC 5321 2.4) and every local part is
// treated case-insensitively here (SMTP AUTH lowercases the username,
// aliases are keyed by the lowercased local part), so keys differing only by
// case name the same mailbox. The exact-key probe keeps the common duplicate
// O(log n); the scan only runs for a new key and only on create paths.
func hasKeyFold(b *bbolt.Bucket, key []byte) bool {
	if b.Get(key) != nil {
		return true
	}
	c := b.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		if bytes.EqualFold(k, key) {
			return true
		}
	}
	return false
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

// UpdateAccount updates an existing account; ErrAccountNotFound if absent.
//
// F5166: callers update from a snapshot read earlier (GetAccount -> modify ->
// UpdateAccount). The counters below are owned by their own atomic
// operations, so the stored values are carried over inside this transaction
// instead of being overwritten by the snapshot:
//   - QuotaUsed is maintained only by IncrementQuota; a stale snapshot would
//     erase deliveries committed in between and let the mailbox exceed its
//     quota.
//   - TOTPLastUsedStep only moves forward (RFC 6238 5.2 replay guard,
//     ConsumeTOTPStep); a stale snapshot would rewind it and make an
//     already-used code acceptable again.
//
// The caller's struct is updated to the values actually stored.
func (d *DB) UpdateAccount(account *AccountData) error {
	account.UpdatedAt = time.Now()
	key := []byte(AccountKey(account.Domain, account.LocalPart))
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		cur := b.Get(key)
		if cur == nil {
			// F6080: never resurrect an account deleted after the caller's
			// snapshot was read.
			return ErrAccountNotFound
		}
		{
			// An unreadable stored row has no counters to keep; the update
			// then replaces it, as before.
			var stored AccountData
			if json.Unmarshal(cur, &stored) == nil {
				account.QuotaUsed = stored.QuotaUsed
				if stored.TOTPLastUsedStep > account.TOTPLastUsedStep {
					account.TOTPLastUsedStep = stored.TOTPLastUsedStep
				}
				// F6250: a stale snapshot must not re-validate sessions
				// revoked after it was read.
				if stored.TokensValidAfter > account.TokensValidAfter {
					account.TokensValidAfter = stored.TokensValidAfter
				}
			}
		}
		data, err := json.Marshal(account)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		return b.Put(key, data)
	})
}

// MutateAccount atomically reads an account, applies fn and writes it back in
// one bbolt transaction (F6255/F6256): callers that did GetAccount -> slow
// work (password hashing) -> UpdateAccount overwrote concurrent changes
// (a concurrent password change, disable, role change) with their stale
// snapshot. fn sees the stored row; returning an error aborts without
// writing. ErrAccountNotFound if absent. Counters keep the UpdateAccount
// guarantees: QuotaUsed is never changed by fn, TOTPLastUsedStep and
// TokensValidAfter never move backwards. The stored result is returned.
func (d *DB) MutateAccount(domain, localPart string, fn func(*AccountData) error) (*AccountData, error) {
	return d.mutateAccount(domain, localPart, false, fn)
}

// MutateAccountKeepAdmin is MutateAccount plus the F6710 invariant: if fn
// would demote or deactivate the last active administrator, the transaction
// is rolled back with ErrLastActiveAdmin. The check runs inside the write
// transaction, so it holds for concurrent callers and other processes.
func (d *DB) MutateAccountKeepAdmin(domain, localPart string, fn func(*AccountData) error) (*AccountData, error) {
	return d.mutateAccount(domain, localPart, true, fn)
}

// ErrLastActiveAdmin is returned when an operation would leave the system
// without an active administrator (F6710).
var ErrLastActiveAdmin = errors.New("cannot remove the last active administrator")

// otherActiveAdminTx reports whether an active admin exists whose key does
// not satisfy skip.
func otherActiveAdminTx(tx *bbolt.Tx, skip func(a *AccountData) bool) (bool, error) {
	b := tx.Bucket([]byte(BucketAccounts))
	if b == nil {
		return false, fmt.Errorf("bucket not found: %s", BucketAccounts)
	}
	found := false
	err := b.ForEach(func(_, v []byte) error {
		var a AccountData
		if json.Unmarshal(v, &a) == nil && a.IsAdmin && a.IsActive && !skip(&a) {
			found = true
		}
		return nil
	})
	return found, err
}

func (d *DB) mutateAccount(domain, localPart string, keepAdmin bool, fn func(*AccountData) error) (*AccountData, error) {
	key := []byte(AccountKey(domain, localPart))
	var result AccountData
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		cur := b.Get(key)
		if cur == nil {
			return ErrAccountNotFound
		}
		var acct AccountData
		if err := json.Unmarshal(cur, &acct); err != nil {
			return err
		}
		quota, step, valid := acct.QuotaUsed, acct.TOTPLastUsedStep, acct.TokensValidAfter
		ident := [2]string{acct.Domain, acct.LocalPart}
		wasAdmin := acct.IsAdmin && acct.IsActive
		if err := fn(&acct); err != nil {
			return err
		}
		if keepAdmin && wasAdmin && (!acct.IsAdmin || !acct.IsActive) {
			others, err := otherActiveAdminTx(tx, func(a *AccountData) bool {
				return a.Domain == ident[0] && a.LocalPart == ident[1]
			})
			if err != nil {
				return err
			}
			if !others {
				return ErrLastActiveAdmin
			}
		}
		// F6562: the row's key identity is immutable.
		acct.Domain, acct.LocalPart = ident[0], ident[1]
		acct.QuotaUsed = quota
		if acct.TOTPLastUsedStep < step {
			acct.TOTPLastUsedStep = step
		}
		if acct.TokensValidAfter < valid {
			acct.TokensValidAfter = valid
		}
		acct.UpdatedAt = time.Now()
		data, err := json.Marshal(&acct)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		if err := b.Put(key, data); err != nil {
			return err
		}
		result = acct
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
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
		// F5880: a corrupt/legacy negative counter is treated as 0.
		if account.QuotaUsed < 0 {
			account.QuotaUsed = 0
		}
		if delta > 0 && account.QuotaUsed > math.MaxInt64-delta {
			return fmt.Errorf("quota overflow for user: %s", key)
		}
		if delta > 0 && account.QuotaLimit > 0 && account.QuotaUsed+delta > account.QuotaLimit {
			return fmt.Errorf("quota exceeded for user: %s", key)
		}
		// F5880: a release larger than the recorded usage (e.g. usage was
		// reset, or a message was counted before quota tracking) must not
		// drive the counter negative, which would grant free headroom.
		if delta < 0 && account.QuotaUsed+delta < 0 {
			account.QuotaUsed = 0
		} else {
			account.QuotaUsed += delta
		}
		account.UpdatedAt = time.Now()
		newData, err := json.Marshal(&account)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		return b.Put([]byte(key), newData)
	})
}

// ReconcileQuota sets an account's QuotaUsed to the authoritative byte count
// (e.g. recounted from disk), clamped at 0. UpdateAccount deliberately never
// writes QuotaUsed, so this is the only way to correct drift. F5881.
func (d *DB) ReconcileQuota(domain, localPart string, used int64) error {
	if used < 0 {
		used = 0
	}
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
		account.QuotaUsed = used
		account.UpdatedAt = time.Now()
		newData, err := json.Marshal(&account)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		return b.Put([]byte(key), newData)
	})
}

// ConsumeTOTPStep atomically advances an account's TOTP replay guard inside
// a single bbolt transaction. It reports consumed=false when the stored
// TOTPLastUsedStep is not the expected value — another login consumed a step
// in the meantime and the caller must treat the code as replayed
// (RFC 6238 §5.2) — so API-layer read-modify-write races cannot bypass the
// single-acceptance rule.
func (d *DB) ConsumeTOTPStep(domain, localPart string, expectedLast, newStep int64) (bool, error) {
	key := AccountKey(domain, localPart)
	consumed := false
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
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
		// F6561: the guard only moves forward.
		if account.TOTPLastUsedStep != expectedLast || newStep <= expectedLast {
			// consumed stays false: the step moved under us.
			return nil
		}
		account.TOTPLastUsedStep = newStep
		account.UpdatedAt = time.Now()
		newData, err := json.Marshal(&account)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		if err := b.Put([]byte(key), newData); err != nil {
			return err
		}
		consumed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return consumed, nil
}

// StoreRevokedToken persists a revoked token hash with its expiry time.
func (d *DB) StoreRevokedToken(tokenHash string, expiry time.Time) error {
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(BucketRevokedTokens))
		if err != nil {
			return err
		}
		data, _ := json.Marshal(expiry)
		if err := b.Put([]byte(tokenHash), data); err != nil {
			return err
		}
		// F6624: bound growth. Callers rarely invoke CleanupRevokedTokens, so
		// sweep expired entries here, at most once per revokedPurgeEvery.
		nowNs := time.Now().UnixNano()
		last := d.lastRevokedPurge.Load()
		if nowNs-last >= int64(revokedPurgeEvery) && d.lastRevokedPurge.CompareAndSwap(last, nowNs) {
			return purgeExpiredRevoked(b, time.Now())
		}
		return nil
	})
}

// revokedPurgeEvery rate-limits the full-bucket expiry sweep in StoreRevokedToken.
const revokedPurgeEvery = time.Minute

// purgeExpiredRevoked deletes expired/corrupt entries. Keys are collected
// first: deleting while bbolt's ForEach/cursor is running skips the entry
// that follows each deletion (F6623).
func purgeExpiredRevoked(b *bbolt.Bucket, now time.Time) error {
	var keys [][]byte
	_ = b.ForEach(func(k, v []byte) error {
		var expiry time.Time
		if err := json.Unmarshal(v, &expiry); err != nil || now.After(expiry) {
			keys = append(keys, append([]byte(nil), k...))
		}
		return nil
	})
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// IsTokenRevoked checks whether a token hash is in the persistent revocation list.
// It also performs lazy cleanup of expired entries.
func (d *DB) IsTokenRevoked(tokenHash string) (bool, error) {
	var revoked bool
	var toDelete []string
	now := time.Now()
	// F6563: View, not Update: a write transaction fsyncs on every call even
	// when nothing changes, and this runs per authenticated request.
	err := d.bolt.View(func(tx *bbolt.Tx) error {
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
		return purgeExpiredRevoked(b, time.Now())
	})
}

// DeleteAccount removes an account
func (d *DB) DeleteAccount(domain, localPart string) error {
	key := AccountKey(domain, localPart)
	// F6566: aliases targeting the mailbox are removed in the same
	// transaction so they cannot dangle or silently re-attach to a later
	// account created under the same address.
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		if err := b.Delete([]byte(key)); err != nil {
			return err
		}
		return deleteAliasesTargeting(tx, func(t string) bool {
			return strings.EqualFold(t, localPart+"@"+domain)
		})
	})
}

// DeleteAccountKeepAdmin is DeleteAccount refusing (ErrLastActiveAdmin) to
// remove the last active administrator; the check and the delete share one
// write transaction (F6710). A missing account yields ErrAccountNotFound.
func (d *DB) DeleteAccountKeepAdmin(domain, localPart string) error {
	key := AccountKey(domain, localPart)
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAccounts))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAccounts)
		}
		cur := b.Get([]byte(key))
		if cur == nil {
			return ErrAccountNotFound
		}
		var a AccountData
		if json.Unmarshal(cur, &a) == nil && a.IsAdmin && a.IsActive {
			others, err := otherActiveAdminTx(tx, func(o *AccountData) bool {
				return o.Domain == domain && o.LocalPart == localPart
			})
			if err != nil {
				return err
			}
			if !others {
				return ErrLastActiveAdmin
			}
		}
		if err := b.Delete([]byte(key)); err != nil {
			return err
		}
		return deleteAliasesTargeting(tx, func(t string) bool {
			return strings.EqualFold(t, localPart+"@"+domain)
		})
	})
}

// DeleteDomainKeepAdmin is DeleteDomain refusing (ErrLastActiveAdmin) when
// the cascade would remove the last active administrator (F6710).
func (d *DB) DeleteDomainKeepAdmin(name string) error {
	return d.deleteDomain(name, true)
}

// deleteAliasesTargeting removes every alias whose target satisfies match.
func deleteAliasesTargeting(tx *bbolt.Tx, match func(target string) bool) error {
	b := tx.Bucket([]byte(BucketAliases))
	if b == nil {
		return nil
	}
	var keys [][]byte
	_ = b.ForEach(func(k, v []byte) error {
		var a AliasData
		if json.Unmarshal(v, &a) == nil && match(strings.TrimSpace(a.Target)) {
			keys = append(keys, append([]byte(nil), k...))
		}
		return nil
	})
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	return nil
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
	if !validName(domain.Name, "/:") {
		return fmt.Errorf("%w: domain %q", ErrInvalidName, domain.Name)
	}
	if domain.CreatedAt.IsZero() {
		domain.CreatedAt = time.Now()
	}
	domain.UpdatedAt = time.Now()

	data, err := json.Marshal(domain)
	if err != nil {
		return fmt.Errorf("failed to marshal domain: %w", err)
	}

	// Atomic check-and-put: a duplicate create returns ErrDomainExists
	// instead of overwriting the stored domain (DKIM keys, settings,
	// catch-all and active flag).
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDomains))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketDomains)
		}
		if hasKeyFold(b, []byte(domain.Name)) { // F5165: case-insensitive
			return ErrDomainExists
		}
		return b.Put([]byte(domain.Name), data)
	})
}

// GetDomain retrieves a domain
func (d *DB) GetDomain(name string) (*DomainData, error) {
	var domain DomainData
	if err := d.Get(BucketDomains, name, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// UpdateDomain updates an existing domain; ErrDomainNotFound if absent
func (d *DB) UpdateDomain(domain *DomainData) error {
	domain.UpdatedAt = time.Now()
	data, err := json.Marshal(domain)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketDomains))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketDomains)
		}
		// F6082: never resurrect a domain deleted after the read.
		if b.Get([]byte(domain.Name)) == nil {
			return ErrDomainNotFound
		}
		return b.Put([]byte(domain.Name), data)
	})
}

// DeleteDomain removes a domain together with its accounts and aliases in a
// single transaction, so no orphan account of a deleted domain can still
// authenticate and no orphan alias can still resolve.
func (d *DB) DeleteDomain(name string) error {
	return d.deleteDomain(name, false)
}

func (d *DB) deleteDomain(name string, keepAdmin bool) error {
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		if keepAdmin {
			inDomain := false
			ab := tx.Bucket([]byte(BucketAccounts))
			if ab != nil {
				_ = ab.ForEach(func(_, v []byte) error {
					var a AccountData
					if json.Unmarshal(v, &a) == nil && a.IsAdmin && a.IsActive && strings.EqualFold(a.Domain, name) {
						inDomain = true
					}
					return nil
				})
			}
			if inDomain {
				others, err := otherActiveAdminTx(tx, func(a *AccountData) bool { return strings.EqualFold(a.Domain, name) })
				if err != nil {
					return err
				}
				if !others {
					return ErrLastActiveAdmin
				}
			}
		}
		b := tx.Bucket([]byte(BucketDomains))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketDomains)
		}
		// F7331: domains are created case-insensitively (F5165), so delete
		// the stored key and its accounts/aliases the same way; an exact
		// match left case-variant rows behind.
		if err := deletePrefixFold(tx, BucketDomains, name, true); err != nil {
			return err
		}
		if err := deletePrefixFold(tx, BucketAccounts, AccountKey(name, ""), false); err != nil {
			return err
		}
		if err := deletePrefixFold(tx, BucketAliases, name+":", false); err != nil {
			return err
		}
		suffix := "@" + name
		return deleteAliasesTargeting(tx, func(t string) bool {
			return len(t) > len(suffix) && strings.EqualFold(t[len(t)-len(suffix):], suffix)
		})
	})
}

// deletePrefixFold removes every key of bucket that equals (exact=true) or
// starts with (exact=false) name, ignoring ASCII case.
func deletePrefixFold(tx *bbolt.Tx, bucket, name string, exact bool) error {
	b := tx.Bucket([]byte(bucket))
	if b == nil {
		return fmt.Errorf("bucket not found: %s", bucket)
	}
	var keys [][]byte
	_ = b.ForEach(func(k, _ []byte) error {
		if len(k) < len(name) || !strings.EqualFold(string(k[:len(name)]), name) {
			return nil
		}
		if exact && len(k) != len(name) {
			return nil
		}
		keys = append(keys, append([]byte(nil), k...))
		return nil
	})
	for _, k := range keys {
		if err := b.Delete(k); err != nil {
			return err
		}
	}
	return nil
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

// UpdateQueueEntry updates an existing queue entry. F5512: it returns
// ErrQueueEntryNotFound instead of re-creating an entry that was dequeued
// after the caller read it, so a stale write-back cannot resurrect a dropped
// message.
func (d *DB) UpdateQueueEntry(entry *QueueEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketQueue))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketQueue)
		}
		if b.Get([]byte(entry.ID)) == nil {
			return fmt.Errorf("%w: %s", ErrQueueEntryNotFound, entry.ID)
		}
		return b.Put([]byte(entry.ID), data)
	})
}

// ErrQueueEntryNotClaimable is returned by ClaimQueueEntry when the entry is
// no longer pending (another worker claimed it, or it was already finished).
var ErrQueueEntryNotClaimable = errors.New("queue entry is not claimable")

// ClaimQueueEntry atomically moves a pending, due entry to "sending" and
// returns the claimed entry. Between GetPendingQueue and a blind
// UpdateQueueEntry two workers (or a worker and the sweeper) could both pick
// the same entry and deliver it twice; the status check and write happen in
// one write transaction here so exactly one caller wins (F6627).
func (d *DB) ClaimQueueEntry(id string, now time.Time) (*QueueEntry, error) {
	var claimed QueueEntry
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketQueue))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketQueue)
		}
		raw := b.Get([]byte(id))
		if raw == nil {
			return fmt.Errorf("%w: %s", ErrQueueEntryNotFound, id)
		}
		if err := json.Unmarshal(raw, &claimed); err != nil {
			return err
		}
		if claimed.Status != "pending" || claimed.NextRetry.After(now) {
			return fmt.Errorf("%w: %s (status %q)", ErrQueueEntryNotClaimable, id, claimed.Status)
		}
		claimed.Status = "sending"
		claimed.ClaimedAt = now
		data, err := json.Marshal(&claimed)
		if err != nil {
			return err
		}
		return b.Put([]byte(id), data)
	})
	if err != nil {
		return nil, err
	}
	return &claimed, nil
}

// RequeueStaleClaims reverts entries stuck in "sending" whose claim is older
// than lease (the worker's outcome write failed or the worker died) to
// "pending", due now. It returns the number of entries reverted (F7191).
func (d *DB) RequeueStaleClaims(now time.Time, lease time.Duration) (int, error) {
	n := 0
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketQueue))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketQueue)
		}
		type kv struct {
			k string
			v []byte
		}
		var upd []kv
		err := b.ForEach(func(k, v []byte) error {
			if !bytes.Contains(v, []byte(`"status":"sending"`)) {
				return nil
			}
			var e QueueEntry
			if json.Unmarshal(v, &e) != nil || e.Status != "sending" {
				return nil
			}
			if !e.ClaimedAt.IsZero() && now.Sub(e.ClaimedAt) < lease {
				return nil
			}
			e.Status = "pending"
			e.NextRetry = now
			e.ClaimedAt = time.Time{}
			data, err := json.Marshal(&e)
			if err != nil {
				return nil
			}
			upd = append(upd, kv{string(k), data})
			return nil
		})
		if err != nil {
			return err
		}
		for _, u := range upd {
			if err := b.Put([]byte(u.k), u.v); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
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
	// A corrupt row must not hide the valid entries around it (F7570): the
	// scan continues, and the corruption is still surfaced as an error next
	// to the entries that were found.
	var badKey string
	bad := 0
	var badErr error

	err := d.ForEach(BucketQueue, func(key string, value []byte) error {
		if !bytes.Contains(value, pendingStatusMarker) {
			// Cheap sniff: real entries always begin with `{` from json.Marshal.
			// Anything else is corruption and is surfaced rather than silently skipped.
			if len(value) == 0 || value[0] != '{' {
				if bad == 0 {
					badKey, badErr = key, errInvalidQueueEntry
				}
				bad++
			}
			return nil
		}
		var entry QueueEntry
		if err := json.Unmarshal(value, &entry); err != nil {
			if bad == 0 {
				badKey, badErr = key, err
			}
			bad++
			return nil
		}
		if entry.Status == "pending" && entry.NextRetry.Before(now) {
			entries = append(entries, &entry)
		}
		return nil
	})
	if err == nil && bad > 0 {
		err = fmt.Errorf("%d corrupt queue row(s), first key %q: %w", bad, badKey, badErr)
	}

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
	if !validName(alias.Domain, "/:") || !validName(alias.Alias, "/") {
		return fmt.Errorf("%w: alias %q@%q", ErrInvalidName, alias.Alias, alias.Domain)
	}
	// F6083: an alias pointing at itself is a delivery loop.
	if t := strings.ToLower(alias.Target); t == strings.ToLower(alias.Alias) ||
		t == strings.ToLower(alias.Alias+"@"+alias.Domain) {
		return ErrAliasConflict
	}
	if alias.CreatedAt.IsZero() {
		alias.CreatedAt = time.Now()
	}
	key := alias.Domain + ":" + strings.ToLower(alias.Alias)
	data, err := json.Marshal(alias)
	if err != nil {
		return fmt.Errorf("failed to marshal alias: %w", err)
	}

	// Atomic check-and-put: a duplicate create returns ErrAliasExists
	// instead of silently retargeting the existing alias.
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAliases))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAliases)
		}
		// F7330: the domain must exist in this same transaction, else a
		// concurrent DeleteDomain (which also purges aliases) leaves the
		// alias orphaned and still resolvable.
		if db := tx.Bucket([]byte(BucketDomains)); db == nil || !hasKeyFold(db, []byte(alias.Domain)) {
			return ErrDomainNotFound
		}
		if hasKeyFold(b, []byte(key)) { // F5165: case-insensitive
			return ErrAliasExists
		}
		// F6083: an alias must not shadow a mailbox.
		if ab := tx.Bucket([]byte(BucketAccounts)); ab != nil &&
			hasKeyFold(ab, []byte(AccountKey(alias.Domain, alias.Alias))) {
			return ErrAliasConflict
		}
		if aliasCycleTx(tx, alias.Domain, key, alias.Target) {
			return ErrAliasConflict // F7250
		}
		return b.Put([]byte(key), data)
	})
}

// aliasCycleTx reports whether following target through the alias table
// (same-store addresses only) reaches the alias stored under selfKey, or
// runs into a pre-existing loop / absurdly deep chain.
func aliasCycleTx(tx *bbolt.Tx, domain, selfKey, target string) bool {
	b := tx.Bucket([]byte(BucketAliases))
	if b == nil {
		return false
	}
	self := strings.ToLower(selfKey)
	cur := strings.TrimSpace(target)
	for depth := 0; depth < 32; depth++ {
		at := strings.LastIndex(cur, "@")
		if at <= 0 {
			return false
		}
		k := cur[at+1:] + ":" + strings.ToLower(cur[:at])
		if strings.ToLower(k) == self {
			return true
		}
		raw := b.Get([]byte(k))
		if raw == nil {
			c := b.Cursor()
			for kk, v := c.First(); kk != nil; kk, v = c.Next() {
				if bytes.EqualFold(kk, []byte(k)) {
					raw = v
					break
				}
			}
		}
		if raw == nil {
			return false
		}
		var next AliasData
		if json.Unmarshal(raw, &next) != nil || !next.IsActive {
			return false
		}
		cur = strings.TrimSpace(next.Target)
	}
	return true
}

// UpdateAlias updates an existing alias
func (d *DB) UpdateAlias(alias *AliasData) error {
	if t := strings.ToLower(alias.Target); t == strings.ToLower(alias.Alias) ||
		t == strings.ToLower(alias.Alias+"@"+alias.Domain) {
		return ErrAliasConflict // F6560
	}
	key := alias.Domain + ":" + strings.ToLower(alias.Alias)
	data, err := json.Marshal(alias)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	return d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketAliases))
		if b == nil {
			return fmt.Errorf("bucket not found: %s", BucketAliases)
		}
		// F6081: never resurrect an alias deleted after the read.
		if b.Get([]byte(key)) == nil {
			return ErrAliasNotFound
		}
		if aliasCycleTx(tx, alias.Domain, key, alias.Target) {
			return ErrAliasConflict // F7250
		}
		return b.Put([]byte(key), data)
	})
}

// DeleteAlias removes an alias
func (d *DB) DeleteAlias(domain, localPart string) error {
	key := domain + ":" + strings.ToLower(localPart)
	return d.Delete(BucketAliases, key)
}
