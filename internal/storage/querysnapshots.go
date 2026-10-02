package storage

// Query snapshots back the JMAP /queryChanges delta computation (RFC 8620
// §5.6): Email/query stores the full ordered result of a filter/sort pair,
// and Email/queryChanges diffs that snapshot against a fresh run. Only the
// latest snapshot per query hash is kept; without a matching snapshot the
// caller answers stateMismatch.

import (
	"encoding/json"
	"fmt"

	"go.etcd.io/bbolt"
)

// QuerySnapshot captures the full ordered result of a JMAP /query at a
// specific state sequence.
type QuerySnapshot struct {
	QueryHash string   `json:"query_hash"`
	Seq       uint64   `json:"seq"`
	IDs       []string `json:"ids"`
}

func querySnapshotsBucket(user string) string {
	return "query_snapshots_" + user
}

// SaveQuerySnapshot stores (replacing any previous) the snapshot for a query
// hash.
func (db *Database) SaveQuerySnapshot(user string, snap *QuerySnapshot) error {
	if db.bolt == nil {
		return nil
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(querySnapshotsBucket(user)))
		if err != nil {
			return err
		}
		return b.Put([]byte(snap.QueryHash), data)
	})
}

// GetQuerySnapshot returns the stored snapshot for a query hash, or an error
// when none exists (the caller answers stateMismatch per RFC 8620 §5.6).
func (db *Database) GetQuerySnapshot(user, queryHash string) (*QuerySnapshot, error) {
	if db.bolt == nil {
		return nil, fmt.Errorf("database not available")
	}
	var snap QuerySnapshot
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(querySnapshotsBucket(user)))
		if b == nil {
			return fmt.Errorf("query snapshot not found")
		}
		data := b.Get([]byte(queryHash))
		if data == nil {
			return fmt.Errorf("query snapshot not found")
		}
		return json.Unmarshal(data, &snap)
	})
	if err != nil {
		return nil, err
	}
	return &snap, nil
}
