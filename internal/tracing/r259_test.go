package tracing

import "testing"

// F7360: the provider itself refuses plaintext OTLP to non-loopback hosts
// unless Insecure is set (defense in depth for non-config callers).
func TestF7360_ProviderRefusesPlaintextRemote(t *testing.T) {
	for _, ep := range []string{"collector.example.com:4317", "http://10.1.2.3:4317"} {
		if _, err := NewProvider(Config{Enabled: true, Exporter: "otlp", OTLPEndpoint: ep, SampleRate: 1}); err == nil {
			t.Errorf("%q: want error", ep)
		}
	}
	for _, c := range []Config{
		{Enabled: true, Exporter: "otlp", OTLPEndpoint: "collector.example.com:4317", Insecure: true, SampleRate: 1},
		{Enabled: true, Exporter: "otlp", OTLPEndpoint: "https://collector.example.com:4317", SampleRate: 1},
		{Enabled: true, Exporter: "otlp", OTLPEndpoint: "localhost:4317", SampleRate: 1},
		{Enabled: true, Exporter: "otlp", OTLPEndpoint: "[::1]:4317", SampleRate: 1},
	} {
		p, err := NewProvider(c)
		if err != nil {
			t.Errorf("%+v: %v", c, err)
			continue
		}
		_ = p.Stop(t.Context())
	}
}
