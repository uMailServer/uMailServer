package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds tracing configuration
type Config struct {
	Enabled      bool              `yaml:"enabled"`
	ServiceName  string            `yaml:"service_name"`
	Exporter     string            `yaml:"exporter"`      // "otlp", "stdout", or "noop"
	OTLPEndpoint string            `yaml:"otlp_endpoint"` // OTLP collector endpoint (e.g., "localhost:4317")
	Environment  string            `yaml:"environment"`   // "production", "staging", "development"
	Attributes   map[string]string `yaml:"attributes"`    // Additional resource attributes
	SampleRate   float64           `yaml:"sample_rate"`   // 0.0 to 1.0
}

// Provider manages OpenTelemetry tracing
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
	propagator     propagation.TextMapPropagator
	enabled        bool
	stopFunc       func(context.Context) error
	stopOnce       sync.Once
	stopErr        error
}

// NewProvider creates a new tracing provider with OpenTelemetry
func NewProvider(config Config) (*Provider, error) {
	if !config.Enabled {
		return &Provider{enabled: false}, nil
	}

	if config.ServiceName == "" {
		config.ServiceName = "umailserver"
	}
	if config.Exporter == "" {
		config.Exporter = "noop"
	}
	if config.Environment == "" {
		config.Environment = "production"
	}
	if config.SampleRate <= 0 {
		config.SampleRate = 1.0
	}

	// Create exporter based on configuration
	exp, err := createExporter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource with service information
	res, err := createResource(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create sampler based on sample rate
	// F5977: honour the upstream sampling decision (W3C traceparent extracted
	// by HTTPMiddleware) and only ratio-sample root spans, so a sampled
	// distributed trace is not cut into fragments.
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.SampleRate))

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set as global tracer provider
	otel.SetTracerProvider(tp)

	// Create propagator for distributed context propagation
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	return &Provider{
		tracerProvider: tp,
		tracer:         tp.Tracer(config.ServiceName),
		propagator:     propagator,
		enabled:        true,
		stopFunc:       tp.Shutdown,
	}, nil
}

// createExporter creates a trace exporter based on configuration
func createExporter(config Config) (sdktrace.SpanExporter, error) {
	switch config.Exporter {
	case "otlp":
		if config.OTLPEndpoint == "" {
			config.OTLPEndpoint = "localhost:4317"
		}
		// grpc.NewClient is lazy: the channel starts Idle and only dials on
		// first use. Waiting for a state change here never fired on an idle
		// channel, so startup failed after the timeout even with a healthy
		// collector (F5976). The batcher exports asynchronously and retries,
		// so an unreachable collector must not block or fail startup.
		conn, err := grpc.NewClient(config.OTLPEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to OTLP endpoint: %w", err)
		}
		exp, err := otlptracegrpc.New(context.Background(), otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return &connClosingExporter{SpanExporter: exp, conn: conn}, nil

	case "stdout":
		return stdouttrace.New(stdouttrace.WithWriter(os.Stdout))

	case "noop":
		return &noopExporter{}, nil

	default:
		return nil, fmt.Errorf("unknown exporter type: %s", config.Exporter)
	}
}

// connClosingExporter closes the gRPC connection on shutdown; WithGRPCConn
// leaves ownership of the connection with the caller (F5976).
type connClosingExporter struct {
	sdktrace.SpanExporter
	conn *grpc.ClientConn
}

func (e *connClosingExporter) Shutdown(ctx context.Context) error {
	err := e.SpanExporter.Shutdown(ctx)
	if cerr := e.conn.Close(); err == nil {
		err = cerr
	}
	return err
}

// createResource creates a resource with service information
func createResource(config Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion("1.0.0"),
		semconv.DeploymentEnvironment(config.Environment),
	}

	// Add custom attributes
	for k, v := range config.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.NewWithAttributes(semconv.SchemaURL, attrs...), nil
}

// Stop shuts down the tracing provider
func (p *Provider) Stop(ctx context.Context) error {
	if p == nil || !p.enabled || p.stopFunc == nil {
		return nil
	}
	// Idempotent (F6298): a second Stop re-ran the SDK shutdown and returned
	// its "already shutdown" error.
	p.stopOnce.Do(func() { p.stopErr = p.stopFunc(ctx) })
	return p.stopErr
}

// StartSpan starts a new span with the given name and options
func (p *Provider) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if p == nil || !p.enabled {
		// Return a no-op span, never the caller's current one: callers
		// defer span.End(), which would end the parent span early (F5927).
		return ctx, trace.SpanFromContext(context.Background())
	}
	return p.tracer.Start(ctx, name, opts...)
}

// SpanFromContext retrieves the current span from context
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan creates a new context with the given span
func ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}

// Inject propagates the span context into carrier headers
func (p *Provider) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	if p != nil && p.enabled && p.propagator != nil {
		p.propagator.Inject(ctx, carrier)
	}
}

// Extract extracts span context from carrier headers
func (p *Provider) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	if p == nil || !p.enabled || p.propagator == nil {
		return ctx
	}
	return p.propagator.Extract(ctx, carrier)
}

// IsEnabled returns whether tracing is enabled
func (p *Provider) IsEnabled() bool {
	return p != nil && p.enabled
}

// noopExporter is a no-op span exporter
type noopExporter struct{}

func (n *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (n *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}

// Helper functions for common span operations

// SetAttributes sets multiple attributes on a span
func SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// SetStringAttribute sets a string attribute on a span
func SetStringAttribute(span trace.Span, key string, value string) {
	if span.IsRecording() {
		span.SetAttributes(attribute.String(key, value))
	}
}

// SetIntAttribute sets an int attribute on a span
func SetIntAttribute(span trace.Span, key string, value int) {
	if span.IsRecording() {
		span.SetAttributes(attribute.Int(key, value))
	}
}

// SetInt64Attribute sets an int64 attribute on a span
func SetInt64Attribute(span trace.Span, key string, value int64) {
	if span.IsRecording() {
		span.SetAttributes(attribute.Int64(key, value))
	}
}

// SetBoolAttribute sets a bool attribute on a span
func SetBoolAttribute(span trace.Span, key string, value bool) {
	if span.IsRecording() {
		span.SetAttributes(attribute.Bool(key, value))
	}
}

// SetFloatAttribute sets a float64 attribute on a span
func SetFloatAttribute(span trace.Span, key string, value float64) {
	if span.IsRecording() {
		span.SetAttributes(attribute.Float64(key, value))
	}
}

// RecordError records an error on a span
func RecordError(span trace.Span, err error, opts ...trace.EventOption) {
	if span.IsRecording() && err != nil {
		span.RecordError(err, opts...)
	}
}

// AddEvent adds a named event to a span
func AddEvent(span trace.Span, name string, attrs ...attribute.KeyValue) {
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// SetStatus sets the span status
func SetStatus(span trace.Span, code codes.Code, description string) {
	if span.IsRecording() {
		span.SetStatus(code, description)
	}
}

// Status codes for span status
const (
	StatusUnset = codes.Unset
	StatusError = codes.Error
	StatusOk    = codes.Ok
)

// SpanKind represents the kind of span
type SpanKind = trace.SpanKind

// Span kinds
const (
	SpanKindUnspecified = trace.SpanKindUnspecified
	SpanKindInternal    = trace.SpanKindInternal
	SpanKindServer      = trace.SpanKindServer
	SpanKindClient      = trace.SpanKindClient
	SpanKindProducer    = trace.SpanKindProducer
	SpanKindConsumer    = trace.SpanKindConsumer
)

// Carrier adapters for different protocols

// HTTPHeaderCarrier adapts http.Header for propagation
func HTTPHeaderCarrier(headers map[string][]string) propagation.TextMapCarrier {
	return &headerCarrier{headers: headers}
}

type headerCarrier struct {
	headers map[string][]string
}

func (c *headerCarrier) Get(key string) string {
	if vals, ok := c.headers[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	for name, vals := range c.headers {
		if strings.EqualFold(name, key) && len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func (c *headerCarrier) Set(key string, value string) {
	c.headers[key] = []string{value}
}

func (c *headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for k := range c.headers {
		keys = append(keys, k)
	}
	return keys
}

// StartSpanWithKind starts a span with a specific kind
func (p *Provider) StartSpanWithKind(ctx context.Context, name string, kind SpanKind, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithSpanKind(kind)}
	if len(attrs) > 0 {
		opts = append(opts, trace.WithAttributes(attrs...))
	}
	return p.StartSpan(ctx, name, opts...)
}
