package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// isValidMessageID checks that messageID is safe to use in a file path
// MessageIDs must not contain null bytes, path traversal, and must be long enough
func isValidMessageID(messageID string) bool {
	if messageID == "" {
		return false
	}
	// Must be at least 4 characters for the path slicing (messageID[:2], messageID[2:4])
	if len(messageID) < 4 {
		return false
	}
	// Block null bytes (cannot occur in valid Go strings but check anyway)
	for _, c := range messageID {
		if c == 0 {
			return false
		}
	}
	// Block path traversal sequences (but allow forward slashes for maildir paths)
	if strings.Contains(messageID, "..") {
		return false
	}
	// MessageID should not be too long (prevent other injection)
	if len(messageID) > 256 {
		return false
	}
	return true
}

// MessageStore handles storage of raw message data
type MessageStore struct {
	basePath string
	// quotaLocks serializes the usage-check-then-store step of
	// StoreMessageWithQuota per user (F5720).
	quotaLocks sync.Map // user -> *sync.Mutex
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
// is already stored adds no bytes and is always accepted. Writers that use
// plain StoreMessage are not serialized with this path.
func (s *MessageStore) StoreMessageWithQuota(user string, data []byte, limit int64) (string, error) {
	if err := validatePathComponent(user); err != nil {
		return "", err
	}
	if limit <= 0 {
		return s.StoreMessage(user, data)
	}
	m, _ := s.quotaLocks.LoadOrStore(user, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])
	if info, err := os.Stat(filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)); err == nil &&
		info.Mode().IsRegular() && info.Size() == int64(len(data)) {
		return messageID, nil
	}
	used, err := s.UserUsage(user)
	if err != nil {
		return "", err
	}
	if used+int64(len(data)) > limit {
		return "", &QuotaExceededError{User: user, Used: used, Need: int64(len(data)), Limit: limit}
	}
	return s.StoreMessage(user, data)
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
			syncDirPath(filepath.Dir(msgPath)) // F5952: persist the new dir entry
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
			return messageID, nil // Already exists
		}
		// A body of the wrong size under a content hash is a leftover from a
		// pre-F5096 partial write; replace it with the complete copy.
		if err := os.Rename(tmpPath, msgPath); err != nil {
			return "", err
		}
		syncDirPath(filepath.Dir(msgPath))
		break
	}

	return messageID, nil
}

// syncDirPath best-effort fsyncs a directory so a new entry survives a crash.
func syncDirPath(dir string) {
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

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.ReadFile(filepath.Clean(msgPath))
}

// DeleteMessage deletes a message
func (s *MessageStore) DeleteMessage(user, messageID string) error {
	if err := validatePathComponent(user); err != nil {
		return err
	}
	if !isValidMessageID(messageID) {
		return fmt.Errorf("invalid message ID")
	}

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.Remove(msgPath)
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
