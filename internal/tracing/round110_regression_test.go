package tracing

import (
	"context"
	"testing"
)

func TestR110_DisabledStartSpanDoesNotEndParent(t *testing.T) {
	on, err := NewProvider(Config{Enabled: true, Exporter: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	defer on.Stop(context.Background())
	ctx, parent := on.StartSpan(context.Background(), "parent")
	off := &Provider{}
	_, child := off.StartSpan(ctx, "child")
	child.End()
	if !parent.IsRecording() {
		t.Fatal("disabled provider's span.End() ended the parent span")
	}
	parent.End()
	var nilP *Provider
	if nilP.IsEnabled() || nilP.Stop(ctx) != nil {
		t.Fatal("nil provider not safe")
	}
}
