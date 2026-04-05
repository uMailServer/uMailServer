package tracing

import (
	"context"
	"sync"
	"time"
)

// Span represents a trace span
type Span struct {
	Name       string
	TraceID    string
	SpanID     string
	ParentID   string
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]interface{}
	Events     []Event
	Error      error
}

// Event represents an event in a span
type Event struct {
	Name      string
	Timestamp time.Time
	Attributes map[string]interface{}
}

// SpanContext holds span identification
type SpanContext struct {
	TraceID string
	SpanID  string
}

// Tracer creates spans
type Tracer struct {
	serviceName string
	exporter    Exporter
}

// NewTracer creates a new tracer
func NewTracer(serviceName string, exporter Exporter) *Tracer {
	if exporter == nil {
		exporter = &NoopExporter{}
	}
	return &Tracer{
		serviceName: serviceName,
		exporter:    exporter,
	}
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	span := &Span{
		Name:       name,
		TraceID:    generateID(),
		SpanID:     generateID(),
		StartTime:  time.Now(),
		Attributes: make(map[string]interface{}),
	}

	// Check for parent span
	if parent := SpanFromContext(ctx); parent != nil {
		span.TraceID = parent.TraceID
		span.ParentID = parent.SpanID
	}

	newCtx := context.WithValue(ctx, spanKey, span)
	return newCtx, span
}

// Finish ends a span and exports it
func (s *Span) Finish() {
	s.EndTime = time.Now()
	// Export would happen here
}

// SetAttribute sets an attribute on the span
func (s *Span) SetAttribute(key string, value interface{}) {
	s.Attributes[key] = value
}

// AddEvent adds an event to the span
func (s *Span) AddEvent(name string, attrs map[string]interface{}) {
	s.Events = append(s.Events, Event{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// RecordError records an error on the span
func (s *Span) RecordError(err error) {
	s.Error = err
}

// Exporter exports spans
type Exporter interface {
	Export(span *Span)
}

// NoopExporter discards spans
type NoopExporter struct{}

func (n *NoopExporter) Export(span *Span) {}

// Context key for storing span
type contextKey string

const spanKey contextKey = "tracing.span"

// SpanFromContext retrieves span from context
func SpanFromContext(ctx context.Context) *Span {
	if s, ok := ctx.Value(spanKey).(*Span); ok {
		return s
	}
	return nil
}

// ContextWithSpan adds span to context
func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanKey, span)
}

// Simple ID generator
var idCounter uint64
var idMutex sync.Mutex

func generateID() string {
	idMutex.Lock()
	defer idMutex.Unlock()
	idCounter++
	return time.Now().Format("20060102150405") + string(rune(idCounter))
}

// Provider manages tracing
type Provider struct {
	tracer    *Tracer
	enabled   bool
	stopFunc  func()
}

// Config holds tracing configuration
type Config struct {
	Enabled     bool
	ServiceName string
}

// NewProvider creates a new tracing provider
func NewProvider(config Config) *Provider {
	if !config.Enabled {
		return &Provider{enabled: false}
	}

	return &Provider{
		tracer:  NewTracer(config.ServiceName, nil),
		enabled: true,
	}
}

// Stop shuts down the provider
func (p *Provider) Stop() {
	if p.stopFunc != nil {
		p.stopFunc()
	}
}

// StartSpan starts a new span
func (p *Provider) StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	if !p.enabled {
		return ctx, nil
	}
	return p.tracer.StartSpan(ctx, name)
}
