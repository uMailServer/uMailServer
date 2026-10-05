package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

func summaryPageResponse(t *testing.T, s *Server, user, query string) ThreadListResponse {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads"+query, nil)
	r = r.WithContext(context.WithValue(r.Context(), "user", user))
	w := httptest.NewRecorder()
	s.handleThreads(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected handler failure: %d %s", w.Code, w.Body.String())
	}
	var result ThreadListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func seedThreeThreadSummaries(t *testing.T) (*Server, string) {
	t.Helper()
	s, database, user, _ := startThreadsTestServer(t)
	for _, id := range []string{"thread-second", "thread-third"} {
		if err := database.UpdateThread(user, &storage.Thread{ThreadID: id, Subject: id, MessageCount: 1}); err != nil {
			t.Fatal(err)
		}
	}
	return s, user
}

func TestThreadTotalWithoutPagination(t *testing.T) {
	s, user := seedThreeThreadSummaries(t)
	result := summaryPageResponse(t, s, user, "")
	t.Logf("CONTROL EXPECTED: total=3, rows=3 ACTUAL: total=%d, rows=%d", result.Total, len(result.Threads))
	if result.Total != 3 || len(result.Threads) != 3 {
		t.Fatal("control failed")
	}
}

func TestThreadTotalWithPagination(t *testing.T) {
	s, user := seedThreeThreadSummaries(t)
	result := summaryPageResponse(t, s, user, "?limit=1&offset=1")
	t.Logf("EXPECTED: total=3, rows=1 ACTUAL: total=%d, rows=%d", result.Total, len(result.Threads))
	if result.Total != 3 || len(result.Threads) != 1 {
		t.Error("DEFECT F4742: total changes with page size")
	}
}

func TestThreadTotalPaginationBoundaries(t *testing.T) {
	s, user := seedThreeThreadSummaries(t)
	for _, offset := range []int{0, 2, 3, 100} {
		result := summaryPageResponse(t, s, user, fmt.Sprintf("?limit=1&offset=%d", offset))
		rows := 0
		if offset < 3 {
			rows = 1
		}
		if result.Total != 3 || len(result.Threads) != rows || result.Offset != offset {
			t.Errorf("offset %d: total=%d rows=%d offset=%d", offset, result.Total, len(result.Threads), result.Offset)
		}
	}
	if result := summaryPageResponse(t, s, "empty@example.com", "?offset=100"); result.Total != 0 || len(result.Threads) != 0 {
		t.Fatal("empty user has threads")
	}
}
