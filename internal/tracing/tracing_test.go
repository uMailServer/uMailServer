package tracing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewTracer(t *testing.T) {
	tracer := NewTracer("test-service", nil)
	if tracer == nil {
		t.Fatal("NewTracer returned nil")
	}
	if tracer.serviceName != "test-service" {
		t.Errorf("expected service name 'test-service', got %s", tracer.serviceName)
	}
}

func TestTracer_StartSpan(t *testing.T) {
	tracer := NewTracer("test", nil)
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "test-span")
	if span == nil {
		t.Fatal("StartSpan returned nil span")
	}

	if span.Name != "test-span" {
		t.Errorf("expected span name 'test-span', got %s", span.Name)
	}

	if span.TraceID == "" {
		t.Error("expected non-empty trace ID")
	}

	if span.SpanID == "" {
		t.Error("expected non-empty span ID")
	}

	// Check context has span
	if SpanFromContext(ctx) != span {
		t.Error("context should contain the span")
	}
}

func TestTracer_StartSpan_WithParent(t *testing.T) {
	tracer := NewTracer("test", nil)
	ctx := context.Background()

	// Create parent
	ctx, parent := tracer.StartSpan(ctx, "parent")

	// Create child
	_, child := tracer.StartSpan(ctx, "child")

	if child.TraceID != parent.TraceID {
		t.Error("child should have same trace ID as parent")
	}

	if child.ParentID != parent.SpanID {
		t.Error("child should have parent ID set to parent's span ID")
	}
}

func TestSpan_SetAttribute(t *testing.T) {
	span := &Span{
		Attributes: make(map[string]interface{}),
	}

	span.SetAttribute("key", "value")

	if span.Attributes["key"] != "value" {
		t.Error("attribute not set correctly")
	}
}

func TestSpan_AddEvent(t *testing.T) {
	span := &Span{}

	span.AddEvent("test-event", map[string]interface{}{"key": "value"})

	if len(span.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(span.Events))
	}

	if span.Events[0].Name != "test-event" {
		t.Errorf("expected event name 'test-event', got %s", span.Events[0].Name)
	}
}

func TestSpan_RecordError(t *testing.T) {
	span := &Span{}
	testErr := errors.New("test error")

	span.RecordError(testErr)

	if span.Error != testErr {
		t.Error("error not recorded correctly")
	}
}

func TestSpan_Finish(t *testing.T) {
	span := &Span{}

	if !span.EndTime.IsZero() {
		t.Error("end time should be zero before finish")
	}

	span.Finish()

	if span.EndTime.IsZero() {
		t.Error("end time should be set after finish")
	}
}

func TestSpanFromContext(t *testing.T) {
	// No span in context
	ctx := context.Background()
	if SpanFromContext(ctx) != nil {
		t.Error("should return nil for context without span")
	}

	// With span
	span := &Span{Name: "test"}
	ctx = ContextWithSpan(ctx, span)

	retrieved := SpanFromContext(ctx)
	if retrieved != span {
		t.Error("should return the same span")
	}
}

func TestContextWithSpan(t *testing.T) {
	ctx := context.Background()
	span := &Span{Name: "test"}

	newCtx := ContextWithSpan(ctx, span)

	if newCtx == ctx {
		t.Error("ContextWithSpan should return a new context")
	}
}

func TestNoopExporter(t *testing.T) {
	exporter := &NoopExporter{}
	span := &Span{Name: "test"}

	// Should not panic
	exporter.Export(span)
}

func TestNewProvider(t *testing.T) {
	// Disabled provider
	provider := NewProvider(Config{Enabled: false})
	if provider.enabled {
		t.Error("provider should be disabled")
	}

	// Enabled provider
	provider = NewProvider(Config{Enabled: true, ServiceName: "test"})
	if !provider.enabled {
		t.Error("provider should be enabled")
	}
	if provider.tracer == nil {
		t.Error("tracer should be set")
	}
}

func TestProvider_StartSpan_Disabled(t *testing.T) {
	provider := NewProvider(Config{Enabled: false})
	ctx := context.Background()

	ctx, span := provider.StartSpan(ctx, "test")
	if span != nil {
		t.Error("should return nil span when disabled")
	}
}

func TestProvider_StartSpan_Enabled(t *testing.T) {
	provider := NewProvider(Config{Enabled: true, ServiceName: "test"})
	ctx := context.Background()

	ctx, span := provider.StartSpan(ctx, "test")
	if span == nil {
		t.Fatal("should return span when enabled")
	}

	if span.Name != "test" {
		t.Errorf("expected name 'test', got %s", span.Name)
	}
}

func TestProvider_Stop(t *testing.T) {
	called := false
	provider := &Provider{
		enabled: true,
		stopFunc: func() {
			called = true
		},
	}

	provider.Stop()

	if !called {
		t.Error("stopFunc should be called")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("generateID should return non-empty string")
	}

	if id1 == id2 {
		t.Error("generateID should return unique IDs")
	}

	// Should contain timestamp-like prefix
	if len(id1) < 14 {
		t.Error("ID should be at least 14 characters (timestamp)")
	}
}

func TestSpan_Duration(t *testing.T) {
	span := &Span{
		StartTime: time.Now(),
	}

	time.Sleep(10 * time.Millisecond)
	span.Finish()

	duration := span.EndTime.Sub(span.StartTime)
	if duration < 10*time.Millisecond {
		t.Error("duration should be at least 10ms")
	}
}
