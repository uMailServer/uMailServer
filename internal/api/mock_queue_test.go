package api

import (
	"github.com/umailserver/umailserver/internal/queue"
)

// MockQueueManager mock for testing
type MockQueueManager struct {
	StatsError  error
	RetryError  error
	DeleteError error
	StatsResult *queue.QueueStats
}

func (m *MockQueueManager) GetStats() (*queue.QueueStats, error) {
	if m.StatsError != nil {
		return nil, m.StatsError
	}
	return m.StatsResult, nil
}

func (m *MockQueueManager) RetryEntry(id string) error {
	return m.RetryError
}

func (m *MockQueueManager) DeleteEntry(id string) error {
	return m.DeleteError
}
