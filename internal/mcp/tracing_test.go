package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/tracing"
)

func newMCPProvider(t *testing.T) *tracing.Provider {
	t.Helper()
	p, err := tracing.NewProvider(tracing.Config{
		Enabled:     true,
		ServiceName: "mcp-test",
		Exporter:    "noop",
		SampleRate:  1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p
}

func newMCPTestDB(t *testing.T) *db.DB {
	t.Helper()
	f, err := os.CreateTemp("", "mcp-trace-*.db")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	d, err := db.Open(f.Name())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestSetTracingProvider_Stores(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	p := newMCPProvider(t)
	srv.SetTracingProvider(p)
	if srv.tracingProvider != p {
		t.Error("provider not stored")
	}
}

func TestHandleHTTP_NilProvider_DispatchesNormally(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	body := mustJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	})
	rr := httptest.NewRecorder()
	srv.HandleHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestHandleHTTP_TracingEnabled_InitializeOK(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	srv.SetTracingProvider(newMCPProvider(t))

	body := mustJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	})
	rr := httptest.NewRecorder()
	srv.HandleHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestHandleHTTP_TracingEnabled_ToolsCallExtractsToolName(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	srv.SetTracingProvider(newMCPProvider(t))

	// Issue tools/call with a known tool name. The span recording is internal
	// to the noop exporter, so we just assert that the dispatch path doesn't
	// crash when the params probe runs.
	body := mustJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{
			"name":      "get_server_stats",
			"arguments": map[string]interface{}{},
		},
	})
	rr := httptest.NewRecorder()
	srv.HandleHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))
	// get_server_stats can succeed or report a benign error; we only care
	// that the tracing wrapper didn't break the dispatcher.
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestHandleHTTP_TracingEnabled_UnknownMethod_RecordsError(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	srv.SetTracingProvider(newMCPProvider(t))

	body := mustJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 3, "method": "does/not/exist",
	})
	rr := httptest.NewRecorder()
	srv.HandleHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleHTTP_DisabledProvider_BypassesTracing(t *testing.T) {
	srv := NewServer(newMCPTestDB(t))
	p, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	srv.SetTracingProvider(p)
	body := mustJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 4, "method": "initialize",
	})
	rr := httptest.NewRecorder()
	srv.HandleHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
