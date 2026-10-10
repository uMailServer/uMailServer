package tracing

import (
	"context"
	"testing"
)

func TestF6298_StopIdempotent(t *testing.T) {
	calls := 0
	p := &Provider{enabled: true, stopFunc: func(context.Context) error { calls++; return nil }}
	for i := 0; i < 3; i++ {
		if err := p.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("stopFunc ran %d times", calls)
	}
}
