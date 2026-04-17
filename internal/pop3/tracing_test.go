package pop3

import (
	"context"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/tracing"
)

func newTracingProvider(t *testing.T) *tracing.Provider {
	t.Helper()
	p, err := tracing.NewProvider(tracing.Config{
		Enabled:     true,
		ServiceName: "pop3-test",
		Exporter:    "noop",
		SampleRate:  1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p
}

func TestSetTracingProvider_Stores(t *testing.T) {
	srv := NewServer("127.0.0.1:0", newMockMailstore(), nil)
	p := newTracingProvider(t)
	srv.SetTracingProvider(p)
	if srv.tracingProvider != p {
		t.Error("tracing provider not stored")
	}
}

func TestSetTracingProvider_Nil_DoesNotPanic(t *testing.T) {
	srv := NewServer("127.0.0.1:0", newMockMailstore(), nil)
	srv.SetTracingProvider(nil)
	// startCommandSpan must not panic when provider is nil.
	_, span := srv.startCommandSpan(context.Background(), "TEST", "sid", "1.1.1.1")
	if span == nil {
		t.Fatal("startCommandSpan returned nil span on nil provider")
	}
	span.End()
}

func TestStartCommandSpan_DisabledProvider_NoOp(t *testing.T) {
	srv := NewServer("127.0.0.1:0", newMockMailstore(), nil)
	p, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	srv.SetTracingProvider(p)
	_, span := srv.startCommandSpan(context.Background(), "TEST", "sid", "1.1.1.1")
	if span == nil {
		t.Fatal("expected non-nil noop span")
	}
	if span.IsRecording() {
		t.Error("disabled provider must not record")
	}
	span.End()
}

func TestStartCommandSpan_EnabledProvider_Records(t *testing.T) {
	srv := NewServer("127.0.0.1:0", newMockMailstore(), nil)
	srv.SetTracingProvider(newTracingProvider(t))
	_, span := srv.startCommandSpan(context.Background(), "PASS", "sid", "127.0.0.1")
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if !span.IsRecording() {
		t.Error("expected recording span when provider is enabled")
	}
	span.End()
}

// TestPOP3_FullSession_WithTracingEnabled verifies the server doesn't crash
// when tracing is on and a real client cycles through USER/PASS/QUIT.
func TestPOP3_FullSession_WithTracingEnabled(t *testing.T) {
	store := newMockMailstore()
	srv, addr := startTestServer(t, store, func(user, pass string) (bool, error) {
		return user == "user" && pass == "pw", nil
	})
	defer srv.Stop()

	srv.SetTracingProvider(newTracingProvider(t))

	conn, reader := dialAndRead(t, addr)
	defer conn.Close()
	if resp := sendCmd(t, conn, reader, "USER user"); resp[:3] != "+OK" {
		t.Fatalf("USER: %s", resp)
	}
	if resp := sendCmd(t, conn, reader, "PASS pw"); resp[:3] != "+OK" {
		t.Fatalf("PASS: %s", resp)
	}
	if resp := sendCmd(t, conn, reader, "STAT"); resp[:3] != "+OK" {
		t.Fatalf("STAT: %s", resp)
	}
	// Allow the server goroutine to flush spans before teardown.
	time.Sleep(20 * time.Millisecond)
}
