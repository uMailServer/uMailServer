package jmap

import (
	"context"
	"testing"

	"github.com/umailserver/umailserver/internal/tracing"
)

func TestProcessMethodCall_NoTracing_ReturnsResponse(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	resp := server.processMethodCall("user@example.com", MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{"accountId": "user@example.com"},
		ID:   "c1",
	})
	if resp.Name != "Mailbox/get" {
		t.Errorf("got %q want Mailbox/get", resp.Name)
	}
}

func TestProcessMethodCall_WithDisabledProvider_StillRuns(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	provider, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	server.SetTracingProvider(provider)

	resp := server.processMethodCall("user@example.com", MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{"accountId": "user@example.com"},
		ID:   "c1",
	})
	if resp.Name != "Mailbox/get" {
		t.Errorf("got %q want Mailbox/get", resp.Name)
	}
}

func TestProcessMethodCall_TracingEnabled_DispatchesCorrectly(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })
	server.SetTracingProvider(provider)

	resp := server.processMethodCall("user@example.com", MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{"accountId": "user@example.com"},
		ID:   "c1",
	})
	if resp.Name != "Mailbox/get" {
		t.Errorf("got %q want Mailbox/get", resp.Name)
	}
}

func TestProcessMethodCall_TracingEnabled_RecordsErrorOnAccountMismatch(t *testing.T) {
	server, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })
	server.SetTracingProvider(provider)

	// accountId mismatches authenticated user — handler returns accountNotFound.
	resp := server.processMethodCall("user@example.com", MethodCall{
		Name: "Mailbox/get",
		Args: map[string]interface{}{"accountId": "other@example.com"},
		ID:   "c1",
	})
	if resp.Args["type"] != "accountNotFound" {
		t.Errorf("expected accountNotFound, got %+v", resp.Args)
	}
}
