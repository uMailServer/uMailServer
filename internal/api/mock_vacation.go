package api

import (
	"github.com/umailserver/umailserver/internal/vacation"
)

// MockVacationManager mock for testing
type MockVacationManager struct {
	GetConfigError    error
	SetConfigError    error
	DeleteConfigError error
	ListActiveError   error

	GetConfigResult    *vacation.Config
	DeleteConfigCalled bool
	SetConfigCalled    bool
	SetConfigArg       *vacation.Config
	ListActiveResult   []string
}

func (m *MockVacationManager) GetConfig(userID string) (*vacation.Config, error) {
	if m.GetConfigError != nil {
		return nil, m.GetConfigError
	}
	return m.GetConfigResult, nil
}

func (m *MockVacationManager) SetConfig(userID string, cfg *vacation.Config) error {
	m.SetConfigCalled = true
	m.SetConfigArg = cfg
	return m.SetConfigError
}

func (m *MockVacationManager) DeleteConfig(userID string) error {
	m.DeleteConfigCalled = true
	return m.DeleteConfigError
}

func (m *MockVacationManager) ListActive() ([]string, error) {
	if m.ListActiveError != nil {
		return nil, m.ListActiveError
	}
	return m.ListActiveResult, nil
}
