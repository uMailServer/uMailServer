package health

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// F6503: a checker ignoring its context leaked one goroutine per probe.
func TestF6503_HungCheckerNotRespawned(t *testing.T) {
	m := NewMonitor("t")
	var started atomic.Int32
	release := make(chan struct{})
	m.Register("hung", func(ctx context.Context) Check {
		started.Add(1)
		<-release
		return Check{Status: StatusHealthy}
	})
	defer close(release)

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		r := m.Check(ctx)
		cancel()
		if r.Status != StatusUnhealthy || len(r.Checks) != 1 {
			t.Fatalf("probe %d: status=%v checks=%d", i, r.Status, len(r.Checks))
		}
	}
	if n := started.Load(); n != 1 {
		t.Fatalf("hung checker started %d times, want 1", n)
	}
}
