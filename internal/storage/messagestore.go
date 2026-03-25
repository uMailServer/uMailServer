package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// MessageStore handles storage of raw message data
type MessageStore struct {
	basePath string
}

// NewMessageStore creates a new message store
func NewMessageStore(basePath string) (*MessageStore, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, err
	}

	return &MessageStore{basePath: basePath}, nil
}

// Close closes the message store
func (s *MessageStore) Close() error {
	return nil
}

// StoreMessage stores a message and returns its ID
func (s *MessageStore) StoreMessage(user string, data []byte) (string, error) {
	// Generate message ID from content hash
	hash := sha256.Sum256(data)
	messageID := hex.EncodeToString(hash[:])

	// Create user directory
	userPath := filepath.Join(s.basePath, user)
	if err := os.MkdirAll(userPath, 0755); err != nil {
		return "", err
	}

	// Store message using hash-based filename
	// Split into subdirectories for better filesystem performance
	msgPath := filepath.Join(userPath, messageID[:2], messageID[2:4], messageID)
	if err := os.MkdirAll(filepath.Dir(msgPath), 0755); err != nil {
		return "", err
	}

	// Check if already exists
	if _, err := os.Stat(msgPath); err == nil {
		return messageID, nil // Already exists
	}

	if err := os.WriteFile(msgPath, data, 0644); err != nil {
		return "", err
	}

	return messageID, nil
}

// ReadMessage reads a message by ID
func (s *MessageStore) ReadMessage(user, messageID string) ([]byte, error) {
	if len(messageID) < 4 {
		return nil, fmt.Errorf("invalid message ID")
	}

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.ReadFile(msgPath)
}

// DeleteMessage deletes a message
func (s *MessageStore) DeleteMessage(user, messageID string) error {
	if len(messageID) < 4 {
		return fmt.Errorf("invalid message ID")
	}

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	return os.Remove(msgPath)
}

// MessageExists checks if a message exists
func (s *MessageStore) MessageExists(user, messageID string) bool {
	if len(messageID) < 4 {
		return false
	}

	msgPath := filepath.Join(s.basePath, user, messageID[:2], messageID[2:4], messageID)
	_, err := os.Stat(msgPath)
	return err == nil
}
