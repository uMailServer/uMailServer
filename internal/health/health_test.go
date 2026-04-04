package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewMonitor(t *testing.T) {
	m := NewMonitor("1.0.0")
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}
	if m.version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", m.version)
	}
}

func TestMonitor_RegisterUnregister(t *testing.T) {
	m := NewMonitor("1.0.0")

	checker := func(ctx context.Context) Check {
		return Check{Status: StatusHealthy, Message: "test"}
	}

	m.Register("test", checker)

	m.mu.RLock()
	if _, ok := m.checkers["test"]; !ok {
		t.Error("checker not registered")
	}
	m.mu.RUnlock()

	m.Unregister("test")

	m.mu.RLock()
	if _, ok := m.checkers["test"]; ok {
		t.Error("checker not unregistered")
	}
	m.mu.RUnlock()
}

func TestMonitor_Check(t *testing.T) {
	m := NewMonitor("1.0.0")

	// Register a healthy checker
	m.Register("healthy", func(ctx context.Context) Check {
		return Check{Status: StatusHealthy, Message: "all good"}
	})

	// Register an unhealthy checker
	m.Register("unhealthy", func(ctx context.Context) Check {
		return Check{Status: StatusUnhealthy, Message: "something wrong"}
	})

	ctx := context.Background()
	report := m.Check(ctx)

	if report.Status != StatusUnhealthy {
		t.Errorf("expected overall status unhealthy, got %s", report.Status)
	}

	if len(report.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(report.Checks))
	}

	if report.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", report.Version)
	}
}

func TestMonitor_CheckLiveness(t *testing.T) {
	m := NewMonitor("1.0.0")

	result := m.CheckLiveness()

	if result["status"] != "alive" {
		t.Errorf("expected status alive, got %v", result["status"])
	}

	if result["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", result["version"])
	}

	if _, ok := result["uptime"]; !ok {
		t.Error("expected uptime in result")
	}
}

func TestMonitor_CheckReadiness(t *testing.T) {
	m := NewMonitor("1.0.0")

	// Only healthy check
	m.Register("healthy", func(ctx context.Context) Check {
		return Check{Status: StatusHealthy}
	})

	ctx := context.Background()
	result := m.CheckReadiness(ctx)

	if !result["ready"].(bool) {
		t.Error("expected ready to be true")
	}

	// Add unhealthy check
	m.Register("unhealthy", func(ctx context.Context) Check {
		return Check{Status: StatusUnhealthy}
	})

	result = m.CheckReadiness(ctx)
	if result["ready"].(bool) {
		t.Error("expected ready to be false with unhealthy check")
	}
}

func TestMonitor_HTTPHandler(t *testing.T) {
	m := NewMonitor("1.0.0")

	m.Register("test", func(ctx context.Context) Check {
		return Check{Status: StatusHealthy, Message: "test check"}
	})

	handler := m.HTTPHandler()

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/health", http.StatusOK},
		{"/health/", http.StatusOK},
		{"/healthz", http.StatusOK},
		{"/health/live", http.StatusOK},
		{"/health/ready", http.StatusOK},
		{"/health/unknown", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("path %s: expected status %d, got %d", tt.path, tt.wantStatus, rr.Code)
			}

			if tt.wantStatus == http.StatusOK {
				contentType := rr.Header().Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("expected Content-Type application/json, got %s", contentType)
				}
			}
		})
	}
}

func TestMonitor_HTTPHandler_ReadinessUnhealthy(t *testing.T) {
	m := NewMonitor("1.0.0")

	// Register an unhealthy check
	m.Register("db", func(ctx context.Context) Check {
		return Check{Status: StatusUnhealthy, Message: "database connection failed"}
	})

	handler := m.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Readiness should return 503 when unhealthy
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestMonitor_HTTPHandler_HealthUnhealthy(t *testing.T) {
	m := NewMonitor("1.0.0")

	// Register an unhealthy check
	m.Register("critical", func(ctx context.Context) Check {
		return Check{Status: StatusUnhealthy, Message: "critical failure"}
	})

	handler := m.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Health should return 503 when unhealthy
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
	}
}

func TestMonitor_HTTPHandler_HealthDegraded(t *testing.T) {
	m := NewMonitor("1.0.0")

	// Register a degraded check
	m.Register("degraded", func(ctx context.Context) Check {
		return Check{Status: StatusDegraded, Message: "running degraded"}
	})

	handler := m.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Degraded should still return 200
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d for degraded, got %d", http.StatusOK, rr.Code)
	}
}

func TestMonitor_Check_CountByStatus(t *testing.T) {
	m := NewMonitor("1.0.0")

	m.Register("healthy1", func(ctx context.Context) Check {
		return Check{Status: StatusHealthy}
	})
	m.Register("healthy2", func(ctx context.Context) Check {
		return Check{Status: StatusHealthy}
	})
	m.Register("degraded1", func(ctx context.Context) Check {
		return Check{Status: StatusDegraded}
	})
	m.Register("unhealthy1", func(ctx context.Context) Check {
		return Check{Status: StatusUnhealthy}
	})

	ctx := context.Background()
	report := m.Check(ctx)

	if report.Status != StatusUnhealthy {
		t.Errorf("expected overall status unhealthy, got %s", report.Status)
	}

	// Count by status manually
	healthy, degraded, unhealthy := 0, 0, 0
	for _, c := range report.Checks {
		switch c.Status {
		case StatusHealthy:
			healthy++
		case StatusDegraded:
			degraded++
		case StatusUnhealthy:
			unhealthy++
		}
	}
	if healthy != 2 {
		t.Errorf("expected 2 healthy, got %d", healthy)
	}
	if degraded != 1 {
		t.Errorf("expected 1 degraded, got %d", degraded)
	}
	if unhealthy != 1 {
		t.Errorf("expected 1 unhealthy, got %d", unhealthy)
	}
}

func TestDatabaseCheck(t *testing.T) {
	// Test healthy database
	healthyCheck := DatabaseCheck(func() error { return nil })
	check := healthyCheck(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	// Test unhealthy database
	unhealthyCheck := DatabaseCheck(func() error { return errors.New("connection refused") })
	check = unhealthyCheck(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", check.Status)
	}
}

func TestQueueCheck(t *testing.T) {
	mockStats := &mockQueueStats{
		stats: QueueStatInfo{
			Pending:  10,
			Sending:  2,
			Failed:   0,
			Deferred: 1,
		},
	}

	check := QueueCheck(mockStats, 100)(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	// Test with failed messages
	mockStats.stats.Failed = 50
	check = QueueCheck(mockStats, 100)(context.Background())

	if check.Status != StatusDegraded {
		t.Errorf("expected degraded status, got %s", check.Status)
	}

	// Test with many failed messages
	mockStats.stats.Failed = 150
	check = QueueCheck(mockStats, 100)(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", check.Status)
	}
}

func TestMessageStoreCheck(t *testing.T) {
	// Test healthy
	healthyCheck := MessageStoreCheck(func() error { return nil })
	check := healthyCheck(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	// Test unhealthy
	unhealthyCheck := MessageStoreCheck(func() error { return errors.New("disk full") })
	check = unhealthyCheck(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", check.Status)
	}
}

func TestSearchIndexCheck(t *testing.T) {
	// Test healthy with recent index
	healthyCheck := SearchIndexCheck(
		func() error { return nil },
		func() time.Time { return time.Now().Add(-1 * time.Hour) },
	)
	check := healthyCheck(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	// Test stale index
	staleCheck := SearchIndexCheck(
		func() error { return nil },
		func() time.Time { return time.Now().Add(-48 * time.Hour) },
	)
	check = staleCheck(context.Background())

	if check.Status != StatusDegraded {
		t.Errorf("expected degraded status for stale index, got %s", check.Status)
	}
}

type mockQueueStats struct {
	stats QueueStatInfo
	err   error
}

func (m *mockQueueStats) GetStats() (QueueStatInfo, error) {
	return m.stats, m.err
}
