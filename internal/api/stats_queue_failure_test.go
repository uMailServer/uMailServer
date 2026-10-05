package api

import (
	"encoding/json"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/queue"
)

// F4816 retains the state/error boundary covered by the audit proof.
func TestStatsReportsQueueDependencyFailure(t *testing.T) {
	s, d := apiUpdateFixtureServer(t)
	queueDB, err := db.Open(t.TempDir() + "/queue.db")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "sending", "failed"} {
		if err := queueDB.Enqueue(&db.QueueEntry{ID: status, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	s.queueMgr = queue.NewManager(queueDB, nil, t.TempDir(), nil)
	rec := apiUpdateFixtureCall(t, "GET", "/api/v1/stats", "", s.handleStats)
	apiUpdateFixtureWant(t, "CONTROL working stats", rec.Code, 200)
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "CONTROL queue count", body["queue_size"], float64(3))
	if err := queueDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, queueErr := s.queueMgr.GetStats()
	apiUpdateFixtureWant(t, "CONTROL closed queue produces dependency error", queueErr != nil, true)
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/stats", "", s.handleStats)
	apiUpdateFixtureWant(t, "statistics error reaches HTTP caller", rec.Code, 500)
	s.queueMgr = nil
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/stats", "", s.handleStats)
	apiUpdateFixtureWant(t, "no manager supported", rec.Code, 200)
	s.queueMgr = queue.NewManager(d, nil, t.TempDir(), nil)
	rec = apiUpdateFixtureCall(t, "GET", "/api/v1/stats", "", s.handleStats)
	apiUpdateFixtureWant(t, "healthy empty queue", rec.Code, 200)
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	apiUpdateFixtureWant(t, "empty count", body["queue_size"], float64(0))
}
