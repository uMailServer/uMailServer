package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// errInvalidPath is returned when a user-provided path component contains
// path separators or traversal sequences.
var errInvalidPath = errors.New("invalid path component: contains separator or traversal")

// validatePathComponent checks that s does not contain path separators or "..".
func validatePathComponent(s string) error {
	if s == "" || s == ".." || strings.ContainsAny(s, "/\\") {
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
}

// NewMessageStore creates a new message store
func NewMessageStore(basePath string) (*MessageStore, error) {
	if err := os.MkdirAll(basePath, 0750); err != nil {
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
	if err := os.MkdirAll(userPath, 0750); err != nil {
		return "", err
	}

	// Store message using hash-based filename
	// Split into subdirectories for better filesystem performance
	msgPath := filepath.Join(userPath, messageID[:2], messageID[2:4], messageID)
	if err := os.MkdirAll(filepath.Dir(msgPath), 0750); err != nil {
		return "", err
	}

	// Check if already exists
	file, err := os.OpenFile(msgPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return messageID, nil // Already exists
		}
		return "", err
	}
	_, err = file.Write(data)
	file.Close()
	if err != nil {
		return "", err
	}

	return messageID, nil
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
	return os.ReadFile(msgPath)
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
