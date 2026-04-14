package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func TestNewProvider_Disabled(t *testing.T) {
	provider, err := NewProvider(Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if provider.IsEnabled() {
		t.Error("provider should be disabled")
	}
}

func TestNewProvider_Enabled(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test-service",
		Exporter:    "noop",
		Environment: "test",
		SampleRate:  1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	if !provider.IsEnabled() {
		t.Error("provider should be enabled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	_ = provider.Stop(ctx)
}

func TestProvider_StartSpan(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	if !span.IsRecording() {
		t.Error("span should be recording")
	}

	// Verify span is in context
	if SpanFromContext(ctx) != span {
		t.Error("span should be in context")
	}
}

func TestProvider_StartSpan_Disabled(t *testing.T) {
	provider, err := NewProvider(Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	ctx := context.Background()
	newCtx, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	// When disabled, should return a non-recording span
	if span.IsRecording() {
		t.Error("span should not be recording when disabled")
	}

	// Context should remain unchanged
	if newCtx != ctx {
		t.Error("context should remain unchanged when disabled")
	}
}

func TestProvider_StartSpanWithKind(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	ctx, span := provider.StartSpanWithKind(ctx, "test-span", SpanKindServer,
		attribute.String("key", "value"),
	)
	defer span.End()

	if !span.IsRecording() {
		t.Error("span should be recording")
	}
}

func TestSpanFromContext_NoSpan(t *testing.T) {
	ctx := context.Background()
	span := SpanFromContext(ctx)

	// Should return a non-recording span
	if span.IsRecording() {
		t.Error("should return non-recording span when no span in context")
	}
}

func TestContextWithSpan(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	newCtx := ContextWithSpan(ctx, span)
	retrieved := SpanFromContext(newCtx)

	if retrieved != span {
		t.Error("retrieved span should match the one set")
	}
}

func TestSetAttributes(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetAttributes(span,
		attribute.String("string-key", "value"),
		attribute.Int("int-key", 42),
		attribute.Bool("bool-key", true),
	)

	// Attributes are set - OpenTelemetry handles them internally
	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestSetStringAttribute(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetStringAttribute(span, "key", "value")

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestSetIntAttribute(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetIntAttribute(span, "key", 42)

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestSetInt64Attribute(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetInt64Attribute(span, "key", 9223372036854775807)

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestSetBoolAttribute(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetBoolAttribute(span, "key", true)

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestRecordError(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	testErr := errors.New("test error")
	RecordError(span, testErr)

	// Error is recorded - OpenTelemetry handles it internally
	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestRecordError_Nil(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	// Should not panic with nil error
	RecordError(span, nil)
}

func TestAddEvent(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	AddEvent(span, "test-event",
		attribute.String("event-key", "event-value"),
	)

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestSetStatus(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	ctx := context.Background()
	_, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	SetStatus(span, StatusError, "something went wrong")

	if !span.IsRecording() {
		t.Error("span should still be recording")
	}
}

func TestStatusConstants(t *testing.T) {
	if StatusUnset != codes.Unset {
		t.Error("StatusUnset should equal codes.Unset")
	}
	if StatusError != codes.Error {
		t.Error("StatusError should equal codes.Error")
	}
	if StatusOk != codes.Ok {
		t.Error("StatusOk should equal codes.Ok")
	}
}

func TestSpanKindConstants(t *testing.T) {
	if SpanKindUnspecified != trace.SpanKindUnspecified {
		t.Error("SpanKindUnspecified mismatch")
	}
	if SpanKindInternal != trace.SpanKindInternal {
		t.Error("SpanKindInternal mismatch")
	}
	if SpanKindServer != trace.SpanKindServer {
		t.Error("SpanKindServer mismatch")
	}
	if SpanKindClient != trace.SpanKindClient {
		t.Error("SpanKindClient mismatch")
	}
	if SpanKindProducer != trace.SpanKindProducer {
		t.Error("SpanKindProducer mismatch")
	}
	if SpanKindConsumer != trace.SpanKindConsumer {
		t.Error("SpanKindConsumer mismatch")
	}
}

func TestHTTPHeaderCarrier(t *testing.T) {
	headers := make(map[string][]string)
	carrier := HTTPHeaderCarrier(headers)

	// Test Set and Get
	carrier.Set("X-Test-Header", "test-value")
	if carrier.Get("X-Test-Header") != "test-value" {
		t.Error("carrier should return the set value")
	}

	// Test non-existent key
	if carrier.Get("X-NonExistent") != "" {
		t.Error("carrier should return empty for non-existent key")
	}

	// Test Keys
	keys := carrier.Keys()
	if len(keys) != 1 || keys[0] != "X-Test-Header" {
		t.Errorf("expected keys to contain X-Test-Header, got %v", keys)
	}
}

func TestProvider_InjectExtract(t *testing.T) {
	provider, err := NewProvider(Config{
		Enabled:     true,
		ServiceName: "test",
		Exporter:    "noop",
	})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	// Create a span
	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "test-span")
	defer span.End()

	// Inject into carrier
	headers := make(map[string][]string)
	carrier := HTTPHeaderCarrier(headers)
	provider.Inject(ctx, carrier)

	// Extract from carrier
	extractedCtx := provider.Extract(context.Background(), carrier)
	extractedSpan := SpanFromContext(extractedCtx)

	if !extractedSpan.SpanContext().IsValid() {
		t.Error("extracted span should have valid span context")
	}
}

func TestProvider_InjectExtract_Disabled(t *testing.T) {
	provider, err := NewProvider(Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	// Create a context (no span)
	ctx := context.Background()

	// Inject into carrier (should not panic)
	headers := make(map[string][]string)
	carrier := HTTPHeaderCarrier(headers)
	provider.Inject(ctx, carrier)

	// Extract from carrier (should return original context)
	extractedCtx := provider.Extract(ctx, carrier)
	if extractedCtx != ctx {
		t.Error("extract should return original context when disabled")
	}
}

func TestCreateExporter(t *testing.T) {
	// Test noop exporter
	exp, err := createExporter(Config{Exporter: "noop"})
	if err != nil {
		t.Fatalf("createExporter noop failed: %v", err)
	}
	if exp == nil {
		t.Error("noop exporter should not be nil")
	}

	// Test stdout exporter
	exp, err = createExporter(Config{Exporter: "stdout"})
	if err != nil {
		t.Fatalf("createExporter stdout failed: %v", err)
	}
	if exp == nil {
		t.Error("stdout exporter should not be nil")
	}

	// Test unknown exporter
	_, err = createExporter(Config{Exporter: "unknown"})
	if err == nil {
		t.Error("createExporter should fail for unknown exporter type")
	}
}

func TestCreateResource(t *testing.T) {
	config := Config{
		ServiceName: "test-service",
		Environment: "test",
		Attributes: map[string]string{
			"custom.key": "custom-value",
		},
	}

	res, err := createResource(config)
	if err != nil {
		t.Fatalf("createResource failed: %v", err)
	}
	if res == nil {
		t.Error("resource should not be nil")
	}
}

func TestDefaultConfigValues(t *testing.T) {
	config := Config{
		Enabled: true,
		// Leave other fields empty to test defaults
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("NewProvider with defaults failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
		_ = provider.Stop(ctx)
	}()

	if !provider.IsEnabled() {
		t.Error("provider should be enabled")
	}
}

// defaultTimeout for test cleanup
const defaultTimeout = 5
