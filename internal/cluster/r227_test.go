package cluster

import (
	"context"
	"testing"
	"time"
)

// F7043: heartbeat must record real leadership instead of a constant false.
func TestHeartbeatRecordsLeader(t *testing.T) {
	f := newFakeRedis()
	f.Put("lock:leader:server", "me", 5000)
	h := &RedisHealthMonitor{client: f.client(), instanceID: "me", ttl: time.Minute}
	if err := h.RecordHeartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.hashes["health:me"]["is_leader"]; got != "1" && got != "true" {
		t.Fatalf("is_leader = %q, want true", got)
	}
	f.Put("lock:leader:server", "other", 5000)
	_ = h.RecordHeartbeat(context.Background())
	if got := f.hashes["health:me"]["is_leader"]; got == "1" || got == "true" {
		t.Fatalf("is_leader = %q for non-leader", got)
	}
}

// F7044: a lock named "leader:server" must not collide with the leader key.
func TestLockKeyDoesNotOverlapLeaderKey(t *testing.T) {
	if lockKey("leader:server") == leaderKey("server") {
		t.Fatal("lock key overlaps leader election key")
	}
	f := newFakeRedis()
	f.Put("lock:leader:server", "leader-instance", 5000)
	l := &RedisDistributedLock{client: f.client()}
	ok, err := l.Acquire(context.Background(), "leader:server", time.Second)
	if err != nil || !ok {
		t.Fatalf("independent lock should be acquirable: %v %v", ok, err)
	}
	if v, _, _ := f.State("lock:leader:server"); v != "leader-instance" {
		t.Fatal("lock clobbered the leader key")
	}
}
