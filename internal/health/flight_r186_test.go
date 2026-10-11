package health

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// F6632: a concurrent probe must share the in-flight run, not report a
// healthy-but-slow checker as unhealthy; checker runs once at a time.
func TestConcurrentProbesShareInflightRun_F6632(t *testing.T) {
	m := NewMonitor("t")
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var runs int32
	m.Register("slow", func(ctx context.Context) Check {
		atomic.AddInt32(&runs, 1)
		started <- struct{}{}
		<-release
		return Check{Status: StatusHealthy}
	})
	joined := make(chan struct{}, 1)
	testHookJoin = func() { joined <- struct{}{} }
	defer func() { testHookJoin = nil }()

	ra := make(chan Report, 1)
	go func() { ra <- m.Check(context.Background()) }()
	<-started
	rb := make(chan Report, 1)
	go func() { rb <- m.Check(context.Background()) }()
	<-joined
	close(release)
	for _, r := range []Report{<-ra, <-rb} {
		if r.Status != StatusHealthy {
			t.Fatalf("probe saw %s: %+v", r.Status, r.Checks)
		}
	}
	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Fatalf("checker ran %d times, want 1", n)
	}
}

// F6633: a hung ping spawns at most one goroutine across repeated probes.
func TestDatabaseCheckHungPingNoGoroutinePileup_F6633(t *testing.T) {
	var pings int32
	block := make(chan struct{})
	chk := DatabaseCheck(func() error { atomic.AddInt32(&pings, 1); <-block; return nil })
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		c := chk(ctx)
		cancel()
		if c.Status != StatusUnhealthy {
			t.Fatalf("probe %d: %s", i, c.Status)
		}
	}
	if n := atomic.LoadInt32(&pings); n != 1 {
		t.Fatalf("pings=%d, want 1", n)
	}
	close(block)
}
