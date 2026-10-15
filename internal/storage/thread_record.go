package storage

import (
	"encoding/json"
	"errors"
	"time"

	"go.etcd.io/bbolt"
)

// RecordMessageInThread adds one message to the thread's summary row (message
// count, unread count, participants, last activity), creating the row when the
// thread is new. The read-modify-write runs in a single write transaction, so
// concurrent deliveries into one conversation cannot lose each other's update.
// from is the sender shown as a participant ("" adds none).
func (db *Database) RecordMessageInThread(user, threadID, subject, from string, internalDate time.Time, flags []string) error {
	if db.bolt == nil {
		return nil
	}
	if threadID == "" {
		return errors.New("thread requires a non-empty ThreadID")
	}
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(threadBucket(user)))
		if err != nil {
			return err
		}
		thread := &Thread{
			ThreadID:     threadID,
			Subject:      NormalizeSubject(subject),
			Participants: []string{},
			LastActivity: internalDate,
			CreatedAt:    time.Now(),
		}
		if raw := b.Get([]byte(threadID)); raw != nil {
			if err := json.Unmarshal(raw, thread); err != nil {
				return err
			}
		}
		thread.MessageCount++
		thread.LastActivity = internalDate
		if from != "" {
			found := false
			for _, p := range thread.Participants {
				if p == from {
					found = true
					break
				}
			}
			if !found {
				thread.Participants = append(thread.Participants, from)
			}
		}
		if !HasFlag(flags, "\\Seen") {
			thread.UnreadCount++
		}
		data, err := json.Marshal(thread)
		if err != nil {
			return err
		}
		return b.Put([]byte(threadID), data)
	})
}
