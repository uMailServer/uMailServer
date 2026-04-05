package server

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewConnectionDrainer(t *testing.T) {
	drainer := NewConnectionDrainer(5*time.Second, 10*time.Second)
	if drainer == nil {
		t.Fatal("NewConnectionDrainer returned nil")
	}
	if drainer.maxWaitTime != 5*time.Second {
		t.Errorf("expected maxWaitTime 5s, got %v", drainer.maxWaitTime)
	}
	if drainer.forceCloseAfter != 10*time.Second {
		t.Errorf("expected forceCloseAfter 10s, got %v", drainer.forceCloseAfter)
	}
}

func TestConnectionDrainer_AddRemoveConnection(t *testing.T) {
	drainer := NewConnectionDrainer(5*time.Second, 10*time.Second)

	// Add connection should succeed when not closed
	if !drainer.AddConnection() {
		t.Error("AddConnection should return true when not closed")
	}

	if drainer.ActiveConnections() != 1 {
		t.Errorf("expected 1 active connection, got %d", drainer.ActiveConnections())
	}

	// Add more connections
	drainer.AddConnection()
	drainer.AddConnection()

	if drainer.ActiveConnections() != 3 {
		t.Errorf("expected 3 active connections, got %d", drainer.ActiveConnections())
	}

	// Remove connection
	drainer.RemoveConnection()
	if drainer.ActiveConnections() != 2 {
		t.Errorf("expected 2 active connections after remove, got %d", drainer.ActiveConnections())
	}
}

func TestConnectionDrainer_AddConnectionWhenClosed(t *testing.T) {
	drainer := NewConnectionDrainer(100*time.Millisecond, 200*time.Millisecond)

	// Close the drainer
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	drainer.Close(ctx)

	// Add connection should fail when closed
	if drainer.AddConnection() {
		t.Error("AddConnection should return false when closed")
	}
}

func TestConnectionDrainer_Close(t *testing.T) {
	drainer := NewConnectionDrainer(500*time.Millisecond, time.Second)

	// Add a connection
	drainer.AddConnection()

	// Close should wait for connections to drain
	go func() {
		time.Sleep(100 * time.Millisecond)
		drainer.RemoveConnection()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := drainer.Close(ctx)
	if err != nil {
		t.Errorf("Close should succeed, got: %v", err)
	}
}

func TestConnectionDrainer_CloseTimeout(t *testing.T) {
	drainer := NewConnectionDrainer(50*time.Millisecond, 100*time.Millisecond)

	// Add a connection that won't be removed
	drainer.AddConnection()

	// Close with short timeout should fail
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := drainer.Close(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestConnectionDrainer_Wait(t *testing.T) {
	drainer := NewConnectionDrainer(time.Second, 200*time.Millisecond)

	// Add connection
	drainer.AddConnection()

	// Start goroutine to remove connection after Wait begins
	go func() {
		time.Sleep(50 * time.Millisecond)
		drainer.RemoveConnection()
	}()

	// Close with a timeout context (since connection is still active)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Close will timeout since connection is still there, that's expected
	drainer.Close(ctx)

	// Wait should block until connections are drained or timeout
	start := time.Now()
	drainer.Wait()
	elapsed := time.Since(start)

	// Should have waited at least a little bit for the connection to be removed
	if elapsed < 10*time.Millisecond {
		t.Error("Wait should have waited for connection to drain")
	}
}

func TestConnectionDrainer_Concurrent(t *testing.T) {
	drainer := NewConnectionDrainer(time.Second, time.Second)

	// Concurrent adds and removes
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if drainer.AddConnection() {
				time.Sleep(time.Millisecond)
				drainer.RemoveConnection()
			}
		}()
	}

	wg.Wait()

	if drainer.ActiveConnections() != 0 {
		t.Errorf("expected 0 connections after concurrent operations, got %d", drainer.ActiveConnections())
	}
}

func TestNewGracefulShutdown(t *testing.T) {
	gs := NewGracefulShutdown(5 * time.Second)
	if gs == nil {
		t.Fatal("NewGracefulShutdown returned nil")
	}
	if gs.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", gs.timeout)
	}
}

func TestGracefulShutdown_AddComponent(t *testing.T) {
	gs := NewGracefulShutdown(time.Second)
	component := &mockComponent{}

	gs.AddComponent("test", component)

	if len(gs.components) != 1 {
		t.Errorf("expected 1 component, got %d", len(gs.components))
	}
}

func TestGracefulShutdown_AddDrainer(t *testing.T) {
	gs := NewGracefulShutdown(time.Second)
	drainer := NewConnectionDrainer(time.Second, time.Second)

	gs.AddDrainer(drainer)

	if len(gs.drainers) != 1 {
		t.Errorf("expected 1 drainer, got %d", len(gs.drainers))
	}
}

func TestGracefulShutdown_Shutdown(t *testing.T) {
	gs := NewGracefulShutdown(2 * time.Second)
	component := &mockComponent{}

	gs.AddComponent("test", component)

	err := gs.Shutdown()
	if err != nil {
		t.Errorf("Shutdown should succeed, got: %v", err)
	}

	if !component.stopped {
		t.Error("component should have been stopped")
	}
}

func TestGracefulShutdown_ShutdownWithDrainer(t *testing.T) {
	gs := NewGracefulShutdown(2 * time.Second)
	drainer := NewConnectionDrainer(time.Second, time.Second)
	component := &mockComponent{}

	drainer.AddConnection()
	gs.AddDrainer(drainer)
	gs.AddComponent("test", component)

	// Remove connection after short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		drainer.RemoveConnection()
	}()

	err := gs.Shutdown()
	if err != nil {
		t.Errorf("Shutdown should succeed, got: %v", err)
	}
}

// mockComponent is a test implementation of ServerComponent
type mockComponent struct {
	stopped bool
	mu      sync.Mutex
}

func (m *mockComponent) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

func (m *mockComponent) ActiveConnections() int {
	return 0
}
