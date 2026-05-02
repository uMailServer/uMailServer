package server

import (
	"fmt"
	"math"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

// ResourceLimits holds configurable resource limits
type ResourceLimits struct {
	MaxMemoryMB         int64         // Maximum memory usage in MB (0 = unlimited)
	MaxGoroutines       int           // Maximum number of goroutines (0 = unlimited)
	MaxConnections      int           // Maximum total connections (0 = unlimited)
	GCPercent           int           // GC target percentage (default 100)
	MemoryCheckInterval time.Duration // How often to check memory
}

// DefaultResourceLimits returns sensible defaults
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxMemoryMB:         0, // Unlimited by default
		MaxGoroutines:       0, // Unlimited by default
		MaxConnections:      0, // Unlimited by default
		GCPercent:           100,
		MemoryCheckInterval: 30 * time.Second,
	}
}

// ResourceMonitor monitors and enforces resource limits
type ResourceMonitor struct {
	limits   ResourceLimits
	logger   Logger
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// Current values
	currentMemory      int64
	currentGoroutines  int
	currentConnections int64

	// Callbacks
	onMemoryLimit     func()
	onGoroutineLimit  func()
	onConnectionLimit func()

	mu sync.RWMutex
}

// Logger interface for resource monitor
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(limits ResourceLimits, logger Logger) *ResourceMonitor {
	if logger == nil {
		logger = &noopLogger{}
	}

	return &ResourceMonitor{
		limits: limits,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// Start begins monitoring resources
func (rm *ResourceMonitor) Start() {
	// Set GC percentage
	if rm.limits.GCPercent > 0 {
		debug.SetGCPercent(rm.limits.GCPercent)
	}

	if rm.limits.MemoryCheckInterval > 0 {
		rm.wg.Add(1)
		go rm.monitorLoop()
	}

	// Perform initial check immediately to catch any existing issues
	rm.checkResources()
}

// Stop stops the resource monitor
func (rm *ResourceMonitor) Stop() {
	rm.stopOnce.Do(func() {
		close(rm.stopCh)
	})
	rm.wg.Wait()
}

// monitorLoop periodically checks resource usage
func (rm *ResourceMonitor) monitorLoop() {
	defer rm.wg.Done()

	ticker := time.NewTicker(rm.limits.MemoryCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.checkResources()
		}
	}
}

// checkResources checks current resource usage against limits
func (rm *ResourceMonitor) checkResources() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Update current values
	rm.mu.Lock()
	if m.Alloc > math.MaxInt64 {
		rm.currentMemory = math.MaxInt64
	} else {
		rm.currentMemory = int64(m.Alloc)
	}
	rm.currentGoroutines = runtime.NumGoroutine()
	memoryMB := rm.currentMemory / 1024 / 1024
	rm.mu.Unlock()

	// Check memory limit
	if rm.limits.MaxMemoryMB > 0 && memoryMB > rm.limits.MaxMemoryMB {
		rm.logger.Warn("Memory limit exceeded",
			"limit_mb", rm.limits.MaxMemoryMB,
			"current_mb", memoryMB)

		// Trigger GC
		runtime.GC()

		// Call callback if set
		if rm.onMemoryLimit != nil {
			rm.onMemoryLimit()
		}
	}

	// Check goroutine limit
	if rm.limits.MaxGoroutines > 0 && rm.currentGoroutines > rm.limits.MaxGoroutines {
		rm.logger.Warn("Goroutine limit exceeded",
			"limit", rm.limits.MaxGoroutines,
			"current", rm.currentGoroutines)

		if rm.onGoroutineLimit != nil {
			rm.onGoroutineLimit()
		}
	}
}

// AddConnection increments connection count
// Returns false if connection limit would be exceeded
func (rm *ResourceMonitor) AddConnection() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.limits.MaxConnections > 0 && rm.currentConnections >= int64(rm.limits.MaxConnections) {
		rm.logger.Warn("Connection limit reached",
			"limit", rm.limits.MaxConnections)

		if rm.onConnectionLimit != nil {
			rm.onConnectionLimit()
		}
		return false
	}

	rm.currentConnections++
	return true
}

// RemoveConnection decrements connection count
func (rm *ResourceMonitor) RemoveConnection() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.currentConnections > 0 {
		rm.currentConnections--
	}
}

// GetStats returns current resource statistics
func (rm *ResourceMonitor) GetStats() ResourceStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return ResourceStats{
		MemoryAllocated: m.Alloc,
		MemoryTotal:     m.TotalAlloc,
		MemorySystem:    m.Sys,
		MemoryGC:        m.NumGC,
		Goroutines:      runtime.NumGoroutine(),
		Connections:     rm.currentConnections,
		MemoryLimitMB:   rm.limits.MaxMemoryMB,
		GoroutineLimit:  rm.limits.MaxGoroutines,
		ConnectionLimit: int64(rm.limits.MaxConnections),
	}
}

// SetMemoryLimitCallback sets callback for memory limit exceeded
func (rm *ResourceMonitor) SetMemoryLimitCallback(fn func()) {
	rm.onMemoryLimit = fn
}

// SetGoroutineLimitCallback sets callback for goroutine limit exceeded
func (rm *ResourceMonitor) SetGoroutineLimitCallback(fn func()) {
	rm.onGoroutineLimit = fn
}

// SetConnectionLimitCallback sets callback for connection limit reached
func (rm *ResourceMonitor) SetConnectionLimitCallback(fn func()) {
	rm.onConnectionLimit = fn
}

// ForceGC forces garbage collection
func (rm *ResourceMonitor) ForceGC() {
	runtime.GC()
}

// SetMaxMemory sets the maximum memory limit at runtime
func (rm *ResourceMonitor) SetMaxMemory(mb int64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.limits.MaxMemoryMB = mb
}

// ResourceStats holds resource statistics
type ResourceStats struct {
	MemoryAllocated uint64 `json:"memory_allocated_bytes"`
	MemoryTotal     uint64 `json:"memory_total_bytes"`
	MemorySystem    uint64 `json:"memory_system_bytes"`
	MemoryGC        uint32 `json:"gc_count"`
	Goroutines      int    `json:"goroutines"`
	Connections     int64  `json:"connections"`
	MemoryLimitMB   int64  `json:"memory_limit_mb"`
	GoroutineLimit  int    `json:"goroutine_limit"`
	ConnectionLimit int64  `json:"connection_limit"`
}

// String returns a formatted string of resource stats
func (rs ResourceStats) String() string {
	return fmt.Sprintf(
		"Memory: %dMB/%dMB, Goroutines: %d/%d, Connections: %d/%d, GC: %d",
		rs.MemoryAllocated/1024/1024,
		rs.MemoryLimitMB,
		rs.Goroutines,
		rs.GoroutineLimit,
		rs.Connections,
		rs.ConnectionLimit,
		rs.MemoryGC,
	)
}

// noopLogger is a no-op logger implementation
type noopLogger struct{}

func (n *noopLogger) Info(msg string, args ...interface{})  { _ = msg }
func (n *noopLogger) Warn(msg string, args ...interface{})  { _ = msg }
func (n *noopLogger) Error(msg string, args ...interface{}) { _ = msg }
