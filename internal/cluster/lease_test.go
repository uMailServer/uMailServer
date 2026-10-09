package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestLock(t *testing.T) (*fakeRedis, *RedisDistributedLock, string) {
	t.Helper()
	f := newFakeRedis()
	l := &RedisDistributedLock{client: f.client()}
	t.Cleanup(func() { l.Close() })
	ok, err := l.Acquire(context.Background(), "job", 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("acquire: %v %v", ok, err)
	}
	tok, _, _ := f.State("lock:job")
	return f, l, tok
}

func newTestLeader(t *testing.T) (*fakeRedis, *RedisLeaderElection) {
	t.Helper()
	f := newFakeRedis()
	l := &RedisLeaderElection{client: f.client(), instanceID: "me", leaseTTL: 15 * time.Second}
	t.Cleanup(func() { l.Close() })
	ok, err := l.TryAcquire(context.Background(), "server")
	if err != nil || !ok {
		t.Fatalf("acquire: %v %v", ok, err)
	}
	f.Put("lock:leader:server", "me", 1000)
	return f, l
}

// F4865: Extend must report failure when the lock is no longer ours.
func TestDistributedLockExtendLostLock(t *testing.T) {
	ctx := context.Background()
	f, l, _ := newTestLock(t)
	if err := l.Extend(ctx, "job", 10*time.Second); err != nil {
		t.Fatalf("extend while held: %v", err)
	}
	f.Put("lock:job", "other-instance", 30000)
	if err := l.Extend(ctx, "job", 10*time.Second); err == nil {
		t.Fatal("Extend reported success on a lock it does not hold")
	}
	if v, ttl, _ := f.State("lock:job"); v != "other-instance" || ttl != 30000 {
		t.Fatalf("other owner's lock modified: %s %d", v, ttl)
	}

	f2 := newFakeRedis()
	l2 := &RedisDistributedLock{client: f2.client()}
	defer l2.Close()
	if err := l2.Extend(ctx, "never", time.Second); err == nil {
		t.Fatal("Extend of never-acquired lock succeeded")
	}
}

// F4866: a sub-second TTL must not turn into EXPIRE 0 (which deletes the key).
func TestDistributedLockExtendSubSecond(t *testing.T) {
	ctx := context.Background()
	f, l, tok := newTestLock(t)
	if err := l.Extend(ctx, "job", 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if v, ttl, ok := f.State("lock:job"); !ok || v != tok || ttl != 500 {
		t.Fatalf("held=%v ours=%v ttl=%d", ok, v == tok, ttl)
	}
	if err := l.Extend(ctx, "job", 0); err == nil {
		t.Fatal("zero TTL accepted")
	}
	if _, _, ok := f.State("lock:job"); !ok {
		t.Fatal("zero TTL released the lock")
	}
}

// Concurrent use of distinct locks must be race-free (run with -race).
func TestDistributedLockConcurrentKeys(t *testing.T) {
	ctx := context.Background()
	f := newFakeRedis()
	l := &RedisDistributedLock{client: f.client()}
	defer l.Close()
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			k := fmt.Sprintf("k%d", i)
			if ok, err := l.Acquire(ctx, k, time.Second); err != nil || !ok {
				errs <- fmt.Errorf("acquire %s: %v %v", k, ok, err)
				return
			}
			if err := l.Extend(ctx, k, 2*time.Second); err != nil {
				errs <- err
				return
			}
			if err := l.Release(ctx, k); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// F4867: another instance takes the lease immediately before each command
// Refresh issues (deterministic interleaving). Refresh must fail and must
// never extend the new leader's lease.
func TestLeaderRefreshTakeoverInterleaving(t *testing.T) {
	ctx := context.Background()
	f, l := newTestLeader(t)
	if err := l.Refresh(ctx, "server"); err != nil {
		t.Fatalf("refresh as leader: %v", err)
	}
	if _, ttl, _ := f.State("lock:leader:server"); ttl != 15000 {
		t.Fatalf("ttl=%d", ttl)
	}
	for k := 1; k <= 3; k++ {
		f, l := newTestLeader(t)
		base := f.n
		f.beforeCmd = func(n int) {
			if n == base+k {
				f.Put("lock:leader:server", "other", 5000)
			}
		}
		err := l.Refresh(ctx, "server")
		v, ttl, _ := f.State("lock:leader:server")
		if v == "other" && (err == nil || ttl != 5000) {
			t.Fatalf("k=%d: Refresh extended another instance's lease (err=%v ttl=%d)", k, err, ttl)
		}
	}
}
