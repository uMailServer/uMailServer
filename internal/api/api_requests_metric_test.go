package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/metrics"
)

// F7793: umailserver_api_requests_total ("HTTP API requests served") stayed 0:
// nothing called APIRequest.
func apiRequestsServed() uint64 {
	return metrics.Get().GetStats()["api"].(map[string]uint64)["requests"]
}

func TestAPIRequestsAreCounted(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})

	serve := func(path string) {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	before := apiRequestsServed()
	serve("/api/v1/stats")
	serve("/api/v1/domains")
	if got := apiRequestsServed() - before; got != 2 {
		t.Fatalf("EXPECTED 2 API requests counted\nACTUAL: %d", got)
	}

	// control: the static UI is not an API request
	before = apiRequestsServed()
	serve("/admin/")
	serve("/")
	if got := apiRequestsServed() - before; got != 0 {
		t.Fatalf("static UI requests were counted as API requests (%d)", got)
	}
}
