package cluster

import (
	"context"
	"strings"
	"testing"
	"time"
)

// F6969: a zero/negative TTL made SET NX carry no expiry, so a crashed lock
// holder or leader blocked the whole cluster forever.
func TestLockAcquireRejectsNonExpiringTTL(t *testing.T) {
	f := newFakeRedis()
	l := &RedisDistributedLock{client: f.client()}
	defer l.Close()
	for _, ttl := range []time.Duration{0, -time.Second} {
		ok, err := l.Acquire(context.Background(), "job", ttl)
		if err == nil || ok {
			t.Fatalf("ttl %v: Acquire = %v, %v; want error", ttl, ok, err)
		}
		if _, _, held := f.State("lock:job"); held {
			t.Fatalf("ttl %v: a non-expiring lock was stored", ttl)
		}
	}
}

func TestLeaderZeroLeaseGetsExpiry(t *testing.T) {
	f := newFakeRedis()
	l := &RedisLeaderElection{client: f.client(), instanceID: "me"} // leaseTTL zero
	defer l.Close()
	ok, err := l.TryAcquire(context.Background(), "server")
	if err != nil || !ok {
		t.Fatalf("TryAcquire: %v %v", ok, err)
	}
	if _, ttl, _ := f.State("lock:leader:server"); ttl <= 0 {
		t.Fatalf("leader key has no expiry (ttl=%d): a dead leader would never be replaced", ttl)
	}
	if err := l.Refresh(context.Background(), "server"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, ttl, _ := f.State("lock:leader:server"); ttl <= 0 {
		t.Fatalf("refresh left ttl=%d", ttl)
	}
}

// F6962: an empty instance ID would make all such instances "leader".
func TestLeaderElectionRequiresInstanceID(t *testing.T) {
	_, err := NewRedisLeaderElection("audit.invalid:0", "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "instance ID") {
		t.Fatalf("err = %v, want instance ID validation error", err)
	}
}
