package tracing

import (
	"context"
	"fmt"
	"os"
	"time"

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
	sampler := sdktrace.TraceIDRatioBased(config.SampleRate)

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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(ctx, config.OTLPEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to OTLP endpoint: %w", err)
		}
		return otlptracegrpc.New(context.Background(), otlptracegrpc.WithGRPCConn(conn))

	case "stdout":
		return stdouttrace.New(stdouttrace.WithWriter(os.Stdout))

	case "noop":
		return &noopExporter{}, nil

	default:
		return nil, fmt.Errorf("unknown exporter type: %s", config.Exporter)
	}
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
	if !p.enabled || p.stopFunc == nil {
		return nil
	}
	return p.stopFunc(ctx)
}

// StartSpan starts a new span with the given name and options
func (p *Provider) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if !p.enabled {
		return ctx, trace.SpanFromContext(ctx)
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
	if p.enabled && p.propagator != nil {
		p.propagator.Inject(ctx, carrier)
	}
}

// Extract extracts span context from carrier headers
func (p *Provider) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	if !p.enabled || p.propagator == nil {
		return ctx
	}
	return p.propagator.Extract(ctx, carrier)
}

// IsEnabled returns whether tracing is enabled
func (p *Provider) IsEnabled() bool {
	return p.enabled
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
