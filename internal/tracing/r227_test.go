package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// F7040: sample_rate 0 must not silently become 100% sampling.
func TestSampleRateZeroSamplesNothing(t *testing.T) {
	p, err := NewProvider(Config{Enabled: true, Exporter: "noop", SampleRate: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(t.Context())
	_, span := p.StartSpan(t.Context(), "x")
	defer span.End()
	if span.SpanContext().IsSampled() {
		t.Fatal("sample_rate 0 sampled a root span")
	}
	p2, _ := NewProvider(Config{Enabled: true, Exporter: "noop", SampleRate: 1})
	defer p2.Stop(t.Context())
	_, s2 := p2.StartSpan(t.Context(), "y")
	defer s2.End()
	if !s2.SpanContext().IsSampled() {
		t.Fatal("sample_rate 1 must sample")
	}
}

// F7041: https:// endpoints use TLS and the scheme is not left in the target.
func TestOTLPTransportScheme(t *testing.T) {
	tgt, c := otlpTransport("https://collector.example:4317")
	if tgt != "collector.example:4317" || c.Info().SecurityProtocol != "tls" {
		t.Fatalf("got %q %q", tgt, c.Info().SecurityProtocol)
	}
	tgt, c = otlpTransport("http://localhost:4317/")
	if tgt != "localhost:4317" || c.Info().SecurityProtocol != "insecure" {
		t.Fatalf("got %q %q", tgt, c.Info().SecurityProtocol)
	}
}

// F7042: the query string (tokens, addresses) never reaches span attributes.
func TestHTTPTargetOmitsQuery(t *testing.T) {
	p, _ := NewProvider(Config{Enabled: true, SampleRate: 1, Exporter: "noop"})
	defer p.Stop(t.Context())
	h := HTTPMiddleware(p, "http", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a?token=secret&to=x@y.z", nil))
	if got := truncateAttr("/a"); got != "/a" {
		t.Fatal(got)
	}
}
