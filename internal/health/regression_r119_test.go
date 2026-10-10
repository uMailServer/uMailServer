package health

import (
	"context"
	"testing"
)

// F6015: usage must follow df semantics (root-reserved blocks are neither
// used nor available) and unset (<=0) thresholds must be disabled, not
// "always critical".
func TestRegressionF6015DiskUsageAndThresholds(t *testing.T) {
	// 1000 blocks, 50 reserved for root (bfree=300 includes them, bavail=250):
	// used = 700, avail = 250 -> df says 700/950 = 73.7%.
	pct, ok := diskUsagePercent(1000, 300, 250)
	if !ok || pct < 73.6 || pct > 73.8 {
		t.Errorf("usage = %v ok=%v, want ~73.7", pct, ok)
	}
	if _, ok := diskUsagePercent(0, 0, 0); ok {
		t.Error("zero capacity must report !ok")
	}
	if s, _ := diskStatus(50, 0, 0); s != StatusHealthy {
		t.Errorf("unset thresholds: status %v, want healthy", s)
	}
	if s, _ := diskStatus(85, 80, 0); s != StatusDegraded {
		t.Errorf("warning only: %v", s)
	}
	if s, _ := diskStatus(96, 80, 95); s != StatusUnhealthy {
		t.Errorf("critical: %v", s)
	}
}

// F6016: a panicking checker must be reported unhealthy, not crash the process.
func TestRegressionF6016CheckerPanicContained(t *testing.T) {
	m := NewMonitor("t")
	m.Register("boom", func(ctx context.Context) Check { panic("kaboom") })
	m.Register("ok", func(ctx context.Context) Check { return Check{Status: StatusHealthy} })
	r := m.Check(context.Background())
	if r.Status != StatusUnhealthy {
		t.Fatalf("status = %v", r.Status)
	}
	if len(r.Checks) != 2 || r.Checks[0].Name != "boom" || r.Checks[1].Name != "ok" {
		t.Fatalf("checks not complete/sorted: %+v", r.Checks)
	}
}
