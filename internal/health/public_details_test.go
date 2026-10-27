package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /health is unauthenticated: it reports each check's status and message
// but not its details (paths, queue counts, disk sizes).
func TestHealthEndpointOmitsDetails(t *testing.T) {
	m := NewMonitor("test")
	m.Register("disk_space", func(context.Context) Check {
		return Check{Name: "disk_space", Status: StatusDegraded, Message: "85% used",
			Details: map[string]interface{}{"path": "/var/lib/secret-data-dir"}}
	})
	rec := httptest.NewRecorder()
	m.HTTPHandler()(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	body := rec.Body.String()
	if strings.Contains(body, "secret-data-dir") || !strings.Contains(body, "85% used") || !strings.Contains(body, `"degraded"`) {
		t.Fatalf("/health body: %s", body)
	}
	// Check itself still carries the details for in-process callers.
	if r := m.Check(context.Background()); r.Checks[0].Details["path"] == nil {
		t.Fatal("Check lost the details")
	}
}
