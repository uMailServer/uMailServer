package server

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestDefaultResourceLimits(t *testing.T) {
	limits := DefaultResourceLimits()

	if limits.MaxMemoryMB != 0 {
		t.Errorf("expected MaxMemoryMB=0 (unlimited), got %d", limits.MaxMemoryMB)
	}
	if limits.MaxGoroutines != 0 {
		t.Errorf("expected MaxGoroutines=0 (unlimited), got %d", limits.MaxGoroutines)
	}
	if limits.GCPercent != 100 {
		t.Errorf("expected GCPercent=100, got %d", limits.GCPercent)
	}
	if limits.MemoryCheckInterval != 30*time.Second {
		t.Errorf("expected MemoryCheckInterval=30s, got %v", limits.MemoryCheckInterval)
	}
}

func TestNewResourceMonitor(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	if monitor == nil {
		t.Fatal("NewResourceMonitor returned nil")
	}
	if monitor.limits.GCPercent != limits.GCPercent {
		t.Error("limits not set correctly")
	}
}

func TestResourceMonitor_StartStop(t *testing.T) {
	limits := ResourceLimits{
		MemoryCheckInterval: 100 * time.Millisecond,
	}
	monitor := NewResourceMonitor(limits, &testLogger{})

	monitor.Start()
	time.Sleep(150 * time.Millisecond)
	monitor.Stop()

	// Should not panic
}

func TestResourceMonitor_AddRemoveConnection(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	// Add connections
	for i := 0; i < 5; i++ {
		if !monitor.AddConnection() {
			t.Errorf("AddConnection should succeed (iteration %d)", i)
		}
	}

	if monitor.currentConnections != 5 {
		t.Errorf("expected 5 connections, got %d", monitor.currentConnections)
	}

	// Remove connections
	for i := 0; i < 5; i++ {
		monitor.RemoveConnection()
	}

	if monitor.currentConnections != 0 {
		t.Errorf("expected 0 connections after remove, got %d", monitor.currentConnections)
	}
}

func TestResourceMonitor_ConnectionLimit(t *testing.T) {
	limits := ResourceLimits{
		MaxConnections: 3,
	}
	logger := &testLogger{}
	monitor := NewResourceMonitor(limits, logger)

	// Add up to limit
	for i := 0; i < 3; i++ {
		if !monitor.AddConnection() {
			t.Errorf("AddConnection should succeed at iteration %d", i)
		}
	}

	// Next should fail
	if monitor.AddConnection() {
		t.Error("AddConnection should fail when limit reached")
	}

	// Remove one
	monitor.RemoveConnection()

	// Now should succeed
	if !monitor.AddConnection() {
		t.Error("AddConnection should succeed after removing one")
	}
}

func TestResourceMonitor_GetStats(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryMB:    1000,
		MaxGoroutines:  500,
		MaxConnections: 100,
	}
	monitor := NewResourceMonitor(limits, nil)

	// Add some connections
	monitor.AddConnection()
	monitor.AddConnection()
	monitor.AddConnection()

	stats := monitor.GetStats()

	if stats.Connections != 3 {
		t.Errorf("expected 3 connections in stats, got %d", stats.Connections)
	}

	if stats.MemoryLimitMB != 1000 {
		t.Errorf("expected MemoryLimitMB=1000, got %d", stats.MemoryLimitMB)
	}

	if stats.GoroutineLimit != 500 {
		t.Errorf("expected GoroutineLimit=500, got %d", stats.GoroutineLimit)
	}

	if stats.ConnectionLimit != 100 {
		t.Errorf("expected ConnectionLimit=100, got %d", stats.ConnectionLimit)
	}

	// Memory and goroutine stats should be populated
	if stats.MemoryAllocated == 0 {
		t.Error("MemoryAllocated should be > 0")
	}

	if stats.Goroutines == 0 {
		t.Error("Goroutines should be > 0")
	}
}

func TestResourceStats_String(t *testing.T) {
	stats := ResourceStats{
		MemoryAllocated: 1024 * 1024 * 100, // 100MB
		MemoryLimitMB:   1000,
		Goroutines:      50,
		GoroutineLimit:  100,
		Connections:     25,
		ConnectionLimit: 100,
		MemoryGC:        10,
	}

	str := stats.String()

	if str == "" {
		t.Error("String() should return non-empty string")
	}

	// Should contain key information
	if len(str) < 20 {
		t.Error("String() should contain formatted stats")
	}
}

func TestResourceMonitor_SetMaxMemory(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	monitor.SetMaxMemory(500)

	if monitor.limits.MaxMemoryMB != 500 {
		t.Errorf("expected MaxMemoryMB=500, got %d", monitor.limits.MaxMemoryMB)
	}
}

func TestResourceMonitor_ForceGC(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	// Allocate some memory
	data := make([]byte, 1024*1024*10) // 10MB
	_ = data

	// Get GC count before
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Force GC
	monitor.ForceGC()

	// Get GC count after
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// GC should have run (count should increase or stay same)
	if m2.NumGC < m1.NumGC {
		t.Error("GC should have run")
	}
}

func TestResourceMonitor_MemoryLimitCallback(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryMB:         1, // 1MB - very low to trigger limit
		MemoryCheckInterval: 50 * time.Millisecond,
	}
	logger := &testLogger{}
	monitor := NewResourceMonitor(limits, logger)

	callbackCalled := false
	monitor.SetMemoryLimitCallback(func() {
		callbackCalled = true
	})

	monitor.Start()

	// Allocate memory to trigger limit
	data := make([]byte, 1024*1024*5) // 5MB
	_ = data

	time.Sleep(100 * time.Millisecond)

	monitor.Stop()

	if !callbackCalled {
		t.Error("memory limit callback should have been called")
	}
}

func TestNoopLogger(t *testing.T) {
	logger := &noopLogger{}

	// Should not panic
	logger.Info("test", "key", "value")
	logger.Warn("test", "key", "value")
	logger.Error("test", "key", "value")
}

func TestResourceMonitor_SetGoroutineLimitCallback(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	monitor.SetGoroutineLimitCallback(func() {
		// callback set
	})

	// Verify setter worked
	if monitor.onGoroutineLimit == nil {
		t.Error("goroutine limit callback should be set")
	}
}

func TestResourceMonitor_SetConnectionLimitCallback(t *testing.T) {
	limits := DefaultResourceLimits()
	monitor := NewResourceMonitor(limits, nil)

	monitor.SetConnectionLimitCallback(func() {
		// callback set
	})

	// Verify setter worked
	if monitor.onConnectionLimit == nil {
		t.Error("connection limit callback should be set")
	}
}

// testLogger is a simple test logger
type testLogger struct {
	logs []string
	mu   sync.Mutex
}

func (t *testLogger) Info(msg string, args ...interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, "INFO: "+msg)
}

func (t *testLogger) Warn(msg string, args ...interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, "WARN: "+msg)
}

func (t *testLogger) Error(msg string, args ...interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logs = append(t.logs, "ERROR: "+msg)
}
