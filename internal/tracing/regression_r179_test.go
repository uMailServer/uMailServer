package tracing

import (
	"context"
	"testing"
	"time"
)

// F6574: Stop with a deadline-less context must not hang forever on a stuck
// exporter flush.
func TestR179StopBoundedWithoutDeadline(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	p := &Provider{enabled: true, stopFunc: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			select {
			case <-block:
			case <-time.After(5 * time.Second):
				t.Error("stop ctx had no deadline")
			}
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	p.shutdownTimeout = 20 * time.Millisecond
	err := p.Stop(context.Background())
	if err != context.DeadlineExceeded {
		t.Fatalf("err=%v want DeadlineExceeded", err)
	}
}
