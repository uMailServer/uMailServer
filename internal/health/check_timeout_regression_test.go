package health

import (
	"context"
	"testing"
)

// TestRegressionF5189_StuckCheckerDoesNotHangReport: a checker that ignores
// its context must be reported unhealthy once the context is done instead of
// blocking Monitor.Check (and /health, /health/ready) forever.
func TestRegressionF5189_StuckCheckerDoesNotHangReport(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	m := NewMonitor("test")
	m.Register("ok", func(ctx context.Context) Check { return Check{Status: StatusHealthy} })
	m.Register("stuck", func(ctx context.Context) Check { <-release; return Check{Status: StatusHealthy} })
	m.Register("ctx_aware", func(ctx context.Context) Check {
		<-ctx.Done()
		return Check{Status: StatusDegraded, Message: "aware"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := m.Check(ctx) // must return: every check context is already done

	if report.Status != StatusUnhealthy {
		t.Errorf("overall status %s, want unhealthy", report.Status)
	}
	byName := map[string]Check{}
	for _, c := range report.Checks {
		byName[c.Name] = c
	}
	if len(byName) != 3 {
		t.Fatalf("got %d checks, want 3", len(byName))
	}
	if byName["stuck"].Status != StatusUnhealthy {
		t.Errorf("stuck checker status %s, want unhealthy", byName["stuck"].Status)
	}

	ready := m.CheckReadiness(ctx)
	if ready["ready"].(bool) {
		t.Error("readiness reported ready with a stuck checker")
	}
}

// TestRegressionF5189_PromptCheckersUnaffected: with a live context, checkers
// that return promptly keep their own results.
func TestRegressionF5189_PromptCheckersUnaffected(t *testing.T) {
	m := NewMonitor("test")
	m.Register("ok", func(ctx context.Context) Check { return Check{Status: StatusHealthy, Message: "fine"} })
	m.Register("deg", func(ctx context.Context) Check { return Check{Status: StatusDegraded} })
	r := m.Check(context.Background())
	if r.Status != StatusDegraded || len(r.Checks) != 2 {
		t.Fatalf("status %s checks %d", r.Status, len(r.Checks))
	}
	for _, c := range r.Checks {
		if c.Name == "ok" && c.Message != "fine" {
			t.Errorf("prompt checker result replaced: %+v", c)
		}
	}
}
