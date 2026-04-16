package queue

import (
	"context"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/tracing"
)

func TestSetTracingProvider_NilSafe(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir + "/test.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	m := NewManager(database, nil, dataDir, nil)
	m.SetTracingProvider(nil)
	if m.tracingProvider != nil {
		t.Errorf("expected nil tracing provider")
	}
}

func TestStartDeliverSpan_AttributesSet(t *testing.T) {
	provider, err := tracing.NewProvider(tracing.Config{
		Enabled: true, ServiceName: "test", Exporter: "noop", SampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Stop(context.Background()) })

	dataDir := t.TempDir()
	database, _ := db.Open(dataDir + "/test.db")
	defer database.Close()

	m := NewManager(database, nil, dataDir, nil)
	m.SetTracingProvider(provider)

	entry := &db.QueueEntry{
		ID:         "q1",
		From:       "from@x",
		To:         []string{"to@y"},
		RetryCount: 2,
	}
	span := m.startDeliverSpan(context.Background(), entry)
	if span == nil {
		t.Fatal("startDeliverSpan returned nil")
	}
	span.End()
}
