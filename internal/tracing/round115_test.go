package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRound115OTLPStartupDoesNotBlock(t *testing.T) {
	start := time.Now()
	p, err := NewProvider(Config{Enabled: true, Exporter: "otlp", OTLPEndpoint: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("startup blocked %v", d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = p.Stop(ctx)
}

func TestRound115SamplerHonoursSampledParent(t *testing.T) {
	p, err := NewProvider(Config{Enabled: true, Exporter: "noop", SampleRate: 1e-12})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(context.Background())
	recording := false
	h := HTTPMiddleware(p, "http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recording = SpanFromContext(r.Context()).IsRecording()
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !recording {
		t.Error("sampled parent was dropped by ratio sampler")
	}
}
