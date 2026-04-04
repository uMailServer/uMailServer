package health

import (
	"context"
	"fmt"
	"time"
)

// QueueStats provides queue statistics
type QueueStats interface {
	GetStats() (QueueStatInfo, error)
}

// QueueStatInfo holds queue statistics
type QueueStatInfo struct {
	Pending  int
	Sending  int
	Failed   int
	Deferred int
}

// QueueCheck creates a queue health checker
func QueueCheck(stats QueueStats, maxPending int) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "queue",
			Status:  StatusHealthy,
			Details: make(map[string]interface{}),
		}

		queueStats, err := stats.GetStats()
		if err != nil {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("failed to get queue stats: %v", err)
			return check
		}

		check.Details["pending"] = queueStats.Pending
		check.Details["sending"] = queueStats.Sending
		check.Details["failed"] = queueStats.Failed
		check.Details["deferred"] = queueStats.Deferred
		check.Details["total"] = queueStats.Pending + queueStats.Sending + queueStats.Failed + queueStats.Deferred

		total := queueStats.Pending + queueStats.Sending + queueStats.Failed + queueStats.Deferred

		if queueStats.Failed > 100 {
			check.Status = StatusUnhealthy
			check.Message = fmt.Sprintf("queue has %d failed messages", queueStats.Failed)
		} else if queueStats.Failed > 10 {
			check.Status = StatusDegraded
			check.Message = fmt.Sprintf("queue has %d failed messages", queueStats.Failed)
		} else if maxPending > 0 && queueStats.Pending > maxPending {
			check.Status = StatusDegraded
			check.Message = fmt.Sprintf("queue has %d pending messages (high load)", queueStats.Pending)
		} else {
			check.Message = fmt.Sprintf("queue healthy: %d messages", total)
		}

		return check
	}
}

// MessageStoreCheck creates a message store health checker
func MessageStoreCheck(ping func() error) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "message_store",
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
				check.Message = fmt.Sprintf("message store ping failed: %v", err)
			} else {
				check.Message = "message store healthy"
			}
		case <-ctx.Done():
			check.Status = StatusUnhealthy
			check.Message = "message store ping timeout"
		}

		return check
	}
}

// SearchIndexCheck creates a search index health checker
func SearchIndexCheck(ping func() error, lastIndexTime func() time.Time) Checker {
	return func(ctx context.Context) Check {
		check := Check{
			Name:    "search_index",
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
				check.Message = fmt.Sprintf("search index ping failed: %v", err)
				return check
			}
		case <-ctx.Done():
			check.Status = StatusUnhealthy
			check.Message = "search index ping timeout"
			return check
		}

		// Check last index time
		if lastIndexTime != nil {
			lastTime := lastIndexTime()
			hoursSince := time.Since(lastTime).Hours()
			check.Details["last_index_hours_ago"] = hoursSince

			if hoursSince > 24 {
				check.Status = StatusDegraded
				check.Message = fmt.Sprintf("search index stale: last update %.1f hours ago", hoursSince)
			} else {
				check.Message = fmt.Sprintf("search index healthy: last update %.1f hours ago", hoursSince)
			}
		} else {
			check.Message = "search index healthy"
		}

		return check
	}
}
