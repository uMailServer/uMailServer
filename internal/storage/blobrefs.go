package storage

import (
	"bytes"
	"encoding/json"

	"go.etcd.io/bbolt"
)

// pendingUploadsBucketName mirrors the JMAP upload tracker bucket
// (internal/jmap/uploads.go). Keys are user + NUL + blobID. A pending upload
// holds a reference to its blob just like a message row does.
const pendingUploadsBucketName = "jmap_pending_uploads"

// BlobReferenced reports, for each id in ids, whether the user still has a
// reference to that content-addressed blob: a message metadata row in any
// mailbox (even one whose mailbox record is gone) or a pending JMAP upload.
//
// The reference count is derived from the authoritative metadata rows rather
// than from a separately persisted counter. The blob files and the metadata
// live in different stores that cannot be updated atomically, and mailbox
// DELETE/RENAME rewrite whole buckets, so a persisted counter can drift and a
// drifted counter either leaks blobs or deletes live ones (F6362). Deriving
// the answer needs no migration: pre-existing data is counted correctly the
// first time it is asked about. The scan is one read transaction and stops as
// soon as every candidate is found.
func (db *Database) BlobReferenced(user string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	if db == nil || db.bolt == nil || len(ids) == 0 {
		return out, nil
	}
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	prefix := []byte("msgs:" + keyUser(user) + ":")
	err := db.bolt.View(func(tx *bbolt.Tx) error {
		done := func() bool { return len(out) == len(want) }
		if pb := tx.Bucket([]byte(pendingUploadsBucketName)); pb != nil {
			for id := range want {
				if pb.Get([]byte(user+"\x00"+id)) != nil {
					out[id] = true
				}
			}
		}
		if done() {
			return nil
		}
		c := tx.Cursor()
		for k, _ := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, _ = c.Next() {
			b := tx.Bucket(k)
			if b == nil {
				continue
			}
			err := b.ForEach(func(_, v []byte) error {
				var m struct {
					MessageID string `json:"message_id"`
				}
				if json.Unmarshal(v, &m) != nil {
					return nil
				}
				if _, ok := want[m.MessageID]; ok {
					out[m.MessageID] = true
				}
				return nil
			})
			if err != nil {
				return err
			}
			if done() {
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
