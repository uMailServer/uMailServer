package cluster

import (
	"context"
	"testing"
	"time"
)

func leadersOf(t *testing.T, m *RedisHealthMonitor) []string {
	t.Helper()
	h, err := m.GetInstanceHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, x := range h {
		if x.IsLeader {
			out = append(out, x.InstanceID)
		}
	}
	return out
}

// F7380: leadership in instance health must follow the live leader key, not
// the is_leader flag cached at each instance's last heartbeat.
func TestInstanceHealthLeaderFollowsLiveKey(t *testing.T) {
	f := newFakeRedis()
	ctx := context.Background()
	a := &RedisHealthMonitor{client: f.client(), instanceID: "a", ttl: time.Minute}
	b := &RedisHealthMonitor{client: f.client(), instanceID: "b", ttl: time.Minute}
	f.Put("lock:leader:server", "a", 5000)
	_ = a.RecordHeartbeat(ctx)
	_ = b.RecordHeartbeat(ctx)
	if got := leadersOf(t, a); len(got) != 1 || got[0] != "a" {
		t.Fatalf("leaders = %v, want [a]", got)
	}
	f.Put("lock:leader:server", "b", 5000) // failover, no new heartbeats
	if got := leadersOf(t, a); len(got) != 1 || got[0] != "b" {
		t.Fatalf("after failover leaders = %v, want [b]", got)
	}
	delete(f.vals, "lock:leader:server") // lease expired / released
	if got := leadersOf(t, a); len(got) != 0 {
		t.Fatalf("with no leader key leaders = %v, want none", got)
	}
}
