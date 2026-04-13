package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// Status represents the health status of a component
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Check represents a health check for a component
type Check struct {
	Name         string                 `json:"name"`
	Status       Status                 `json:"status"`
	Message      string                 `json:"message,omitempty"`
	ResponseTime time.Duration          `json:"response_time_ms"`
	Details      map[string]interface{} `json:"details,omitempty"`
}

// Report represents a complete health report
type Report struct {
	Status    Status     `json:"status"`
	Timestamp time.Time  `json:"timestamp"`
	Version   string     `json:"version"`
	Checks    []Check    `json:"checks"`
	System    SystemInfo `json:"system"`
}

// SystemInfo holds system-level information
type SystemInfo struct {
	GoVersion  string `json:"go_version"`
	Goroutines int    `json:"goroutines"`
	MemoryMB   uint64 `json:"memory_mb"`
	MemoryUsed uint64 `json:"memory_used_mb"`
	Uptime     string `json:"uptime"`
}

// Checker is a function that performs a health check
type Checker func(ctx context.Context) Check

// Monitor manages health checks
type Monitor struct {
	mu        sync.RWMutex
	checkers  map[string]Checker
	startTime time.Time
	version   string
}

// NewMonitor creates a new health monitor
func NewMonitor(version string) *Monitor {
	return &Monitor{
		checkers:  make(map[string]Checker),
		startTime: time.Now(),
		version:   version,
	}
}

// Register adds a health checker
func (m *Monitor) Register(name string, checker Checker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkers[name] = checker
}

// Unregister removes a health checker
func (m *Monitor) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checkers, name)
}

// Check runs all health checks and returns a report
func (m *Monitor) Check(ctx context.Context) Report {
	m.mu.RLock()
	checkers := make(map[string]Checker, len(m.checkers))
	for k, v := range m.checkers {
		checkers[k] = v
	}
	m.mu.RUnlock()

	report := Report{
		Status:    StatusHealthy,
		Timestamp: time.Now(),
		Version:   m.version,
		Checks:    make([]Check, 0, len(checkers)),
		System:    m.getSystemInfo(),
	}

	// Run checks concurrently
	var wg sync.WaitGroup
	checkCh := make(chan Check, len(checkers))

	for name, checker := range checkers {
		wg.Add(1)
		go func(n string, c Checker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			start := time.Now()
			check := c(checkCtx)
			check.ResponseTime = time.Since(start)
			check.Name = n

			checkCh <- check
		}(name, checker)
	}

	go func() {
		wg.Wait()
		close(checkCh)
	}()

	// Collect results
	for check := range checkCh {
		report.Checks = append(report.Checks, check)

		// Determine overall status
		if check.Status == StatusUnhealthy {
			report.Status = StatusUnhealthy
		} else if check.Status == StatusDegraded && report.Status == StatusHealthy {
			report.Status = StatusDegraded
		}
	}

	return report
}

// CheckLiveness returns a simple liveness check
func (m *Monitor) CheckLiveness() map[string]interface{} {
	return map[string]interface{}{
		"status":  "alive",
		"uptime":  time.Since(m.startTime).String(),
		"version": m.version,
	}
}

// CheckReadiness returns readiness status based on critical checks
func (m *Monitor) CheckReadiness(ctx context.Context) map[string]interface{} {
	report := m.Check(ctx)

	// For readiness, we only care about critical checks being healthy
	// Degraded is acceptable for readiness (service can still handle requests)
	ready := report.Status != StatusUnhealthy

	return map[string]interface{}{
		"ready":   ready,
		"status":  report.Status,
		"checks":  len(report.Checks),
		"healthy": countByStatus(report.Checks, StatusHealthy),
	}
}

func (m *Monitor) getSystemInfo() SystemInfo {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return SystemInfo{
		GoVersion:  runtime.Version(),
		Goroutines: runtime.NumGoroutine(),
		MemoryMB:   mem.Sys / 1024 / 1024,
		MemoryUsed: mem.Alloc / 1024 / 1024,
		Uptime:     time.Since(m.startTime).String(),
	}
}

func countByStatus(checks []Check, status Status) int {
	count := 0
	for _, c := range checks {
		if c.Status == status {
			count++
		}
	}
	return count
}

// HTTPHandler returns an HTTP handler for health checks
func (m *Monitor) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		path := r.URL.Path

		switch path {
		case "/health/live", "/healthz":
			// Liveness probe - simple alive check
			result := m.CheckLiveness()
			writeJSON(w, http.StatusOK, result)

		case "/health/ready":
			// Readiness probe - check if ready to serve traffic
			result := m.CheckReadiness(ctx)
			if result["ready"].(bool) {
				writeJSON(w, http.StatusOK, result)
			} else {
				writeJSON(w, http.StatusServiceUnavailable, result)
			}

		case "/health", "/health/":
			// Full health check
			report := m.Check(ctx)
			status := http.StatusOK
			if report.Status == StatusUnhealthy {
				status = http.StatusServiceUnavailable
			} else if report.Status == StatusDegraded {
				status = http.StatusOK // 200 but with degraded status in body
			}
			writeJSON(w, status, report)

		default:
			http.NotFound(w, r)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Simple JSON encoding - in production use proper json marshaling
	jsonData, _ := json.Marshal(data)
	_, _ = w.Write(jsonData) // Best-effort, client may disconnect
}

// Predefined check helpers

// DatabaseCheck creates a database health checker
func DatabaseCheck(ping func() error) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Status:  StatusHealthy,
			Details: make(map[string]interface{}),
		}

		done := make(chan error, 1)
		go func() {
			done <- ping()
		}()

		select {
		case err := <-done:
			if err != nil {
				check.Status = StatusUnhealthy
				check.Message = fmt.Sprintf("database ping failed: %v", err)
			} else {
				check.Message = "database connection healthy"
			}
		case <-ctx.Done():
			check.Status = StatusUnhealthy
			check.Message = "database ping timeout"
		}

		return check
	}
}
