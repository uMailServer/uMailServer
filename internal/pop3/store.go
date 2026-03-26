package pop3

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// MaildirStore implements Mailstore using maildir format
type MaildirStore struct {
	basePath string
}

// NewMaildirStore creates a new maildir-based store
func NewMaildirStore(basePath string) *MaildirStore {
	return &MaildirStore{basePath: basePath}
}

// userPath returns the path for a user's maildir
func (s *MaildirStore) userPath(user string) string {
	// Sanitize user (replace @ with _)
	safeUser := strings.ReplaceAll(user, "@", "_")
	return filepath.Join(s.basePath, safeUser)
}

// newPath returns the new maildir path
func (s *MaildirStore) newPath(user string) string {
	return filepath.Join(s.userPath(user), "new")
}

// curPath returns the cur maildir path
func (s *MaildirStore) curPath(user string) string {
	return filepath.Join(s.userPath(user), "cur")
}

// Authenticate validates user credentials
func (s *MaildirStore) Authenticate(username, password string) (bool, error) {
	// In a real implementation, this would check against the database
	// For now, just check if the user's maildir exists
	path := s.userPath(username)
	_, err := os.Stat(path)
	return err == nil, nil
}

// ListMessages lists all messages for a user
func (s *MaildirStore) ListMessages(user string) ([]*Message, error) {
	var messages []*Message

	// Read new directory
	newPath := s.newPath(user)
	newMsgs, _ := s.readMaildir(newPath, "new")
	messages = append(messages, newMsgs...)

	// Read cur directory
	curPath := s.curPath(user)
	curMsgs, _ := s.readMaildir(curPath, "cur")
	messages = append(curMsgs, curMsgs...)

	// Sort by time (filename starts with timestamp)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].UID < messages[j].UID
	})

	// Set indices
	for i, msg := range messages {
		msg.Index = i + 1
	}

	return messages, nil
}

// readMaildir reads messages from a maildir subdirectory
func (s *MaildirStore) readMaildir(path, subdir string) ([]*Message, error) {
	var messages []*Message

	entries, err := os.ReadDir(path)
	if err != nil {
		return messages, nil // Return empty if directory doesn't exist
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		msg := &Message{
			Index: 0, // Will be set by ListMessages
			UID:   entry.Name(),
			Size:  info.Size(),
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// GetMessage gets a specific message (for interface compatibility)
func (s *MaildirStore) GetMessage(user string, index int) (*Message, error) {
	messages, err := s.ListMessages(user)
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(messages) {
		return nil, os.ErrNotExist
	}

	return messages[index], nil
}

// DeleteMessage marks a message for deletion (removes it)
func (s *MaildirStore) DeleteMessage(user string, index int) error {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return err
	}

	// Try to delete from new first
	newPath := filepath.Join(s.newPath(user), msg.UID)
	if err := os.Remove(newPath); err == nil {
		return nil
	}

	// Try cur
	curPath := filepath.Join(s.curPath(user), msg.UID)
	return os.Remove(curPath)
}

// GetMessageCount returns the number of messages
func (s *MaildirStore) GetMessageCount(user string) (int, error) {
	messages, err := s.ListMessages(user)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

// GetMessageSize returns the size of a message
func (s *MaildirStore) GetMessageSize(user string, index int) (int64, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return 0, err
	}
	return msg.Size, nil
}

// GetMessageData loads message data
func (s *MaildirStore) GetMessageData(user string, index int) ([]byte, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return nil, err
	}

	// Try new directory
	newPath := filepath.Join(s.newPath(user), msg.UID)
	if data, err := os.ReadFile(newPath); err == nil {
		return data, nil
	}

	// Try cur directory
	curPath := filepath.Join(s.curPath(user), msg.UID)
	return os.ReadFile(curPath)
}

// SimpleMemoryStore is an in-memory store for testing
type SimpleMemoryStore struct {
	mu       sync.Mutex
	messages map[string][]*Message
}

// NewSimpleMemoryStore creates a new in-memory store
func NewSimpleMemoryStore() *SimpleMemoryStore {
	return &SimpleMemoryStore{
		messages: make(map[string][]*Message),
	}
}

// Authenticate validates user credentials
func (s *SimpleMemoryStore) Authenticate(username, password string) (bool, error) {
	return true, nil
}

// ListMessages lists all messages for a user
func (s *SimpleMemoryStore) ListMessages(user string) ([]*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.messages[user], nil
}

// AddMessage adds a message for testing
func (s *SimpleMemoryStore) AddMessage(user string, msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages[user] = append(s.messages[user], msg)
}

// GetMessage gets a specific message
func (s *SimpleMemoryStore) GetMessage(user string, index int) (*Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msgs, ok := s.messages[user]; ok && index < len(msgs) {
		return msgs[index], nil
	}
	return nil, os.ErrNotExist
}

// GetMessageData loads message data
func (s *SimpleMemoryStore) GetMessageData(user string, index int) ([]byte, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

// DeleteMessage deletes a message
func (s *SimpleMemoryStore) DeleteMessage(user string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if msgs, ok := s.messages[user]; ok && index < len(msgs) {
		msgs[index] = nil
	}
	return nil
}

// GetMessageCount returns the number of messages
func (s *SimpleMemoryStore) GetMessageCount(user string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.messages[user]), nil
}

// GetMessageSize returns the size of a message
func (s *SimpleMemoryStore) GetMessageSize(user string, index int) (int64, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return 0, err
	}
	return msg.Size, nil
}
