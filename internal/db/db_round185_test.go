package db

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db/migrations"
	"go.etcd.io/bbolt"
)

func openR185(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, p
}

// F6623: deleting inside bbolt's ForEach skips the key after each delete, so
// expired revocations survived cleanup and the list grew without bound.
func TestR185_CleanupRevokedTokensRemovesAllExpired(t *testing.T) {
	d, _ := openR185(t)
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketRevokedTokens))
		past, _ := time.Now().Add(-time.Hour).MarshalJSON()
		for i := 0; i < 500; i++ {
			if err := b.Put([]byte(fmt.Sprintf("h%04d", i)), past); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CleanupRevokedTokens(); err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = d.ForEach(BucketRevokedTokens, func(string, []byte) error { n++; return nil })
	if n != 0 {
		t.Fatalf("%d expired revocations survived cleanup", n)
	}
}

// F6624: StoreRevokedToken must bound growth by purging expired entries.
func TestR185_StoreRevokedTokenPurgesExpired(t *testing.T) {
	d, _ := openR185(t)
	err := d.bolt.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BucketRevokedTokens))
		past, _ := time.Now().Add(-time.Hour).MarshalJSON()
		for i := 0; i < 50; i++ {
			_ = b.Put([]byte(fmt.Sprintf("old%02d", i)), past)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.StoreRevokedToken("live", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = d.ForEach(BucketRevokedTokens, func(string, []byte) error { n++; return nil })
	if n != 1 {
		t.Fatalf("want 1 live revocation, got %d", n)
	}
	if r, _ := d.IsTokenRevoked("live"); !r {
		t.Fatal("live token must stay revoked")
	}
}

// F6625: a missing bucket (older/partially initialised file) is recreated.
func TestR185_OpenRecreatesMissingBuckets(t *testing.T) {
	d, p := openR185(t)
	_ = d.bolt.Update(func(tx *bbolt.Tx) error {
		_ = tx.DeleteBucket([]byte(BucketQueue))
		return tx.DeleteBucket([]byte(BucketRevokedTokens))
	})
	_ = d.Close()
	d2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if err := d2.Enqueue(&QueueEntry{ID: "a", Status: "pending"}); err != nil {
		t.Fatalf("queue bucket not recreated: %v", err)
	}
	if err := d2.bolt.View(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(BucketRevokedTokens)) == nil {
			return errors.New("revoked_tokens missing")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// F6620: a database written by a newer binary is refused, not downgraded.
func TestR185_OpenRefusesNewerSchema(t *testing.T) {
	d, p := openR185(t)
	if err := d.RunMigrations(); err != nil {
		t.Fatal(err)
	}
	_ = d.bolt.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket([]byte("__migrations")).Put([]byte("999"), []byte(`{"version":"999"}`))
	})
	_ = d.Close()
	d2, err := Open(p)
	if err == nil {
		_ = d2.Close()
		t.Fatal("Open must refuse a newer schema")
	}
	if !errors.Is(err, migrations.ErrSchemaTooNew) {
		t.Fatalf("want ErrSchemaTooNew, got %v", err)
	}
}

// F6626: Backup is a consistent online snapshot that round-trips.
func TestR185_BackupRoundTripWhileWriting(t *testing.T) {
	d, _ := openR185(t)
	if err := d.CreateDomain(&DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&AccountData{Domain: "example.com", LocalPart: "u", Email: "u@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := d.StoreRevokedToken("tok", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.Enqueue(&QueueEntry{ID: "q1", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_ = d.Enqueue(&QueueEntry{ID: fmt.Sprintf("w%d", i), Status: "pending"})
			}
		}
	}()
	var buf bytes.Buffer
	if err := d.Backup(&buf); err != nil {
		t.Fatal(err)
	}
	close(stop)
	wg.Wait()

	rp := filepath.Join(t.TempDir(), "restored.db")
	if err := writeFileR185(rp, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	r, err := Open(rp)
	if err != nil {
		t.Fatalf("restored copy must open: %v", err)
	}
	defer r.Close()
	if _, err := r.GetAccount("example.com", "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetDomain("example.com"); err != nil {
		t.Fatal(err)
	}
	if rev, _ := r.IsTokenRevoked("tok"); !rev {
		t.Fatal("revocation list must be in the snapshot")
	}
	if _, err := r.GetQueueEntry("q1"); err != nil {
		t.Fatal(err)
	}
}

// F6627: claiming is atomic: exactly one of many concurrent claimers wins.
func TestR185_ClaimQueueEntryExactlyOnce(t *testing.T) {
	d, _ := openR185(t)
	if err := d.Enqueue(&QueueEntry{ID: "m", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	const N = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			e, err := d.ClaimQueueEntry("m", time.Now())
			if err == nil {
				if e.Status != "sending" {
					t.Errorf("claimed status %q", e.Status)
				}
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, ErrQueueEntryNotClaimable) {
				t.Errorf("unexpected %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d claimers won, want 1", wins)
	}
	got, _ := d.GetQueueEntry("m")
	if got.Status != "sending" {
		t.Fatalf("stored status %q", got.Status)
	}
	if _, err := d.ClaimQueueEntry("gone", time.Now()); !errors.Is(err, ErrQueueEntryNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

// F6628: a ForEach callback may write back into the DB (no deadlock, no
// retained mmap slices) and may mutate the bucket being iterated.
func TestR185_ForEachCallbackMayWrite(t *testing.T) {
	d, _ := openR185(t)
	for i := 0; i < 1000; i++ {
		if err := d.Enqueue(&QueueEntry{ID: fmt.Sprintf("e%04d", i), Status: "pending"}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() {
		seen := 0
		err := d.ForEach(BucketQueue, func(k string, v []byte) error {
			seen++
			return d.Dequeue(k)
		})
		if err == nil && seen != 1000 {
			err = fmt.Errorf("saw %d of 1000", seen)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("ForEach callback writing to DB deadlocked")
	}
	n := 0
	_ = d.ForEach(BucketQueue, func(string, []byte) error { n++; return nil })
	if n != 0 {
		t.Fatalf("%d left", n)
	}
}

func TestR185_ForEachPrefixPagesAndStops(t *testing.T) {
	d, _ := openR185(t)
	for i := 0; i < 700; i++ {
		_ = d.Put(BucketIndex, fmt.Sprintf("a/%04d", i), i)
		_ = d.Put(BucketIndex, fmt.Sprintf("b/%04d", i), i)
	}
	n, last := 0, ""
	if err := d.ForEachPrefix(BucketIndex, "a/", func(k string, _ []byte) error {
		if k <= last {
			t.Fatalf("out of order %q after %q", k, last)
		}
		last = k
		n++
		return nil
	}); err != nil || n != 700 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	stop := errors.New("stop")
	n = 0
	if err := d.ForEach(BucketIndex, func(string, []byte) error { n++; return stop }); !errors.Is(err, stop) || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// F6629: large values and oversized keys surface errors, never corrupt.
func TestR185_LargeValueAndKeyLimits(t *testing.T) {
	d, _ := openR185(t)
	big := bytes.Repeat([]byte("x"), 5<<20)
	if err := d.Put(BucketIndex, "big", string(big)); err != nil {
		t.Fatal(err)
	}
	var s string
	if err := d.Get(BucketIndex, "big", &s); err != nil || len(s) != len(big) {
		t.Fatalf("roundtrip len=%d err=%v", len(s), err)
	}
	if err := d.Put(BucketIndex, string(bytes.Repeat([]byte("k"), 40000)), 1); err == nil {
		t.Fatal("oversized key must error")
	}
	if err := d.Put(BucketIndex, "", 1); err == nil {
		t.Fatal("empty key must error")
	}
}

func writeFileR185(p string, b []byte) error { return os.WriteFile(p, b, 0o600) }
