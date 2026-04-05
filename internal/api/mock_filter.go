package api

// MockFilterManager mock for testing
type MockFilterManager struct {
	GetUserFiltersError error
	GetFilterError      error
	SaveFilterError     error
	DeleteFilterError   error
	ReorderFiltersError error

	GetUserFiltersResult []*EmailFilter
	GetFilterResult      *EmailFilter
	SaveFilterCalled     bool
	DeleteFilterCalled   bool
	ReorderFiltersCalled bool
	ReorderFiltersArg    []string
}

func (m *MockFilterManager) GetUserFilters(userID string) ([]*EmailFilter, error) {
	if m.GetUserFiltersError != nil {
		return nil, m.GetUserFiltersError
	}
	return m.GetUserFiltersResult, nil
}

func (m *MockFilterManager) GetFilter(userID, filterID string) (*EmailFilter, error) {
	if m.GetFilterError != nil {
		return nil, m.GetFilterError
	}
	return m.GetFilterResult, nil
}

func (m *MockFilterManager) SaveFilter(filter *EmailFilter) error {
	m.SaveFilterCalled = true
	return m.SaveFilterError
}

func (m *MockFilterManager) DeleteFilter(userID, filterID string) error {
	m.DeleteFilterCalled = true
	return m.DeleteFilterError
}

func (m *MockFilterManager) ReorderFilters(userID string, filterIDs []string) error {
	m.ReorderFiltersCalled = true
	m.ReorderFiltersArg = filterIDs
	return m.ReorderFiltersError
}
