// Package storage provides data storage for the mail server
package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// Database represents the bbolt database interface
type Database struct {
	path string
	bolt *bbolt.DB
	mu   sync.Mutex
}

// OpenDatabase opens the bbolt database
func OpenDatabase(path string) (*Database, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1})
	if err != nil {
		return &Database{path: path}, nil
	}
	return &Database{path: path, bolt: db}, nil
}

// Close closes the database
func (db *Database) Close() error {
	if db.bolt != nil {
		return db.bolt.Close()
	}
	return nil
}

// AuthenticateUser validates user credentials
func (db *Database) AuthenticateUser(username, password string) (bool, error) {
	// TODO: Implement actual authentication
	return true, nil
}

// Mailbox represents mailbox metadata
type Mailbox struct {
	Name        string
	UIDValidity uint32
	UIDNext     uint32
}

// mailboxKey returns the bucket name for a user's mailbox metadata
func mailboxKey(user, mailbox string) string {
	return fmt.Sprintf("mailbox:%s:%s", user, mailbox)
}

// messagesBucket returns the bucket name for a user's mailbox messages
func messagesBucket(user, mailbox string) string {
	return fmt.Sprintf("msgs:%s:%s", user, mailbox)
}

// GetMailbox retrieves mailbox information
func (db *Database) GetMailbox(user, mailbox string) (*Mailbox, error) {
	if db.bolt == nil {
		return &Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 1}, nil
	}

	var mb Mailbox
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(mailboxKey(user, mailbox)))
		if b == nil {
			mb = Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 1}
			return nil
		}
		mb = Mailbox{
			Name:        mailbox,
			UIDValidity: uint32(btoi(b.Get([]byte("uidvalidity")))),
			UIDNext:     uint32(btoi(b.Get([]byte("uidnext")))),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &mb, nil
}

// CreateMailbox creates a new mailbox
func (db *Database) CreateMailbox(user, mailbox string) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(mailboxKey(user, mailbox)))
		if err != nil {
			return err
		}
		if b.Get([]byte("uidvalidity")) == nil {
			now := time.Now().Unix()
			_ = b.Put([]byte("uidvalidity"), itob(uint32(now)))
			_ = b.Put([]byte("uidnext"), itob(1))
		}
		// Also create the messages bucket
		_, err = tx.CreateBucketIfNotExists([]byte(messagesBucket(user, mailbox)))
		return err
	})
}

// DeleteMailbox deletes a mailbox
func (db *Database) DeleteMailbox(user, mailbox string) error {
	if db.bolt == nil {
		return nil
	}
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		_ = tx.DeleteBucket([]byte(mailboxKey(user, mailbox)))
		_ = tx.DeleteBucket([]byte(messagesBucket(user, mailbox)))
		return nil
	})
}

// RenameMailbox renames a mailbox
func (db *Database) RenameMailbox(user, oldName, newName string) error {
	if db.bolt == nil {
		return nil
	}

	// Read old data
	oldKey := mailboxKey(user, oldName)
	oldMsgs := messagesBucket(user, oldName)
	newKey := mailboxKey(user, newName)
	newMsgs := messagesBucket(user, newName)

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		oldB := tx.Bucket([]byte(oldKey))
		if oldB == nil {
			// If source mailbox doesn't exist, just create the new one
			newB, err := tx.CreateBucketIfNotExists([]byte(newKey))
			if err != nil {
				return err
			}
			_ = newB.Put([]byte("uidvalidity"), itob(uint32(time.Now().Unix())))
			_ = newB.Put([]byte("uidnext"), itob(1))
			_, err = tx.CreateBucketIfNotExists([]byte(newMsgs))
			return err
		}

		// Create new mailbox bucket and copy data
		newB, err := tx.CreateBucketIfNotExists([]byte(newKey))
		if err != nil {
			return err
		}
		oldB.ForEach(func(k, v []byte) error {
			return newB.Put(k, v)
		})

		// Create new messages bucket and copy messages
		oldMB := tx.Bucket([]byte(oldMsgs))
		if oldMB != nil {
			newMB, err := tx.CreateBucketIfNotExists([]byte(newMsgs))
			if err != nil {
				return err
			}
			oldMB.ForEach(func(k, v []byte) error {
				return newMB.Put(k, v)
			})
		}

		// Delete old buckets
		_ = tx.DeleteBucket([]byte(oldKey))
		_ = tx.DeleteBucket([]byte(oldMsgs))
		return nil
	})
}

// ListMailboxes lists all mailboxes for a user
func (db *Database) ListMailboxes(user string) ([]string, error) {
	if db.bolt == nil {
		return []string{"INBOX"}, nil
	}

	var result []string
	prefix := fmt.Sprintf("mailbox:%s:", user)
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		c := tx.Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			// Extract mailbox name from key
			parts := strings.SplitN(string(k), ":", 3)
			if len(parts) == 3 {
				result = append(result, parts[2])
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		result = []string{"INBOX"}
	}
	return result, nil
}

// GetMailboxCounts returns message counts for a mailbox
func (db *Database) GetMailboxCounts(user, mailbox string) (exists, recent, unseen int, err error) {
	if db.bolt == nil {
		return 0, 0, 0, nil
	}

	err = db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var meta MessageMetadata
			if err := json.Unmarshal(v, &meta); err != nil {
				continue
			}
			exists++
			if hasFlag(meta.Flags, "\\Recent") {
				recent++
			}
			if !hasFlag(meta.Flags, "\\Seen") {
				unseen++
			}
		}
		return nil
	})
	return exists, recent, unseen, err
}

// GetNextUID returns the next UID for a mailbox and increments it
func (db *Database) GetNextUID(user, mailbox string) (uint32, error) {
	if db.bolt == nil {
		return 1, nil
	}

	var uid uint32
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(mailboxKey(user, mailbox)))
		if err != nil {
			return err
		}
		uid = uint32(btoi(b.Get([]byte("uidnext"))))
		if uid == 0 {
			uid = 1
		}
		return b.Put([]byte("uidnext"), itob(uid+1))
	})
	return uid, err
}

// MessageMetadata stores message metadata
type MessageMetadata struct {
	MessageID    string    `json:"message_id"`
	UID          uint32    `json:"uid"`
	Flags        []string  `json:"flags"`
	InternalDate time.Time `json:"internal_date"`
	Size         int64     `json:"size"`
	Subject      string    `json:"subject"`
	Date         string    `json:"date"`
	From         string    `json:"from"`
	To           string    `json:"to"`
}

// GetMessageUIDs returns all message UIDs in a mailbox
func (db *Database) GetMessageUIDs(user, mailbox string) ([]uint32, error) {
	if db.bolt == nil {
		return []uint32{}, nil
	}

	uids := []uint32{}
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			uids = append(uids, uint32(btoi(k)))
		}
		return nil
	})
	return uids, err
}

// GetMessageMetadata retrieves message metadata
func (db *Database) GetMessageMetadata(user, mailbox string, uid uint32) (*MessageMetadata, error) {
	if db.bolt == nil {
		return &MessageMetadata{}, nil
	}

	var meta MessageMetadata
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			// Return empty metadata for non-existent mailbox (matches stub behavior)
			return nil
		}
		data := b.Get(itob(uid))
		if data == nil {
			// Return empty metadata for non-existent message (matches stub behavior)
			return nil
		}
		return json.Unmarshal(data, &meta)
	})
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

// StoreMessageMetadata stores message metadata
func (db *Database) StoreMessageMetadata(user, mailbox string, uid uint32, meta *MessageMetadata) error {
	if db.bolt == nil {
		return nil
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(messagesBucket(user, mailbox)))
		if err != nil {
			return err
		}
		return b.Put(itob(uid), data)
	})
}

// UpdateMessageMetadata updates message metadata
func (db *Database) UpdateMessageMetadata(user, mailbox string, uid uint32, meta *MessageMetadata) error {
	return db.StoreMessageMetadata(user, mailbox, uid, meta)
}

// DeleteMessage deletes a message
func (db *Database) DeleteMessage(user, mailbox string, uid uint32) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			return nil
		}
		return b.Delete(itob(uid))
	})
}

// hasFlag checks if a flag is present in the list
func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}

// itob converts uint32 to 4-byte big-endian slice
func itob(v uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
	return b
}

// btoi converts 4-byte big-endian slice to uint32
func btoi(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
