// Package storage provides data storage for the mail server
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// Database represents the bbolt database interface
type Database struct {
	path string
	bolt *bbolt.DB
}

// OpenDatabase opens the bbolt database
func OpenDatabase(path string) (*Database, error) {
	// Create parent directories if they don't exist
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := bbolt.Open(path, 0600, &bbolt.Options{Timeout: 1})
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", path, err)
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

// Bolt returns the underlying bbolt.DB for use by other packages
func (db *Database) Bolt() *bbolt.DB {
	return db.bolt
}

// AuthenticateUser validates user credentials against the account database.
// NOTE: This is a fallback — the primary auth path is via SetAuthFunc injected
// from the server orchestrator. This method should NOT be relied upon for
// production authentication without a proper credential store.
func (db *Database) AuthenticateUser(username, password string) (bool, error) {
	// No account database available in storage.Database — defer to injected authFunc
	return false, fmt.Errorf("AuthenticateUser: not implemented, use SetAuthFunc")
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
			UIDValidity: btoi(b.Get([]byte("uidvalidity"))),
			UIDNext:     btoi(b.Get([]byte("uidnext"))),
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
			if now < 0 || now > 0x7FFFFFFF {
				return fmt.Errorf("timestamp out of range for uidvalidity")
			}
			if err := b.Put([]byte("uidvalidity"), itob(uint32(now))); err != nil {
				return err
			}
			if err := b.Put([]byte("uidnext"), itob(1)); err != nil {
				return err
			}
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
		if err := tx.DeleteBucket([]byte(mailboxKey(user, mailbox))); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
		if err := tx.DeleteBucket([]byte(messagesBucket(user, mailbox))); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
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
			now := time.Now().Unix()
			if now < 0 || now > 0x7FFFFFFF {
				return fmt.Errorf("timestamp out of range for uidvalidity")
			}
			if err := newB.Put([]byte("uidvalidity"), itob(uint32(now))); err != nil {
				return err
			}
			if err := newB.Put([]byte("uidnext"), itob(1)); err != nil {
				return err
			}
			_, err = tx.CreateBucketIfNotExists([]byte(newMsgs))
			return err
		}

		// Create new mailbox bucket and copy data
		newB, err := tx.CreateBucketIfNotExists([]byte(newKey))
		if err != nil {
			return err
		}
		if err := oldB.ForEach(func(k, v []byte) error {
			return newB.Put(k, v)
		}); err != nil {
			return err
		}

		// Create new messages bucket and copy messages
		oldMB := tx.Bucket([]byte(oldMsgs))
		if oldMB != nil {
			newMB, err := tx.CreateBucketIfNotExists([]byte(newMsgs))
			if err != nil {
				return err
			}
			if err := oldMB.ForEach(func(k, v []byte) error {
				return newMB.Put(k, v)
			}); err != nil {
				return err
			}
		}

		// Delete old buckets
		_ = tx.DeleteBucket([]byte(oldKey))  // bucket may not exist in partial state
		_ = tx.DeleteBucket([]byte(oldMsgs)) // bucket may not exist in partial state
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
			if HasFlag(meta.Flags, "\\Recent") {
				recent++
			}
			if !HasFlag(meta.Flags, "\\Seen") {
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
		uid = btoi(b.Get([]byte("uidnext")))
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
	InReplyTo    string    `json:"in_reply_to,omitempty"`
	References   []string  `json:"references,omitempty"`
	ThreadID     string    `json:"thread_id,omitempty"`
	IsThreadRoot bool      `json:"is_thread_root,omitempty"`
}

// Thread represents an email conversation thread
type Thread struct {
	ThreadID     string    `json:"thread_id"`
	Subject      string    `json:"subject"`
	Participants []string  `json:"participants"`
	MessageCount int       `json:"message_count"`
	UnreadCount  int       `json:"unread_count"`
	LastActivity time.Time `json:"last_activity"`
	CreatedAt    time.Time `json:"created_at"`
}

// ThreadMessage represents a message within a thread
type ThreadMessage struct {
	MessageID string    `json:"message_id"`
	UID       uint32    `json:"uid"`
	Mailbox   string    `json:"mailbox"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Date      time.Time `json:"date"`
	Flags     []string  `json:"flags"`
	InReplyTo string    `json:"in_reply_to,omitempty"`
	IsRead    bool      `json:"is_read"`
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
			uids = append(uids, btoi(k))
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

// HasFlag checks if a flag is present in the list (exported)
func HasFlag(flags []string, flag string) bool {
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
	// #nosec G115 -- Intentional big-endian byte extraction from uint32
	b[0] = byte(v >> 24)
	// #nosec G115 -- Intentional big-endian byte extraction from uint32
	b[1] = byte(v >> 16)
	// #nosec G115 -- Intentional big-endian byte extraction from uint32
	b[2] = byte(v >> 8)
	// #nosec G115 -- Intentional big-endian byte extraction from uint32
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

// threadBucket returns the bucket name for thread storage
func threadBucket(user string) string {
	return fmt.Sprintf("threads:%s", user)
}

// GetOrCreateThreadID finds an existing thread for a message or creates a new one
func (db *Database) GetOrCreateThreadID(user, mailbox string, subject, inReplyTo string, references []string) (string, error) {
	if db.bolt == nil {
		return generateThreadID(subject), nil
	}

	// Normalize subject (remove Re: Fwd: prefixes)
	normalizedSubject := NormalizeSubject(subject)

	// Try to find existing thread by message ID references
	if inReplyTo != "" || len(references) > 0 {
		threadID, err := db.findThreadByReferences(user, mailbox, inReplyTo, references)
		if err == nil && threadID != "" {
			return threadID, nil
		}
	}

	// Try to find by normalized subject (for messages without proper threading headers)
	threadID, err := db.findThreadBySubject(user, mailbox, normalizedSubject)
	if err == nil && threadID != "" {
		return threadID, nil
	}

	// Create new thread
	return generateThreadID(subject), nil
}

// findThreadByReferences finds a thread by In-Reply-To or References headers
func (db *Database) findThreadByReferences(user, mailbox, inReplyTo string, references []string) (string, error) {
	if db.bolt == nil {
		return "", nil
	}

	threadID := ""
	err := db.bolt.View(func(tx *bbolt.Tx) error {
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

			// Check if this message matches our references
			if inReplyTo != "" && meta.MessageID == inReplyTo {
				threadID = meta.ThreadID
				return nil
			}

			for _, ref := range references {
				if meta.MessageID == ref && meta.ThreadID != "" {
					threadID = meta.ThreadID
					return nil
				}
			}
		}
		return nil
	})

	return threadID, err
}

// findThreadBySubject finds a thread by normalized subject
func (db *Database) findThreadBySubject(user, mailbox, normalizedSubject string) (string, error) {
	if db.bolt == nil || normalizedSubject == "" {
		return "", nil
	}

	threadID := ""
	oldestDate := time.Now()

	err := db.bolt.View(func(tx *bbolt.Tx) error {
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

			// Check if subjects match
			if NormalizeSubject(meta.Subject) == normalizedSubject {
				// Use the oldest message's thread ID (thread root)
				if meta.InternalDate.Before(oldestDate) {
					oldestDate = meta.InternalDate
					threadID = meta.ThreadID
				}
			}
		}
		return nil
	})

	// Only use subject-based threading if the thread is recent (within 30 days)
	if threadID != "" && time.Since(oldestDate) > 30*24*time.Hour {
		return "", nil
	}

	return threadID, err
}

// NormalizeSubject removes Re: and Fwd: prefixes for thread matching (exported)
func NormalizeSubject(subject string) string {
	subject = strings.TrimSpace(subject)

	// Remove Re: prefixes (case insensitive)
	for {
		upper := strings.ToUpper(subject)
		if strings.HasPrefix(upper, "RE:") {
			subject = strings.TrimSpace(subject[3:])
		} else if strings.HasPrefix(upper, "RE[") {
			// Handle Re[n]: format
			if idx := strings.Index(subject, "]:"); idx != -1 {
				subject = strings.TrimSpace(subject[idx+2:])
			} else {
				break
			}
		} else {
			break
		}
	}

	// Remove Fwd: prefixes
	for {
		upper := strings.ToUpper(subject)
		if strings.HasPrefix(upper, "FWD:") {
			subject = strings.TrimSpace(subject[4:])
		} else if strings.HasPrefix(upper, "FW:") {
			subject = strings.TrimSpace(subject[3:])
		} else {
			break
		}
	}

	return strings.TrimSpace(subject)
}

// generateThreadID creates a unique thread ID based on subject and timestamp
func generateThreadID(subject string) string {
	data := fmt.Sprintf("%s:%d", subject, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16])
}

// GetThread retrieves a thread by ID
func (db *Database) GetThread(user, threadID string) (*Thread, error) {
	if db.bolt == nil {
		return nil, fmt.Errorf("database not available")
	}

	var thread Thread
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(threadBucket(user)))
		if b == nil {
			return fmt.Errorf("thread not found")
		}

		data := b.Get([]byte(threadID))
		if data == nil {
			return fmt.Errorf("thread not found")
		}

		return json.Unmarshal(data, &thread)
	})

	if err != nil {
		return nil, err
	}

	return &thread, nil
}

// GetThreads retrieves all threads for a user
func (db *Database) GetThreads(user string, limit, offset int) ([]*Thread, error) {
	if db.bolt == nil {
		return []*Thread{}, nil
	}

	threads := []*Thread{}
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(threadBucket(user)))
		if b == nil {
			return nil
		}

		c := b.Cursor()
		count := 0
		skipped := 0

		for k, v := c.First(); k != nil; k, v = c.Next() {
			if skipped < offset {
				skipped++
				continue
			}

			if limit > 0 && count >= limit {
				break
			}

			var thread Thread
			if err := json.Unmarshal(v, &thread); err != nil {
				continue
			}

			threads = append(threads, &thread)
			count++
		}

		return nil
	})

	return threads, err
}

// GetThreadMessages retrieves all messages in a thread
func (db *Database) GetThreadMessages(user, mailbox, threadID string) ([]*ThreadMessage, error) {
	if db.bolt == nil {
		return []*ThreadMessage{}, nil
	}

	messages := []*ThreadMessage{}
	err := db.bolt.View(func(tx *bbolt.Tx) error {
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

			if meta.ThreadID == threadID {
				tm := &ThreadMessage{
					MessageID: meta.MessageID,
					UID:       meta.UID,
					Mailbox:   mailbox,
					From:      meta.From,
					To:        meta.To,
					Subject:   meta.Subject,
					Date:      meta.InternalDate,
					Flags:     meta.Flags,
					InReplyTo: meta.InReplyTo,
					IsRead:    HasFlag(meta.Flags, "\\Seen"),
				}
				messages = append(messages, tm)
			}
		}

		return nil
	})

	return messages, err
}

// UpdateThread updates or creates a thread entry
func (db *Database) UpdateThread(user string, thread *Thread) error {
	if db.bolt == nil {
		return nil
	}

	data, err := json.Marshal(thread)
	if err != nil {
		return err
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(threadBucket(user)))
		if err != nil {
			return err
		}
		return b.Put([]byte(thread.ThreadID), data)
	})
}

// DeleteThread deletes a thread entry
func (db *Database) DeleteThread(user, threadID string) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(threadBucket(user)))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(threadID))
	})
}

// SearchThreads searches for threads by subject or participant
func (db *Database) SearchThreads(user, query string) ([]*Thread, error) {
	if db.bolt == nil {
		return []*Thread{}, nil
	}

	threads := []*Thread{}
	queryLower := strings.ToLower(query)

	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(threadBucket(user)))
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var thread Thread
			if err := json.Unmarshal(v, &thread); err != nil {
				continue
			}

			// Search in subject
			if strings.Contains(strings.ToLower(thread.Subject), queryLower) {
				threads = append(threads, &thread)
				continue
			}

			// Search in participants
			for _, participant := range thread.Participants {
				if strings.Contains(strings.ToLower(participant), queryLower) {
					threads = append(threads, &thread)
					break
				}
			}
		}

		return nil
	})

	return threads, err
}
