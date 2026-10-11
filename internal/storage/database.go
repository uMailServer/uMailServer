// Package storage provides data storage for the mail server
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// Database represents the bbolt database interface
type Database struct {
	path string
	bolt *bbolt.DB
}

// openLockTimeout bounds how long Open waits for another holder of the file
// lock (F5950: the bare literal 1 meant 1ns, i.e. no wait at all).
const openLockTimeout = 5 * time.Second

// OpenDatabase opens the bbolt database
func OpenDatabase(path string) (*Database, error) {
	// Create parent directories if they don't exist
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: openLockTimeout})
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
	Name          string
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64 // RFC 7162: highest modification sequence number
	Subscribed    bool
}

// mailboxKey returns the bucket name for a user's mailbox metadata
func mailboxKey(user, mailbox string) string {
	return fmt.Sprintf("mailbox:%s:%s", keyUser(user), mailbox)
}

// messagesBucket returns the bucket name for a user's mailbox messages
func messagesBucket(user, mailbox string) string {
	return fmt.Sprintf("msgs:%s:%s", keyUser(user), mailbox)
}

// keyUser makes a user name safe to embed in a ':'-separated bucket or key
// name. Without it user "a" / mailbox "b:c" and user "a:b" / mailbox "c" share
// the bucket "msgs:a:b:c", so one user could read, overwrite or be billed for
// another's data (F6663). The mapping is injective, leaves every ordinary
// address unchanged (no migration needed), and escapes with NUL, which never
// appears in a ':' position.
func keyUser(user string) string {
	if !strings.ContainsAny(user, ":\x00") {
		return user
	}
	return userKeyEscaper.Replace(user)
}

var (
	userKeyEscaper   = strings.NewReplacer("\x00", "\x00z", ":", "\x00c")
	userKeyUnescaper = strings.NewReplacer("\x00c", ":", "\x00z", "\x00")
)

// isINBOX reports whether name is INBOX in any case (RFC 3501: INBOX is
// case-insensitive and always exists).
func isINBOX(name string) bool { return strings.EqualFold(name, "INBOX") }

// MaxStoredReferences bounds the References list kept per message so a
// hostile header cannot bloat a metadata row without limit. The newest
// (last) references are the closest ancestors and are kept.
const MaxStoredReferences = 100

// normalizeFlags drops empty flags and case-insensitive duplicates, keeping
// the first spelling of each. It returns a new slice.
func normalizeFlags(flags []string) []string {
	if flags == nil {
		return nil
	}
	out := make([]string, 0, len(flags))
	seen := make(map[string]struct{}, len(flags))
	for _, f := range flags {
		if f == "" {
			continue
		}
		k := strings.ToLower(f)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, f)
	}
	return out
}

// GetMailbox retrieves mailbox information
func (db *Database) GetMailbox(user, mailbox string) (*Mailbox, error) {
	if db.bolt == nil {
		return &Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 1, HighestModSeq: 0}, nil
	}

	var mb Mailbox
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(mailboxKey(user, mailbox)))
		if b == nil {
			mb = Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 1, HighestModSeq: 0}
			return nil
		}
		mb = Mailbox{
			Name:          mailbox,
			UIDValidity:   btoi(b.Get([]byte("uidvalidity"))),
			UIDNext:       btoi(b.Get([]byte("uidnext"))),
			HighestModSeq: btoi64(b.Get([]byte("highestmodseq"))),
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

	created := false
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(mailboxKey(user, mailbox)))
		if err != nil {
			return err
		}
		uvMissing := b.Get([]byte("uidvalidity")) == nil
		if uvMissing {
			// F5723: never reuse a UIDVALIDITY a deleted/renamed mailbox
			// of this name had.
			uv, err := nextUIDValidityTx(tx, mailboxKey(user, mailbox))
			if err != nil {
				return err
			}
			if err := b.Put([]byte("uidvalidity"), itob(uv)); err != nil {
				return err
			}
			created = true
		}
		// F5722: GetNextUID/GetNextModSeq may have auto-created this bucket
		// already; only initialise counters that are absent so UIDs and
		// MODSEQs already issued are not re-issued.
		if b.Get([]byte("uidnext")) == nil {
			if err := b.Put([]byte("uidnext"), itob(1)); err != nil {
				return err
			}
		}
		if b.Get([]byte("highestmodseq")) == nil {
			// Initialize highest modification sequence (RFC 7162)
			if err := b.Put([]byte("highestmodseq"), itob64(0)); err != nil {
				return err
			}
		}
		// Also create the messages bucket
		_, err = tx.CreateBucketIfNotExists([]byte(messagesBucket(user, mailbox)))
		return err
	})
	if err == nil && created {
		_ = db.RecordChange(user, ChangeTypeMailbox, ChangeKindCreated, mailbox, "")
	}
	return err
}

// uidValidityFloorBucket remembers, per mailbox key, the UIDVALIDITY of a
// mailbox that was deleted or renamed away, so a re-created mailbox of the
// same name gets a strictly larger value (RFC 3501 2.3.1.1). F5723.
const uidValidityFloorBucket = "uidvalidity_floor"

func recordUIDValidityFloorTx(tx *bbolt.Tx, key string) error {
	b := tx.Bucket([]byte(key))
	if b == nil {
		return nil
	}
	uv := b.Get([]byte("uidvalidity"))
	if uv == nil {
		return nil
	}
	fb, err := tx.CreateBucketIfNotExists([]byte(uidValidityFloorBucket))
	if err != nil {
		return err
	}
	if cur := fb.Get([]byte(key)); cur != nil && btoi(cur) >= btoi(uv) {
		return nil
	}
	return fb.Put([]byte(key), append([]byte(nil), uv...))
}

func nextUIDValidityTx(tx *bbolt.Tx, key string) (uint32, error) {
	now := time.Now().Unix()
	if now < 0 || now > 0x7FFFFFFF {
		return 0, fmt.Errorf("timestamp out of range for uidvalidity")
	}
	uv := uint32(now)
	if fb := tx.Bucket([]byte(uidValidityFloorBucket)); fb != nil {
		if floor := btoi(fb.Get([]byte(key))); floor >= uv {
			uv = floor + 1
		}
	}
	return uv, nil
}

// DeleteMailbox deletes a mailbox
func (db *Database) DeleteMailbox(user, mailbox string) error {
	if db.bolt == nil {
		return nil
	}
	if isINBOX(mailbox) {
		return errors.New("cannot delete INBOX")
	}
	existed := false
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(mailboxKey(user, mailbox))) != nil {
			existed = true
		}
		if err := recordUIDValidityFloorTx(tx, mailboxKey(user, mailbox)); err != nil {
			return err
		}
		if err := tx.DeleteBucket([]byte(mailboxKey(user, mailbox))); err != nil && err != bolterrors.ErrBucketNotFound {
			return err
		}
		if err := tx.DeleteBucket([]byte(messagesBucket(user, mailbox))); err != nil && err != bolterrors.ErrBucketNotFound {
			return err
		}
		return nil
	})
	if err == nil && existed {
		_ = db.RecordChange(user, ChangeTypeMailbox, ChangeKindDestroyed, mailbox, "")
		// Clean up ACL entries for this mailbox
		_ = db.DeleteACL(user, mailbox, "")
	}
	return err
}

// RenameMailbox renames a mailbox
func (db *Database) RenameMailbox(user, oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	if db.bolt == nil {
		return nil
	}

	// Read old data
	oldKey := mailboxKey(user, oldName)
	oldMsgs := messagesBucket(user, oldName)
	newKey := mailboxKey(user, newName)
	newMsgs := messagesBucket(user, newName)

	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(newKey)) != nil || tx.Bucket([]byte(newMsgs)) != nil {
			return fmt.Errorf("destination mailbox already exists: %s", newName)
		}
		oldB := tx.Bucket([]byte(oldKey))
		if oldB == nil {
			// If source mailbox doesn't exist, just create the new one
			newB, err := tx.CreateBucketIfNotExists([]byte(newKey))
			if err != nil {
				return err
			}
			uv, err := nextUIDValidityTx(tx, newKey)
			if err != nil {
				return err
			}
			if err := newB.Put([]byte("uidvalidity"), itob(uv)); err != nil {
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

		// F6665: the retired UIDVALIDITY of a previous mailbox with the
		// destination's name is a floor; never go backwards across reuse.
		if fb := tx.Bucket([]byte(uidValidityFloorBucket)); fb != nil {
			if floor := btoi(fb.Get([]byte(newKey))); floor >= btoi(newB.Get([]byte("uidvalidity"))) {
				if err := newB.Put([]byte("uidvalidity"), itob(floor+1)); err != nil {
					return err
				}
			}
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
		if err := recordUIDValidityFloorTx(tx, oldKey); err != nil {
			return err
		}
		_ = tx.DeleteBucket([]byte(oldKey))  // bucket may not exist in partial state
		_ = tx.DeleteBucket([]byte(oldMsgs)) // bucket may not exist in partial state

		// Migrate ACL entries to the new mailbox name
		aclB := tx.Bucket([]byte("acl"))
		if aclB != nil {
			prefix := aclOwnerMailboxPrefix(user, oldName)
			// F5095: collect first, mutate after. Put/Delete on the bucket
			// invalidate a live cursor (page splits/rebalances), which made
			// the scan skip grants: they stayed on the old name and were
			// missing on the new one.
			type aclKV struct{ k, v []byte }
			var moves []aclKV
			c := aclB.Cursor()
			for k, v := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, v = c.Next() {
				if !aclKeyBelongsTo(user, oldName, k, v) {
					continue // entry of a mailbox merely prefixed by oldName (F6121)
				}
				moves = append(moves, aclKV{append([]byte(nil), k...), append([]byte(nil), v...)})
			}
			for _, m := range moves {
				grantee := string(m.k[len(prefix):])
				if err := aclB.Put([]byte(aclKey(user, newName, grantee)), m.v); err != nil {
					return err
				}
				if err := aclB.Delete(m.k); err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err == nil {
		_ = db.RecordChange(user, ChangeTypeMailbox, ChangeKindDestroyed, oldName, "")
		_ = db.RecordChange(user, ChangeTypeMailbox, ChangeKindCreated, newName, "")
	}
	return err
}

// ListMailboxes lists all mailboxes for a user
func (db *Database) ListMailboxes(user string) ([]string, error) {
	if db.bolt == nil {
		return []string{"INBOX"}, nil
	}

	var result []string
	prefix := fmt.Sprintf("mailbox:%s:", keyUser(user))
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

// subscribedBucket returns the bucket name for user's subscribed mailboxes
func subscribedBucket(user string) string {
	return fmt.Sprintf("subscribed:%s", keyUser(user))
}

// SetSubscribed sets the subscription status of a mailbox
func (db *Database) SetSubscribed(user, mailbox string, subscribed bool) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(subscribedBucket(user)))
		if err != nil {
			return err
		}

		if subscribed {
			return bucket.Put([]byte(mailbox), []byte("1"))
		}
		return bucket.Delete([]byte(mailbox))
	})
}

// GetSubscribed returns the subscription status of a mailbox
func (db *Database) GetSubscribed(user, mailbox string) (bool, error) {
	if db.bolt == nil {
		return false, nil
	}

	var subscribed bool
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(subscribedBucket(user)))
		if bucket == nil {
			return nil
		}
		subscribed = bucket.Get([]byte(mailbox)) != nil
		return nil
	})
	return subscribed, err
}

// ListSubscribed lists all subscribed mailboxes for a user
func (db *Database) ListSubscribed(user string) ([]string, error) {
	if db.bolt == nil {
		// In nil mode, no subscriptions exist
		return nil, nil
	}

	var result []string
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(subscribedBucket(user)))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, _ []byte) error {
			result = append(result, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
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

// ErrUIDSpaceExhausted is returned by GetNextUID when the mailbox has issued
// every UID; the caller must change UIDVALIDITY (recreate the mailbox).
var ErrUIDSpaceExhausted = errors.New("mailbox UID space exhausted")

// ensureUIDValidityTx gives a mailbox bucket that was auto-created by a
// counter call a valid (non-zero, never reused) UIDVALIDITY.
func ensureUIDValidityTx(tx *bbolt.Tx, b *bbolt.Bucket, key string) error {
	if b.Get([]byte("uidvalidity")) != nil {
		return nil
	}
	uv, err := nextUIDValidityTx(tx, key)
	if err != nil {
		return err
	}
	return b.Put([]byte("uidvalidity"), itob(uv))
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
		if err := ensureUIDValidityTx(tx, b, mailboxKey(user, mailbox)); err != nil {
			return err
		}
		uid = btoi(b.Get([]byte("uidnext")))
		if uid == 0 {
			uid = 1
		}
		if uid == math.MaxUint32 {
			// uid+1 would wrap to 0 and the next call would re-issue UID 1.
			uid = 0
			return ErrUIDSpaceExhausted
		}
		return b.Put([]byte("uidnext"), itob(uid+1))
	})
	return uid, err
}

// GetNextModSeq returns the next modification sequence number for a mailbox and increments it (RFC 7162)
func (db *Database) GetNextModSeq(user, mailbox string) (uint64, error) {
	if db.bolt == nil {
		return 1, nil
	}

	var modSeq uint64
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(mailboxKey(user, mailbox)))
		if err != nil {
			return err
		}
		if err := ensureUIDValidityTx(tx, b, mailboxKey(user, mailbox)); err != nil {
			return err
		}
		modSeq = btoi64(b.Get([]byte("highestmodseq")))
		modSeq++
		return b.Put([]byte("highestmodseq"), itob64(modSeq))
	})
	return modSeq, err
}

// GetHighestModSeq returns the highest modification sequence number for a mailbox
func (db *Database) GetHighestModSeq(user, mailbox string) (uint64, error) {
	if db.bolt == nil {
		return 0, nil
	}

	var modSeq uint64
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(mailboxKey(user, mailbox)))
		if b == nil {
			return nil
		}
		modSeq = btoi64(b.Get([]byte("highestmodseq")))
		return nil
	})
	return modSeq, err
}

// MessageMetadata stores message metadata
type MessageMetadata struct {
	MessageID    string    `json:"message_id"`
	UID          uint32    `json:"uid"`
	Flags        []string  `json:"flags"`
	InternalDate time.Time `json:"internal_date"`
	Size         int64     `json:"size"`
	ModSeq       uint64    `json:"mod_seq"` // RFC 7162: highest modification sequence number
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

	if meta == nil {
		return errors.New("nil message metadata")
	}
	// Work on a copy: normalisation must not mutate the caller's struct.
	cp := *meta
	cp.Flags = normalizeFlags(cp.Flags)
	if len(cp.References) > MaxStoredReferences {
		cp.References = append([]string(nil), cp.References[len(cp.References)-MaxStoredReferences:]...)
	}

	preexisting := false
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(messagesBucket(user, mailbox)))
		if err != nil {
			return err
		}
		if old := b.Get(itob(uid)); old != nil {
			preexisting = true
			// F6666: a replace carrying a stale (or zero) ModSeq must not
			// lower the row's ModSeq; give it a fresh one instead so CONDSTORE
			// clients still see the change.
			var prev MessageMetadata
			if json.Unmarshal(old, &prev) == nil && cp.ModSeq <= prev.ModSeq {
				if err := raiseHighestModSeqTx(tx, user, mailbox, prev.ModSeq); err != nil {
					return err
				}
				ms, err := db.getNextModSeqTx(tx, user, mailbox)
				if err != nil {
					return err
				}
				cp.ModSeq = ms
			}
		}
		// HIGHESTMODSEQ must cover every message ModSeq (RFC 7162).
		if err := raiseHighestModSeqTx(tx, user, mailbox, cp.ModSeq); err != nil {
			return err
		}
		data, err := json.Marshal(&cp)
		if err != nil {
			return err
		}
		return b.Put(itob(uid), data)
	})
	if err == nil && meta.MessageID != "" {
		kind := ChangeKindCreated
		if preexisting {
			kind = ChangeKindUpdated
		}
		_ = db.RecordChange(user, ChangeTypeEmail, kind, meta.MessageID, mailbox)
		if meta.ThreadID != "" {
			tk := ChangeKindUpdated
			if !preexisting && meta.IsThreadRoot {
				tk = ChangeKindCreated
			}
			_ = db.RecordChange(user, ChangeTypeThread, tk, meta.ThreadID, "")
		}
	}
	return err
}

// UpdateMessageMetadata updates message metadata
func (db *Database) UpdateMessageMetadata(user, mailbox string, uid uint32, meta *MessageMetadata) error {
	return db.StoreMessageMetadata(user, mailbox, uid, meta)
}

// UpdateMessageMetadataFunc atomically reads message metadata, applies fn to it,
// and writes it back inside a single bbolt transaction. This eliminates the
// read-modify-write race when multiple sessions update flags concurrently.
// Also updates the message's ModSeq if the metadata was modified (RFC 7162).
func (db *Database) UpdateMessageMetadataFunc(user, mailbox string, uid uint32, fn func(*MessageMetadata) error) error {
	if db.bolt == nil {
		return nil
	}

	preexisting := false
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		// F6660: an update racing an expunge must not resurrect the row (or
		// invent a bucket for a mailbox that no longer exists).
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			return nil
		}

		var meta MessageMetadata
		data := b.Get(itob(uid))
		if data == nil {
			return nil
		}
		preexisting = true
		if err := json.Unmarshal(data, &meta); err != nil {
			return err
		}

		if err := fn(&meta); err != nil {
			return err
		}
		meta.Flags = normalizeFlags(meta.Flags)

		// Update ModSeq if this is an existing message (RFC 7162)
		if preexisting {
			modSeq, err := db.getNextModSeqTx(tx, user, mailbox)
			if err != nil {
				return err
			}
			meta.ModSeq = modSeq
		}

		newData, err := json.Marshal(&meta)
		if err != nil {
			return err
		}
		return b.Put(itob(uid), newData)
	})
	if err == nil && preexisting {
		// MessageID is not available here, so change recording is skipped.
		// This is acceptable for flag updates which don't alter message content.
		//lint:ignore SA9003 Change recording is intentionally skipped for flag updates.
		_ = struct{}{}
	}
	return err
}

// raiseHighestModSeqTx lifts the mailbox's HIGHESTMODSEQ to at least v. It
// never creates the mailbox record and never lowers the value.
func raiseHighestModSeqTx(tx *bbolt.Tx, user, mailbox string, v uint64) error {
	if v == 0 {
		return nil
	}
	mb := tx.Bucket([]byte(mailboxKey(user, mailbox)))
	if mb == nil || btoi64(mb.Get([]byte("highestmodseq"))) >= v {
		return nil
	}
	return mb.Put([]byte("highestmodseq"), itob64(v))
}

// getNextModSeqTx returns the next modseq for a mailbox within a transaction
func (db *Database) getNextModSeqTx(tx *bbolt.Tx, user, mailbox string) (uint64, error) {
	b, err := tx.CreateBucketIfNotExists([]byte(mailboxKey(user, mailbox)))
	if err != nil {
		return 0, err
	}
	modSeq := btoi64(b.Get([]byte("highestmodseq")))
	modSeq++
	return modSeq, b.Put([]byte("highestmodseq"), itob64(modSeq))
}

// ClearRecent clears the \Recent flag from all messages in a mailbox.
// Called when a mailbox is SELECTed so messages from previous sessions are
// no longer marked \Recent (RFC 3501).
func (db *Database) ClearRecent(user, mailbox string) error {
	if db.bolt == nil {
		return nil
	}

	return db.bolt.Update(func(tx *bbolt.Tx) error {
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
			if HasFlag(meta.Flags, "\\Recent") {
				meta.Flags = RemoveFlag(meta.Flags, "\\Recent")
				newData, err := json.Marshal(&meta)
				if err != nil {
					continue
				}
				if err := b.Put(k, newData); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// DeleteMessage deletes a message
func (db *Database) DeleteMessage(user, mailbox string, uid uint32) error {
	if db.bolt == nil {
		return nil
	}

	var deletedID, deletedThread string
	err := db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(messagesBucket(user, mailbox)))
		if b == nil {
			return nil
		}
		raw := b.Get(itob(uid))
		if raw != nil {
			var meta MessageMetadata
			if err := json.Unmarshal(raw, &meta); err == nil {
				deletedID = meta.MessageID
				deletedThread = meta.ThreadID
			}
		}
		return b.Delete(itob(uid))
	})
	if err == nil && deletedID != "" {
		_ = db.RecordChange(user, ChangeTypeEmail, ChangeKindDestroyed, deletedID, mailbox)
		if deletedThread != "" {
			_ = db.RecordChange(user, ChangeTypeThread, ChangeKindUpdated, deletedThread, "")
		}
	}
	return err
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

// RemoveFlag removes a flag from the list (exported)
func RemoveFlag(flags []string, flag string) []string {
	var result []string
	for _, f := range flags {
		if !strings.EqualFold(f, flag) {
			result = append(result, f)
		}
	}
	return result
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

// itob64 converts uint64 to 8-byte big-endian slice
func itob64(v uint64) []byte {
	b := make([]byte, 8)
	// #nosec G115 -- Intentional big-endian byte extraction from uint64
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)
	return b
}

// btoi64 converts 8-byte big-endian slice to uint64
func btoi64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

// threadBucket returns the bucket name for thread storage
func threadBucket(user string) string {
	return fmt.Sprintf("threads:%s", keyUser(user))
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

	// Strip leading Re:/Re[n]:/Fwd:/FW: prefixes in a single loop so that any
	// interleaving is removed (e.g. "Fwd: Re: Topic"), not just all Re then all
	// Fwd. Every same-conversation subject must normalize identically for thread
	// matching, otherwise a conversation fragments across thread IDs.
	for {
		upper := strings.ToUpper(subject)
		switch {
		case strings.HasPrefix(upper, "RE:"):
			subject = strings.TrimSpace(subject[3:])
		case strings.HasPrefix(upper, "RE["):
			// Handle Re[n]: format
			// Only a numeric counter "Re[12]:" is a reply prefix; anything
			// else ("Re[ticket 4] ... ]:") is subject text.
			idx := strings.Index(subject, "]:")
			if idx < 3 || !isDigits(subject[3:idx]) {
				return strings.TrimSpace(subject)
			}
			subject = strings.TrimSpace(subject[idx+2:])
		case strings.HasPrefix(upper, "FWD:"):
			subject = strings.TrimSpace(subject[4:])
		case strings.HasPrefix(upper, "FW:"):
			subject = strings.TrimSpace(subject[3:])
		default:
			return strings.TrimSpace(subject)
		}
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
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

	if thread == nil || thread.ThreadID == "" {
		return errors.New("thread requires a non-empty ThreadID")
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
