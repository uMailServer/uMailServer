package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errInvalidPath is returned when a user-provided path component contains
// path separators or traversal sequences.
var errInvalidPath = errors.New("invalid path component: contains separator or traversal")

// validatePathComponent checks that s does not contain path separators or "..".
func validatePathComponent(s string) error {
	// F5721: "." resolves to the store root, outside any user's directory.
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
		return errInvalidPath
	}
	return nil
}

// isValidMessageID reports whether messageID is a blob key this store can
// have produced: the lowercase-hex SHA-256 content hash (64 chars), or the
// copy-qualified form "<hash>_<uid>_<base64url(mailbox)>" that the API and
// JMAP layers hand out (F7332: the old check accepted any string without
// "..", including path separators and absolute-looking prefixes). Hex case is
// tolerated for ids stored by older builds; the path is derived from the
// first four characters, so qualified ids resolve to their hash's file.
func isValidMessageID(messageID string) bool {
	if len(messageID) < 64 || len(messageID) > 256 {
		return false
	}
	for i := 0; i < 64; i++ {
		c := messageID[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	rest := messageID[64:]
	if rest == "" {
		return true
	}
	// rest = "_" digits "_" base64url; base64url may itself contain '_'.
	if rest[0] != '_' {
		return false
	}
	rest = rest[1:]
	sep := strings.IndexByte(rest, '_')
	if sep <= 0 || sep == len(rest)-1 {
		return false
	}
	for i := 0; i < sep; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	for i := sep + 1; i < len(rest); i++ {
		c := rest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// MessageStore handles storage of raw message data
type MessageStore struct {
	basePath string
	// quotaLocks serializes the usage-check-then-store step of
	// StoreMessageWithQuota per user (F5720).
	quotaLocks sync.Map // user -> *sync.Mutex

	// refChecker, when set, makes DeleteMessage reference-aware (F6362).
	refChecker atomic.Pointer[BlobReferenceChecker]
	// userGuards are the cross-package "reference change vs blob removal"
	// mutexes handed out by UserGuard.
	userGuards sync.Map // user -> *sync.Mutex

	// Test seams (nil in production), used to order concurrent writers
	// deterministically.
	afterQuotaCheck func()           // quota check passed, store not yet started
	onLockContended func()           // a writer found the user lock held
	onDirSync       func(dir string) // a directory is being fsynced
}

// BlobReferenceChecker reports which of ids the user still references
// (message rows or pending uploads). Database.BlobReferenced implements it.
type BlobReferenceChecker func(user string, ids []string) (map[string]bool, error)

// ErrBlobReferenced is returned by DeleteMessage when the content-addressed
// blob is still referenced by another message of the same user (identical
// content shares one file), or when that could not be ruled out. Nothing was
// removed, so callers must not release quota for it.
var ErrBlobReferenced = errors.New("blob still referenced")

// SetBlobReferenceChecker installs the reference lookup that makes
// DeleteMessage safe for shared blobs. Call it once at wiring time, before
// the store is used concurrently. Without a checker DeleteMessage removes the
// file unconditionally (legacy behaviour; callers must check references).
func (s *MessageStore) SetBlobReferenceChecker(c BlobReferenceChecker) {
	if c == nil {
		s.refChecker.Store(nil)
		return
	}
	s.refChecker.Store(&c)
}

// UserGuard returns the per-user mutex that serializes "store blob + write
// its metadata row" against "drop metadata row + release blob". Every
// package that does either must hold it across the whole sequence, so a
// release can never observe the window between a dedupe hit and the new
// reference becoming visible. It is distinct from the internal blob lock and
// is not held by MessageStore methods themselves, so holding it while
// calling them is safe.
func (s *MessageStore) UserGuard(user string) *sync.Mutex {
	m, _ := s.userGuards.LoadOrStore(user, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// lockUser takes the per-user write lock shared by every blob writer and
// deleter (R154: SMTP delivery, IMAP APPEND and DeleteMessage previously
// serialized only on the quota path, so plain stores raced the quota check
// and deletes raced dedupe hits).
func (s *MessageStore) lockUser(user string) func() {
	m, _ := s.quotaLocks.LoadOrStore(user, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if !mu.TryLock() {
		if s.onLockContended != nil {
			s.onLockContended()
		}
		mu.Lock()
	}
	return mu.Unlock
}

// ErrQuotaExceeded is matched (errors.Is) by every *QuotaExceededError.
// Callers can map it to SMTP 452 (temporary) or 552 (permanent) and IMAP
// [OVERQUOTA].
var ErrQuotaExceeded = errors.New("quota exceeded")

// QuotaExceededError reports a store refused because it would push a user
// past their byte limit.
type QuotaExceededError struct {
	User  string
	Used  int64
	Need  int64
	Limit int64
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded for %s: used %d + %d > limit %d", e.User, e.Used, e.Need, e.Limit)
}

// Is makes errors.Is(err, ErrQuotaExceeded) true.
func (e *QuotaExceededError) Is(target error) bool { return target == ErrQuotaExceeded }

// UserUsage returns the bytes of stored message bodies for user, recounted
// from disk (so deletes are always reflected). In-flight ".tmp-" files are
// not counted. A user with no data uses 0 bytes.
func (s *MessageStore) UserUsage(user string) (int64, error) {
	if err := validatePathComponent(user); err != nil {
		return 0, err
	}
	var used int64
	err := filepath.Walk(filepath.Join(s.basePath, user), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode().IsRegular() && !strings.HasPrefix(info.Name(), ".tmp-") {
			used += info.Size()
		}
		return nil
	})
	return used, err
}

// StoreMessageWithQuota is StoreMessage with a per-user byte limit
// (limit <= 0 means unlimited). The usage check and the store are serialized
// per user, so concurrent calls cannot jointly exceed the limit. Content that
// is already stored adds no bytes and is always accepted. StoreMessage and
// DeleteMessage share the same per-user lock.
func (s *MessageStore) StoreMessageWithQuota(user string, data []byte, limit int64) (string, error) {
	if err := validatePathComponent(user); err != nil {
		return "", err
	}
	defer s.lockUser(user)()
	if limit <= 0 {
		return s.storeMessageLocked(user, data)
	}

	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])
	if info, err := os.Stat(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)); err == nil &&
		info.Mode().IsRegular() && info.Size() == int64(len(data)) {
		touchBlob(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID))
		s.syncDir(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4]))
		if s.afterQuotaCheck != nil {
			s.afterQuotaCheck()
		}
		return messageID, nil
	}
	used, err := s.UserUsage(user)
	if err != nil {
		return "", err
	}
	if used+int64(len(data)) > limit {
		return "", &QuotaExceededError{User: user, Used: used, Need: int64(len(data)), Limit: limit}
	}
	if s.afterQuotaCheck != nil {
		s.afterQuotaCheck()
	}
	return s.storeMessageLocked(user, data)
}

// NewMessageStore creates a new message store
func NewMessageStore(basePath string) (*MessageStore, error) {
	if err := os.MkdirAll(basePath, 0o750); err != nil {
		return nil, err
	}

	return &MessageStore{basePath: basePath}, nil
}

// Close closes the message store
func (s *MessageStore) Close() error {
	return nil
}

// maxMessageSize is a defensive limit to prevent enormous messages from
// exhausting memory during hashing or storage.
const maxMessageSize = 100 * 1024 * 1024 // 100 MB

// StoreMessage stores a message and returns its ID
func (s *MessageStore) StoreMessage(user string, data []byte) (string, error) {
	if err := validatePathComponent(user); err != nil {
		return "", err
	}
	defer s.lockUser(user)()
	return s.storeMessageLocked(user, data)
}

// storeMessageLocked requires the caller to hold the per-user lock.
func (s *MessageStore) storeMessageLocked(user string, data []byte) (string, error) {
	if len(data) > maxMessageSize {
		return "", fmt.Errorf("message exceeds maximum size of %d bytes", maxMessageSize)
	}
	// Generate message ID from content hash
	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])

	// Create user directory
	userPath := filepath.Join(s.basePath, user)
	if err := os.MkdirAll(userPath, 0o750); err != nil {
		return "", err
	}

	// Store message using hash-based filename
	// Split into subdirectories for better filesystem performance
	msgPath := filepath.Join(userPath, messageID[:2], messageID[2:4], messageID)
	if err := os.MkdirAll(filepath.Dir(msgPath), 0o750); err != nil {
		return "", err
	}

	// F5096: write and fsync a temp file in the same directory, then publish
	// it under the content-addressed name with a hard link (atomic and
	// non-overwriting). A failed or interrupted write therefore never leaves
	// a truncated body under msgPath that later deliveries would dedup onto.
	removeStaleTemps(filepath.Dir(msgPath))
	tmp, err := os.CreateTemp(filepath.Dir(msgPath), ".tmp-"+messageID+"-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}

	// F5951: a concurrent DeleteMessage can remove msgPath between the
	// EEXIST from Link and the Stat; retry the publish instead of failing.
	for attempt := 0; attempt < 5; attempt++ {
		err := os.Link(tmpPath, msgPath)
		if err == nil {
			s.syncNewBlobDirs(userPath, msgPath) // F5952/F7337: persist the entry and new shard dirs
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
		info, statErr := os.Stat(msgPath)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return "", statErr
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("message path is not a regular file: %s", msgPath)
		}
		if info.Size() == int64(len(data)) {
			// F7336: a dedupe hit re-references the blob; restart its
			// orphan-GC grace period and persist its directory entry.
			touchBlob(msgPath)
			s.syncDir(filepath.Dir(msgPath))
			return messageID, nil // Already exists
		}
		// A body of the wrong size under a content hash is a leftover from a
		// pre-F5096 partial write; replace it with the complete copy.
		if err := os.Rename(tmpPath, msgPath); err != nil {
			return "", err
		}
		s.syncNewBlobDirs(userPath, msgPath)
		break
	}

	return messageID, nil
}

// syncNewBlobDirs fsyncs the blob's directory, its shard parent, the user
// directory and the store root, so freshly created shard directories survive
// a crash together with the blob entry (F7337: only the leaf was synced, so a
// new "ab/cd" chain could vanish after a power loss).
func (s *MessageStore) syncNewBlobDirs(userPath, msgPath string) {
	leaf := filepath.Dir(msgPath)
	s.syncDir(leaf)
	s.syncDir(filepath.Dir(leaf))
	s.syncDir(userPath)
	s.syncDir(s.basePath)
}

// touchBlob refreshes a blob's mtime; best-effort.
func touchBlob(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// syncDir best-effort fsyncs a directory so a new entry survives a crash.
func (s *MessageStore) syncDir(dir string) {
	if s.onDirSync != nil {
		s.onDirSync(dir)
	}
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}

// staleTempAge is how old an in-flight ".tmp-" file must be before it is
// considered an orphan from a crashed store.
const staleTempAge = time.Hour

// removeStaleTemps deletes orphaned ".tmp-" files (F5952: a crash between
// CreateTemp and Link otherwise leaks them forever) from one blob directory.
func removeStaleTemps(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTempAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ReadMessage reads a message by ID
func (s *MessageStore) ReadMessage(user, messageID string) ([]byte, error) {
	if err := validatePathComponent(user); err != nil {
		return nil, err
	}
	if !isValidMessageID(messageID) {
		return nil, fmt.Errorf("invalid message ID")
	}

	msgPath := filepath.Clean(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID))
	f, err := os.Open(msgPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// F6668: never load a file larger than the store accepts (a corrupted or
	// replaced blob must not exhaust memory). Read at most max+1 so a file
	// that grows between Stat and Read is caught too.
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("message path is not a regular file")
	}
	if info.Size() > maxMessageSize {
		return nil, fmt.Errorf("message exceeds maximum size of %d bytes", maxMessageSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMessageSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxMessageSize {
		return nil, fmt.Errorf("message exceeds maximum size of %d bytes", maxMessageSize)
	}
	return data, nil
}

// orphanGCGrace is how old a blob file must be before GCOrphanBlobs may
// remove it. Delivery paths that do not hold UserGuard across "store blob +
// write metadata row" have a short window in which a fresh blob has no row.
const orphanGCGrace = time.Hour

// GCOrphanBlobs removes the user's blobs that have no metadata row and no
// pending upload (as reported by the installed reference checker), plus stale
// ".tmp-" files. Blobs younger than orphanGCGrace are kept. It refuses to run
// without a reference checker, since "unreferenced" cannot be decided then.
// The sweep holds UserGuard(user) and the per-user blob lock, so it is
// serialized against every cooperating writer/deleter. It is not scheduled by
// the store; operators call it explicitly. It returns the number of blobs
// removed.
func (s *MessageStore) GCOrphanBlobs(user string) (int, error) {
	if err := validatePathComponent(user); err != nil {
		return 0, err
	}
	p := s.refChecker.Load()
	if p == nil {
		return 0, errors.New("orphan GC requires a blob reference checker")
	}
	g := s.UserGuard(user)
	g.Lock()
	defer g.Unlock()
	defer s.lockUser(user)()

	root := filepath.Join(s.basePath, user)
	type cand struct{ id, path string }
	var cands []cand
	var dirs []string
	cutoff := time.Now().Add(-orphanGCGrace)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if path != root {
				dirs = append(dirs, path)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".tmp-") {
			if time.Since(info.ModTime()) > staleTempAge {
				_ = os.Remove(path)
			}
			return nil
		}
		// Only canonical content-addressed blobs: <root>/ab/cd/<64 hex>.
		if len(name) != 64 || !isLowerHex(name) {
			return nil
		}
		if filepath.Dir(path) != filepath.Join(root, name[:2], name[2:4]) {
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		cands = append(cands, cand{name, path})
		return nil
	})
	if err != nil {
		return 0, err
	}
	removed := 0
	if len(cands) > 0 {
		ids := make([]string, len(cands))
		for i, c := range cands {
			ids[i] = c.id
		}
		refs, err := (*p)(user, ids)
		if err != nil {
			return 0, fmt.Errorf("reference check failed: %w", err)
		}
		for _, c := range cands {
			if refs[c.id] {
				continue
			}
			if err := os.Remove(c.path); err == nil {
				removed++
			} else if !os.IsNotExist(err) {
				return removed, err
			}
		}
	}
	// Best-effort prune of now-empty shard directories (deepest first).
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // fails harmlessly when not empty
	}
	return removed, nil
}

// BlobOwners returns the users that have a blob directory in this store
// (round 61): the layout is basePath/<user>/ab/cd/<64hex>, so the store
// root's directory entries are the owner ids. Only directories are listed;
// entries are sorted for deterministic sweeps.
func (s *MessageStore) BlobOwners() ([]string, error) {
	ents, err := os.ReadDir(s.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var owners []string
	for _, e := range ents {
		if e.IsDir() {
			owners = append(owners, e.Name())
		}
	}
	sort.Strings(owners)
	return owners, nil
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// DeleteMessage removes a blob. With a reference checker installed it only
// removes the file when no message row or pending upload of the user still
// references it, and otherwise returns ErrBlobReferenced without touching
// anything (F6362: identical messages share one file). Callers must drop their
// own metadata row first.
func (s *MessageStore) DeleteMessage(user, messageID string) error {
	if err := validatePathComponent(user); err != nil {
		return err
	}
	if !isValidMessageID(messageID) {
		return fmt.Errorf("invalid message ID")
	}

	defer s.lockUser(user)()
	if p := s.refChecker.Load(); p != nil {
		refs, err := (*p)(user, []string{messageID})
		if err != nil {
			return fmt.Errorf("%w: reference check failed: %v", ErrBlobReferenced, err)
		}
		if refs[messageID] {
			return ErrBlobReferenced
		}
	}
	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.Remove(msgPath)
}

// DeleteMessageUnchecked removes the blob without consulting the reference
// checker. Only for callers that have just verified the references
// themselves (e.g. a batch scan).
func (s *MessageStore) DeleteMessageUnchecked(user, messageID string) error {
	if err := validatePathComponent(user); err != nil {
		return err
	}
	if !isValidMessageID(messageID) {
		return fmt.Errorf("invalid message ID")
	}

	defer s.lockUser(user)()
	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.Remove(msgPath)
}

// MessageSize returns the on-disk size of a stored message blob.
func (s *MessageStore) MessageSize(user, messageID string) (int64, error) {
	if err := validatePathComponent(user); err != nil {
		return 0, err
	}
	if !isValidMessageID(messageID) {
		return 0, fmt.Errorf("invalid message ID")
	}
	fi, err := os.Stat(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID))
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// MessageExists checks if a message exists
func (s *MessageStore) MessageExists(user, messageID string) bool {
	if err := validatePathComponent(user); err != nil {
		return false
	}
	if !isValidMessageID(messageID) {
		return false
	}

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	_, err := os.Stat(msgPath)
	return err == nil
}
