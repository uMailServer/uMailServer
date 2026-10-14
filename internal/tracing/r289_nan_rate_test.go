package tracing

import (
	"context"
	"math"
	"testing"
)

func r289Sampled(t *testing.T, rate float64) int {
	t.Helper()
	p, err := NewProvider(Config{Enabled: true, Exporter: "noop", SampleRate: rate})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := p.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
		if err := p.Stop(context.Background()); err != nil {
			t.Errorf("second Stop: %v", err)
		}
	}()
	n := 0
	for i := 0; i < 500; i++ {
		_, s := p.StartSpan(context.Background(), "x")
		if s.SpanContext().IsSampled() {
			n++
		}
		s.End()
	}
	return n
}

// F7631: a NaN sample_rate slipped past the clamps and sampled everything.
func TestR289SampleRateBounds(t *testing.T) {
	for rate, want := range map[float64]int{math.NaN(): 0, math.Inf(-1): 0, -1: 0, 0: 0, 1: 500, 2: 500, math.Inf(1): 500} {
		if got := r289Sampled(t, rate); got != want {
			t.Errorf("rate %v sampled %d, want %d", rate, got, want)
		}
	}
}
