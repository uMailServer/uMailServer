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

		// RFC 1939 §4: messages flagged \Deleted (via POP3 DELE+UPDATE or an
		// IMAP client) are no longer part of the POP3 maildrop view.
		hasDeleted := false
		for _, f := range meta.Flags {
			if strings.EqualFold(f, "\\Deleted") || strings.EqualFold(f, "Deleted") {
				hasDeleted = true
				break
			}
		}
		if hasDeleted {
			continue
		}

		msg := &Message{
			Index:  i + 1, // 1-based index for POP3
			UID:    fmt.Sprintf("%d", uid),
			BlobID: meta.MessageID,
			Size:   meta.Size,
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

	if msg.BlobID == "" {
		return nil, fmt.Errorf("message data unavailable: no blob id in metadata")
	}

	// Raw octets are content-hash-addressed; the blob id comes from the
	// message metadata (MessageID), not the POP3 index or numeric UID.
	data, err := s.msgStore.ReadMessage(user, msg.BlobID)
	if err != nil {
		return nil, fmt.Errorf("failed to read message data: %w", err)
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

	// RFC 1939 §4: UPDATE removes the message. Flagging \Deleted only left the
	// metadata and blob on disk forever, so the user's quota was never freed
	// (F5743). Remove the metadata row, then the content-addressed blob once no
	// mailbox still references it.
	meta, err := s.db.GetMessageMetadata(user, "INBOX", uid)
	if err != nil {
		return err
	}
	if err := s.db.DeleteMessage(user, "INBOX", uid); err != nil {
		return err
	}
	if meta != nil && meta.MessageID != "" && !s.blobReferenced(user, meta.MessageID) {
		_ = s.msgStore.DeleteMessage(user, meta.MessageID)
	}
	return nil
}

// blobReferenced reports whether any of the user's mailboxes still holds a
// message whose content blob is blobID (copies share one blob).
func (s *BboltStore) blobReferenced(user, blobID string) bool {
	mailboxes, err := s.db.ListMailboxes(user)
	if err != nil {
		return true // unknown: keep the blob
	}
	for _, mbox := range mailboxes {
		uids, err := s.db.GetMessageUIDs(user, mbox)
		if err != nil {
			return true
		}
		for _, uid := range uids {
			if m, err := s.db.GetMessageMetadata(user, mbox, uid); err == nil && m != nil && m.MessageID == blobID {
				return true
			}
		}
	}
	return false
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
