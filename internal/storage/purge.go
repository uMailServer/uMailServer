package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bbolt "go.etcd.io/bbolt"
)

// PurgeUser removes every piece of per-user metadata (mailboxes, message
// rows, subscriptions, threads, change log, query snapshots, pending uploads
// and ACL entries owned by or granted to the user) in one transaction
// (F6649c). UIDVALIDITY floors are kept, so a mailbox re-created under the
// same address gets a strictly larger UIDVALIDITY than the purged one.
func (db *Database) PurgeUser(user string) error {
	if db == nil || db.bolt == nil {
		return nil
	}
	ku := keyUser(user)
	return db.bolt.Update(func(tx *bbolt.Tx) error {
		var doomed [][]byte
		for _, p := range []string{"mailbox:" + ku + ":", "msgs:" + ku + ":"} {
			pre := []byte(p)
			c := tx.Cursor()
			for k, _ := c.Seek(pre); k != nil && bytes.HasPrefix(k, pre); k, _ = c.Next() {
				doomed = append(doomed, append([]byte(nil), k...))
			}
		}
		for _, name := range []string{subscribedBucket(user), threadBucket(user), changesBucket(user), querySnapshotsBucket(user)} {
			if tx.Bucket([]byte(name)) != nil {
				doomed = append(doomed, []byte(name))
			}
		}
		for _, k := range doomed {
			if bytes.HasPrefix(k, []byte("mailbox:")) {
				if err := recordUIDValidityFloorTx(tx, string(k)); err != nil {
					return err
				}
			}
			if err := tx.DeleteBucket(k); err != nil {
				return fmt.Errorf("purge bucket %q: %w", k, err)
			}
		}
		if ab := tx.Bucket([]byte("acl")); ab != nil {
			var keys [][]byte
			pre := []byte("acl:" + ku + ":")
			_ = ab.ForEach(func(k, v []byte) error {
				var e ACLEntry
				if bytes.HasPrefix(k, pre) || (json.Unmarshal(v, &e) == nil && e.Grantee == user) {
					keys = append(keys, append([]byte(nil), k...))
				}
				return nil
			})
			for _, k := range keys {
				if err := ab.Delete(k); err != nil {
					return err
				}
			}
		}
		if pb := tx.Bucket([]byte(pendingUploadsBucketName)); pb != nil {
			pre := []byte(user + "\x00")
			var keys [][]byte
			c := pb.Cursor()
			for k, _ := c.Seek(pre); k != nil && bytes.HasPrefix(k, pre); k, _ = c.Next() {
				keys = append(keys, append([]byte(nil), k...))
			}
			for _, k := range keys {
				if err := pb.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// PurgeUser removes all of a user's blob files (F6649c). It serialises with
// writers through the per-user locks; a user with no data is not an error.
func (s *MessageStore) PurgeUser(user string) error {
	if err := validatePathComponent(user); err != nil {
		return err
	}
	g := s.UserGuard(user)
	g.Lock()
	defer g.Unlock()
	defer s.lockUser(user)()
	return os.RemoveAll(filepath.Join(s.basePath, user))
}
