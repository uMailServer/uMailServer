package tracing

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel/propagation"
)

// statusCaptureWriter wraps an http.ResponseWriter to capture the status code
// for tracing. Defaults to 200 when WriteHeader is never called.
type statusCaptureWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCaptureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush passes through to the wrapped writer when it supports flushing
// (needed for SSE / streaming responses).
func (w *statusCaptureWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware returns an http.Handler that wraps next in an http.<METHOD>
// server-kind span when the provider is enabled. W3C trace context is
// extracted from the incoming request headers so the span joins any upstream
// trace. The span receives http.method/target/host/scheme/user_agent/status_code
// attributes; 4xx responses set client error status, 5xx set server error
// status. When the provider is nil or disabled the middleware is a passthrough.
//
// The component label is used to name the span ("<component>.<METHOD>"). Use
// "http" for the generic API surface, or a more specific tag like "caldav" /
// "carddav" / "jmap" so traces can be filtered per service.
func HTTPMiddleware(provider *Provider, component string, next http.Handler) http.Handler {
	if component == "" {
		component = "http"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if provider == nil || !provider.IsEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		ctx := provider.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := provider.StartSpanWithKind(ctx, component+"."+r.Method, SpanKindServer)
		defer span.End()

		SetStringAttribute(span, "http.method", r.Method)
		SetStringAttribute(span, "http.target", r.URL.Path)
		SetStringAttribute(span, "http.host", r.Host)
		if scheme := r.URL.Scheme; scheme != "" {
			SetStringAttribute(span, "http.scheme", scheme)
		}
		if ua := r.UserAgent(); ua != "" {
			SetStringAttribute(span, "http.user_agent", ua)
		}

		rec := &statusCaptureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))

		SetIntAttribute(span, "http.status_code", rec.status)
		switch {
		case rec.status >= 500:
			SetStatus(span, StatusError, "server error: "+strconv.Itoa(rec.status))
		case rec.status >= 400:
			SetStatus(span, StatusError, "client error: "+strconv.Itoa(rec.status))
		default:
			SetStatus(span, StatusOk, "")
		}
	})
}
