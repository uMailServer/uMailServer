package pop3

import (
	"fmt"
	"strings"

	"github.com/umailserver/umailserver/internal/storage"
)

// BboltStore adapts the storage.Database and MessageStore to the POP3 Mailstore interface
type BboltStore struct {
	db       *storage.Database
	msgStore *storage.MessageStore
}

// NewBboltStore creates a new bbolt-backed POP3 store
func NewBboltStore(db *storage.Database, msgStore *storage.MessageStore) *BboltStore {
	return &BboltStore{
		db:       db,
		msgStore: msgStore,
	}
}

// Authenticate validates user credentials (not used when authFunc is set on server)
func (s *BboltStore) Authenticate(username, password string) (bool, error) {
	// Authentication is handled by the server's authFunc callback
	return false, nil
}

// ListMessages lists all messages for a user from their INBOX
func (s *BboltStore) ListMessages(user string) ([]*Message, error) {
	uids, err := s.db.GetMessageUIDs(user, "INBOX")
	if err != nil {
		return nil, err
	}

	var messages []*Message
	for i, uid := range uids {
		meta, err := s.db.GetMessageMetadata(user, "INBOX", uid)
		if err != nil {
			continue
		}

		msg := &Message{
			Index: i + 1, // 1-based index for POP3
			UID:   fmt.Sprintf("%d", uid),
			Size:  meta.Size,
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// GetMessage gets a specific message by 1-based index
func (s *BboltStore) GetMessage(user string, index int) (*Message, error) {
	messages, err := s.ListMessages(user)
	if err != nil {
		return nil, err
	}

	if index < 1 || index > len(messages) {
		return nil, fmt.Errorf("message index out of range")
	}

	return messages[index-1], nil
}

// GetMessageData loads the full message data for a given message index
func (s *BboltStore) GetMessageData(user string, index int) ([]byte, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return nil, err
	}

	// Try to read from message store using UID as message ID
	messageID := msg.UID
	data, err := s.msgStore.ReadMessage(user, messageID)
	if err != nil {
		// Fallback: try with INBOX prefix
		data, err = s.msgStore.ReadMessage(user, "INBOX/"+messageID)
		if err != nil {
			return nil, fmt.Errorf("failed to read message data: %w", err)
		}
	}

	return data, nil
}

// DeleteMessage deletes a message by marking it for deletion
func (s *BboltStore) DeleteMessage(user string, index int) error {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return err
	}

	uidStr := msg.UID
	// Parse UID string to uint32
	var uid uint32
	for _, c := range uidStr {
		if c >= '0' && c <= '9' {
			uid = uid*10 + uint32(c-'0')
		}
	}

	// Add \Deleted flag to the message
	meta, err := s.db.GetMessageMetadata(user, "INBOX", uid)
	if err != nil {
		return err
	}

	// Check if already has \Deleted flag
	hasDeleted := false
	for _, f := range meta.Flags {
		if strings.EqualFold(f, "\\Deleted") || strings.EqualFold(f, "Deleted") {
			hasDeleted = true
			break
		}
	}

	if !hasDeleted {
		meta.Flags = append(meta.Flags, "\\Deleted")
	}

	return s.db.StoreMessageMetadata(user, "INBOX", uid, meta)
}

// GetMessageCount returns the number of messages in the user's INBOX
func (s *BboltStore) GetMessageCount(user string) (int, error) {
	messages, err := s.ListMessages(user)
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

// GetMessageSize returns the size of a specific message
func (s *BboltStore) GetMessageSize(user string, index int) (int64, error) {
	msg, err := s.GetMessage(user, index)
	if err != nil {
		return 0, err
	}
	return msg.Size, nil
}
