package jmap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// Unreferenced-upload accounting (RFC 8620 §6.1): a blob stored by
// /jmap/upload that no Email references may be expired by the server after at
// least an hour. The blob lives in the content-addressed message store (the
// same files Email/import turns into Emails), so a pending upload is tracked
// in a bbolt bucket (survives restarts) until Email/import references it or
// the GC expires it. Per account at most maxPendingUploads unreferenced
// uploads are kept (oldest evicted first), bounding unreferenced bytes to
// maxPendingUploads x the 50 MiB upload limit.
const (
	pendingUploadsBucket = "jmap_pending_uploads"
	maxPendingUploads    = 10
	defaultUploadTTL     = 2 * time.Hour // RFC 8620 §6.1: at least 1 hour
	defaultUploadGCEvery = 10 * time.Minute
)

type pendingUpload struct {
	At   int64 `json:"at"` // unix nanoseconds of the (latest) upload
	Size int64 `json:"size"`
}

type uploadGC struct {
	mu      sync.Mutex
	started bool
	stopped bool
	stop    chan struct{}
	done    chan struct{}
}

func pendingKey(user, blobID string) []byte {
	return []byte(user + "\x00" + blobID)
}

// userUploadLock serializes upload tracking, import reference marking and
// GC deletion for one account.
func (s *Server) userUploadLock(user string) *sync.Mutex {
	m, _ := s.uploadLocks.LoadOrStore(user, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// trackUpload records blobID as an unreferenced upload and enforces the
// per-account bound. Caller holds the user's upload lock.
func (s *Server) trackUpload(user, blobID string, size int64, now time.Time) {
	if s.db == nil || s.msgStore == nil {
		return
	}
	rec, _ := json.Marshal(pendingUpload{At: now.UnixNano(), Size: size})
	var evict []string
	_ = s.db.Bolt().Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(pendingUploadsBucket))
		if err != nil {
			return err
		}
		if err := b.Put(pendingKey(user, blobID), rec); err != nil {
			return err
		}
		type ent struct {
			id string
			at int64
		}
		var ents []ent
		prefix := []byte(user + "\x00")
		c := b.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var p pendingUpload
			_ = json.Unmarshal(v, &p)
			ents = append(ents, ent{string(k[len(prefix):]), p.At})
		}
		if len(ents) > maxPendingUploads {
			sort.Slice(ents, func(i, j int) bool { return ents[i].at < ents[j].at })
			for _, e := range ents[:len(ents)-maxPendingUploads] {
				if e.id == blobID {
					continue
				}
				evict = append(evict, e.id)
				if err := b.Delete(pendingKey(user, e.id)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	for _, id := range evict {
		s.dropUnreferencedBlob(user, id)
	}
}

// untrackUpload marks blobID referenced (an Email now owns it).
func (s *Server) untrackUpload(user, blobID string) {
	if s.db == nil {
		return
	}
	_ = s.db.Bolt().Update(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(pendingUploadsBucket)); b != nil {
			return b.Delete(pendingKey(user, blobID))
		}
		return nil
	})
}

func (s *Server) isPendingUpload(user, blobID string) bool {
	if s.db == nil {
		return false
	}
	found := false
	_ = s.db.Bolt().View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(pendingUploadsBucket)); b != nil {
			found = b.Get(pendingKey(user, blobID)) != nil
		}
		return nil
	})
	return found
}

// dropUnreferencedBlob deletes the blob unless some Email references it.
func (s *Server) dropUnreferencedBlob(user, blobID string) {
	if len(s.findEmailCopies(user, blobID)) == 0 {
		_ = s.msgStore.DeleteMessage(user, blobID)
	}
}

// SweepUploads expires pending uploads older than the TTL as of now and
// returns how many blobs were removed. A blob that an Email references is
// only un-tracked, never deleted.
func (s *Server) SweepUploads(now time.Time) int {
	if s.db == nil || s.msgStore == nil {
		return 0
	}
	ttl := s.uploadTTL
	if ttl <= 0 {
		ttl = defaultUploadTTL
	}
	type key struct{ user, id string }
	var expired []key
	_ = s.db.Bolt().View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(pendingUploadsBucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var p pendingUpload
			_ = json.Unmarshal(v, &p)
			if now.Sub(time.Unix(0, p.At)) >= ttl {
				i := bytes.IndexByte(k, 0)
				if i > 0 {
					expired = append(expired, key{string(k[:i]), string(k[i+1:])})
				}
			}
			return nil
		})
	})
	removed := 0
	for _, e := range expired {
		mu := s.userUploadLock(e.user)
		mu.Lock()
		// Re-check under the lock: a re-upload may have refreshed it.
		still := false
		_ = s.db.Bolt().View(func(tx *bbolt.Tx) error {
			if b := tx.Bucket([]byte(pendingUploadsBucket)); b != nil {
				if v := b.Get(pendingKey(e.user, e.id)); v != nil {
					var p pendingUpload
					_ = json.Unmarshal(v, &p)
					still = now.Sub(time.Unix(0, p.At)) >= ttl
				}
			}
			return nil
		})
		if still {
			if len(s.findEmailCopies(e.user, e.id)) == 0 {
				if s.msgStore.DeleteMessage(e.user, e.id) == nil {
					removed++
				}
			}
			s.untrackUpload(e.user, e.id)
		}
		mu.Unlock()
	}
	return removed
}

// StartUploadGC starts the background expiry of unreferenced uploads. It is
// idempotent and is also started lazily by the first upload. Stop ends it.
func (s *Server) StartUploadGC() {
	g := &s.gc
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started || g.stopped {
		return
	}
	g.started = true
	g.stop = make(chan struct{})
	g.done = make(chan struct{})
	every := s.uploadGCEvery
	if every <= 0 {
		every = defaultUploadGCEvery
	}
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				s.SweepUploads(now)
			}
		}
	}(g.stop, g.done)
}

// Stop terminates the upload GC goroutine and waits for it. Safe to call
// multiple times and when the GC never started; after Stop it does not
// restart.
func (s *Server) Stop() {
	g := &s.gc
	g.mu.Lock()
	g.stopped = true
	stop, done := g.stop, g.done
	g.stop = nil
	g.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}
