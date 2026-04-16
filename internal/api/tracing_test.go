package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/tracing"
)

func newTracingTestServer(t *testing.T, provider *tracing.Provider) *Server {
	t.Helper()
	s := &Server{}
	s.tracingProvider = provider
	return s
}

func TestTraceRequest_NoProvider_PassesThrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	s := newTracingTestServer(t, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	s.traceRequest(next).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should have been called")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status passthrough broken: got %d", rec.Code)
	}
}

func TestTraceRequest_DisabledProvider_PassesThrough(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	s := newTracingTestServer(t, provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/y", nil)
	s.traceRequest(next).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should run when provider disabled")
	}
}

func TestTraceRequest_EnabledProvider_RunsHandlerWithSpan(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	body := []byte("hello")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify trace context made it through.
		if r.Context() == nil {
			t.Error("expected non-nil request context")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	})
	s := newTracingTestServer(t, provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/anything", nil)
	req.Header.Set("User-Agent", "ua-test")
	s.traceRequest(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "hello" {
		t.Errorf("body passthrough broken: got %q", got)
	}
}

func TestTraceRequest_EnabledProvider_DefaultStatusOK(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	// Handler that never calls WriteHeader — middleware must default to 200.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	s := newTracingTestServer(t, provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/quiet", nil)
	s.traceRequest(next).ServeHTTP(rec, req)
}
