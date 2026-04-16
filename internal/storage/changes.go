package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"go.etcd.io/bbolt"
)

// ChangeType identifies the JMAP type whose state advanced.
type ChangeType string

const (
	ChangeTypeMailbox ChangeType = "mailbox"
	ChangeTypeEmail   ChangeType = "email"
	ChangeTypeThread  ChangeType = "thread"
)

// ChangeKind identifies the operation.
type ChangeKind string

const (
	ChangeKindCreated   ChangeKind = "created"
	ChangeKindUpdated   ChangeKind = "updated"
	ChangeKindDestroyed ChangeKind = "destroyed"
)

// ChangeEntry is a single mutation recorded for a user.
type ChangeEntry struct {
	Seq     uint64     `json:"seq"`
	Type    ChangeType `json:"type"`
	Kind    ChangeKind `json:"kind"`
	ID      string     `json:"id"`
	Mailbox string     `json:"mailbox,omitempty"`
	At      time.Time  `json:"at"`
}

// ChangesRetention bounds how long entries are kept.
const ChangesRetention = 30 * 24 * time.Hour

// ChangesMaxEntriesPerUser caps the journal size per user.
const ChangesMaxEntriesPerUser = 50000

func changesBucket(user string) string {
	return fmt.Sprintf("changes:%s", user)
}

func seqKey(s uint64) []byte {
	k := make([]byte, 8)
	binary.BigEndian.PutUint64(k, s)
	return k
}

// RecordChange appends a change entry for the given user. Mutation paths
// should call this best-effort: a journal write failure must not roll back
// the underlying mutation, so callers typically log and continue.
func (db *Database) RecordChange(user string, ct ChangeType, ck ChangeKind, id, mailbox string) error {
	if db.bolt == nil || user == "" {
		return nil
	}
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(changesBucket(user)))
		if err != nil {
			return err
		}
		seq, err := b.NextSequence()
		if err != nil {
			return err
		}
		entry := ChangeEntry{
			Seq:     seq,
			Type:    ct,
			Kind:    ck,
			ID:      id,
			Mailbox: mailbox,
			At:      time.Now().UTC(),
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put(seqKey(seq), data)
	})
}

// CurrentChangeState returns the latest sequence as a JMAP state token.
// Callers treat the value as opaque.
func (db *Database) CurrentChangeState(user string) (string, error) {
	if db.bolt == nil {
		return "0", nil
	}
	var seq uint64
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(changesBucket(user)))
		if b != nil {
			seq = b.Sequence()
		}
		return nil
	})
	if err != nil {
		return "0", err
	}
	return strconv.FormatUint(seq, 10), nil
}

// ParseChangeState converts a JMAP state token back to a sequence.
// Unknown / malformed tokens parse as 0 so the caller returns the full window.
func ParseChangeState(state string) uint64 {
	if state == "" {
		return 0
	}
	v, err := strconv.ParseUint(state, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// GetChangesSince returns up to max entries with seq > sinceSeq filtered by type.
// hasMore is true if more entries of the requested type exist beyond what was
// returned. lastSeq is the seq of the last returned entry (or sinceSeq if
// nothing matched), suitable for the caller's newState.
func (db *Database) GetChangesSince(user string, ct ChangeType, sinceSeq uint64, max int) (entries []ChangeEntry, hasMore bool, lastSeq uint64, err error) {
	if db.bolt == nil {
		return nil, false, sinceSeq, nil
	}
	if max <= 0 {
		max = 256
	}
	lastSeq = sinceSeq
	err = db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(changesBucket(user)))
		if b == nil {
			return nil
		}
		c := b.Cursor()
		start := seqKey(sinceSeq + 1)
		for k, v := c.Seek(start); k != nil; k, v = c.Next() {
			var e ChangeEntry
			if err := json.Unmarshal(v, &e); err != nil {
				continue
			}
			if e.Type != ct {
				continue
			}
			if len(entries) >= max {
				// Determine if more matching entries exist past the cap.
				for nk, nv := c.Next(); nk != nil; nk, nv = c.Next() {
					var ne ChangeEntry
					if err := json.Unmarshal(nv, &ne); err != nil {
						continue
					}
					if ne.Type == ct {
						hasMore = true
						break
					}
				}
				return nil
			}
			entries = append(entries, e)
			lastSeq = e.Seq
		}
		return nil
	})
	return entries, hasMore, lastSeq, err
}

// PruneChanges removes entries older than ChangesRetention, then trims the
// oldest entries until the journal is at or below ChangesMaxEntriesPerUser.
// Sequence numbers are preserved (state tokens stay stable for live clients).
func (db *Database) PruneChanges(user string) error {
	if db.bolt == nil || user == "" {
		return nil
	}
	cutoff := time.Now().Add(-ChangesRetention)
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(changesBucket(user)))
		if b == nil {
			return nil
		}
		// Pass 1: delete entries older than cutoff.
		c := b.Cursor()
		var toDelete [][]byte
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var e ChangeEntry
			if err := json.Unmarshal(v, &e); err != nil {
				toDelete = append(toDelete, append([]byte(nil), k...))
				continue
			}
			if e.At.Before(cutoff) {
				toDelete = append(toDelete, append([]byte(nil), k...))
				continue
			}
			break
		}
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}

		// Pass 2: cap total entries.
		total := b.Stats().KeyN
		if total <= ChangesMaxEntriesPerUser {
			return nil
		}
		excess := total - ChangesMaxEntriesPerUser
		toDelete = toDelete[:0]
		c = b.Cursor()
		for k, _ := c.First(); k != nil && len(toDelete) < excess; k, _ = c.Next() {
			toDelete = append(toDelete, append([]byte(nil), k...))
		}
		for _, k := range toDelete {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
