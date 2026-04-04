package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueueCheck_Healthy(t *testing.T) {
	mock := &mockQueueStats{
		stats: QueueStatInfo{
			Pending:  5,
			Sending:  2,
			Failed:   0,
			Deferred: 1,
		},
	}

	checker := QueueCheck(mock, 100)
	check := checker(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	if check.Details["pending"] != 5 {
		t.Errorf("expected pending=5, got %v", check.Details["pending"])
	}

	if check.Details["total"] != 8 {
		t.Errorf("expected total=8, got %v", check.Details["total"])
	}
}

func TestQueueCheck_Degraded_FailedMessages(t *testing.T) {
	mock := &mockQueueStats{
		stats: QueueStatInfo{
			Pending:  5,
			Sending:  2,
			Failed:   15, // Between 10 and 100
			Deferred: 1,
		},
	}

	checker := QueueCheck(mock, 100)
	check := checker(context.Background())

	if check.Status != StatusDegraded {
		t.Errorf("expected degraded status for failed messages, got %s", check.Status)
	}
}

func TestQueueCheck_Unhealthy_ManyFailed(t *testing.T) {
	mock := &mockQueueStats{
		stats: QueueStatInfo{
			Pending:  5,
			Sending:  2,
			Failed:   150, // More than 100
			Deferred: 1,
		},
	}

	checker := QueueCheck(mock, 100)
	check := checker(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status for many failed messages, got %s", check.Status)
	}
}

func TestQueueCheck_Degraded_HighPending(t *testing.T) {
	mock := &mockQueueStats{
		stats: QueueStatInfo{
			Pending:  150, // More than maxPending
			Sending:  2,
			Failed:   0,
			Deferred: 1,
		},
	}

	checker := QueueCheck(mock, 100)
	check := checker(context.Background())

	if check.Status != StatusDegraded {
		t.Errorf("expected degraded status for high pending, got %s", check.Status)
	}
}

func TestQueueCheck_Error(t *testing.T) {
	mock := &mockQueueStats{
		err: errors.New("connection failed"),
	}

	checker := QueueCheck(mock, 100)
	check := checker(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status on error, got %s", check.Status)
	}

	if check.Message != "failed to get queue stats: connection failed" {
		t.Errorf("unexpected error message: %s", check.Message)
	}
}

// MessageStoreCheck tests
func TestMessageStoreCheck_Healthy(t *testing.T) {
	ping := func() error { return nil }
	checker := MessageStoreCheck(ping)
	check := checker(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	if check.Message != "message store healthy" {
		t.Errorf("unexpected message: %s", check.Message)
	}
}

func TestMessageStoreCheck_Error(t *testing.T) {
	ping := func() error { return errors.New("ping failed") }
	checker := MessageStoreCheck(ping)
	check := checker(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", check.Status)
	}

	if check.Message != "message store ping failed: ping failed" {
		t.Errorf("unexpected error message: %s", check.Message)
	}
}

func TestMessageStoreCheck_Timeout(t *testing.T) {
	ping := func() error {
		time.Sleep(2 * time.Second)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	checker := MessageStoreCheck(ping)
	check := checker(ctx)

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status on timeout, got %s", check.Status)
	}

	if check.Message != "message store ping timeout" {
		t.Errorf("unexpected timeout message: %s", check.Message)
	}
}

// SearchIndexCheck tests
func TestSearchIndexCheck_Healthy(t *testing.T) {
	ping := func() error { return nil }
	lastIndexTime := func() time.Time { return time.Now().Add(-1 * time.Hour) }

	checker := SearchIndexCheck(ping, lastIndexTime)
	check := checker(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	if _, ok := check.Details["last_index_hours_ago"]; !ok {
		t.Error("expected last_index_hours_ago in details")
	}
}

func TestSearchIndexCheck_Stale(t *testing.T) {
	ping := func() error { return nil }
	lastIndexTime := func() time.Time { return time.Now().Add(-48 * time.Hour) } // 48 hours ago

	checker := SearchIndexCheck(ping, lastIndexTime)
	check := checker(context.Background())

	if check.Status != StatusDegraded {
		t.Errorf("expected degraded status for stale index, got %s", check.Status)
	}

	if check.Details["last_index_hours_ago"].(float64) < 24 {
		t.Error("expected hours_ago to be greater than 24")
	}
}

func TestSearchIndexCheck_PingError(t *testing.T) {
	ping := func() error { return errors.New("search index unavailable") }
	lastIndexTime := func() time.Time { return time.Now() }

	checker := SearchIndexCheck(ping, lastIndexTime)
	check := checker(context.Background())

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status on ping error, got %s", check.Status)
	}

	if check.Message != "search index ping failed: search index unavailable" {
		t.Errorf("unexpected error message: %s", check.Message)
	}
}

func TestSearchIndexCheck_Timeout(t *testing.T) {
	ping := func() error {
		time.Sleep(2 * time.Second)
		return nil
	}
	lastIndexTime := func() time.Time { return time.Now() }

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	checker := SearchIndexCheck(ping, lastIndexTime)
	check := checker(ctx)

	if check.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status on timeout, got %s", check.Status)
	}

	if check.Message != "search index ping timeout" {
		t.Errorf("unexpected timeout message: %s", check.Message)
	}
}

func TestSearchIndexCheck_NoLastIndexTime(t *testing.T) {
	ping := func() error { return nil }

	checker := SearchIndexCheck(ping, nil)
	check := checker(context.Background())

	if check.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", check.Status)
	}

	if check.Message != "search index healthy" {
		t.Errorf("unexpected message: %s", check.Message)
	}
}
