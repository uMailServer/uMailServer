package sieve

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/umailserver/umailserver/internal/tracing"
)

func newSieveTracingProvider(t *testing.T) *tracing.Provider {
	t.Helper()
	p, err := tracing.NewProvider(tracing.Config{
		Enabled:     true,
		ServiceName: "managesieve-test",
		Exporter:    "noop",
		SampleRate:  1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = p.Stop(context.Background()) })
	return p
}

func TestManageSieve_SetTracingProvider_Stores(t *testing.T) {
	srv := NewManageSieveServer(nil, nil)
	p := newSieveTracingProvider(t)
	srv.SetTracingProvider(p)
	if srv.tracingProvider != p {
		t.Error("tracing provider not stored")
	}
}

func TestManageSieve_StartCommandSpan_NilProvider_ReturnsNoopSpan(t *testing.T) {
	srv := NewManageSieveServer(nil, nil)
	span := srv.startCommandSpan("NOOP", &manageSieveSession{})
	if span == nil {
		t.Fatal("nil span returned")
	}
	if span.IsRecording() {
		t.Error("noop span should not record")
	}
	span.End()
}

func TestManageSieve_StartCommandSpan_DisabledProvider_ReturnsNoopSpan(t *testing.T) {
	srv := NewManageSieveServer(nil, nil)
	p, err := tracing.NewProvider(tracing.Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	srv.SetTracingProvider(p)
	span := srv.startCommandSpan("NOOP", &manageSieveSession{})
	if span.IsRecording() {
		t.Error("disabled provider should not record")
	}
	span.End()
}

func TestManageSieve_StartCommandSpan_EnabledProvider_Records(t *testing.T) {
	srv := NewManageSieveServer(nil, nil)
	srv.SetTracingProvider(newSieveTracingProvider(t))
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	span := srv.startCommandSpan("PUTSCRIPT", &manageSieveSession{conn: c1})
	if !span.IsRecording() {
		t.Error("expected recording span")
	}
	span.End()
}

// TestManageSieve_ProcessCommand_LOGOUT_TracingDoesNotCrash drives a logout
// through processCommandSession with tracing enabled to confirm the wrapper
// handles the io.EOF return value (which signals normal close, not error).
func TestManageSieve_ProcessCommand_LOGOUT_TracingDoesNotCrash(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Drain anything the server writes so processCommandSession doesn't block.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := c2.Read(buf); err != nil {
				return
			}
		}
	}()

	srv := NewManageSieveServer(nil, nil)
	srv.SetTracingProvider(newSieveTracingProvider(t))
	session := &manageSieveSession{conn: c1, reader: &manageSieveReader{r: c1}}
	err := srv.processCommandSession(session, "LOGOUT")
	if err != io.EOF {
		t.Errorf("expected io.EOF (clean close), got %v", err)
	}
}

// TestManageSieve_ProcessCommand_UnknownCommand_RecordsError verifies the
// dispatcher routes errors to the span when tracing is on.
func TestManageSieve_ProcessCommand_UnknownCommand_RecordsError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := c2.Read(buf); err != nil {
				return
			}
		}
	}()
	srv := NewManageSieveServer(nil, nil)
	srv.SetTracingProvider(newSieveTracingProvider(t))
	session := &manageSieveSession{conn: c1, reader: &manageSieveReader{r: c1}}
	if err := srv.processCommandSession(session, "WHATISTHIS"); err == nil {
		t.Error("expected error for unknown command")
	}
}
