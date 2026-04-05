package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// Test handleFilters dispatcher
func TestHandleFilters_Get(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilters(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFilters_Post(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name":     "Test Filter",
		"matchAll": true,
		"conditions": []map[string]string{
			{"field": "from", "operator": "contains", "value": "test@example.com"},
		},
		"actions": []map[string]string{
			{"type": "move", "target": "Junk"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilters(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestHandleFilters_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("PATCH", "/api/v1/filters", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilters(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test handleFilter dispatcher
func TestHandleFilter_Get(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	req := httptest.NewRequest("GET", "/api/v1/filters/"+filter.ID, nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFilter_Put(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	body := map[string]interface{}{
		"name": "Updated Filter Name",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/filters/"+filter.ID, bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFilter_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	req := httptest.NewRequest("DELETE", "/api/v1/filters/"+filter.ID, nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFilter_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("PATCH", "/api/v1/filters/filter-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilter(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test handleGetFilters
func TestHandleGetFilters_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleGetFilters(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetFilters_WithFilters(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create multiple filters
	createTestFilter(t, server, "user@example.com")
	createTestFilter(t, server, "user@example.com")

	req := httptest.NewRequest("GET", "/api/v1/filters", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetFilters(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	filters, ok := result["filters"].([]interface{})
	if !ok {
		t.Fatal("Expected filters array in response")
	}

	if len(filters) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(filters))
	}
}

// Test handleGetFilter
func TestHandleGetFilter_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters/nonexistent-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetFilter(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetFilter_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters/filter-id", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleGetFilter(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleCreateFilter
func TestHandleCreateFilter_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateFilter_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name": "",
		"conditions": []map[string]string{
			{"field": "from", "operator": "contains", "value": "test"},
		},
		"actions": []map[string]string{
			{"type": "delete"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateFilter_MissingConditions(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name":       "Test Filter",
		"conditions": []map[string]string{},
		"actions": []map[string]string{
			{"type": "delete"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateFilter_MissingActions(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name": "Test Filter",
		"conditions": []map[string]string{
			{"field": "from", "operator": "contains", "value": "test"},
		},
		"actions": []map[string]string{},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateFilter_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name": "Test Filter",
		"conditions": []map[string]string{
			{"field": "from", "operator": "contains", "value": "test"},
		},
		"actions": []map[string]string{
			{"type": "delete"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters", bytes.NewReader(bodyJSON))
	// No user context
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleUpdateFilter
func TestHandleUpdateFilter_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name": "Updated Name",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/filters/nonexistent-id", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleUpdateFilter(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateFilter_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	req := httptest.NewRequest("PUT", "/api/v1/filters/"+filter.ID, bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleUpdateFilter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateFilter_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"name": "Updated Name",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/filters/filter-id", bytes.NewReader(bodyJSON))
	// No user context
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleUpdateFilter(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleUpdateFilter_ToggleEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	enabled := false
	body := map[string]interface{}{
		"enabled": &enabled,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/filters/"+filter.ID, bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleUpdateFilter(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result EmailFilter
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.Enabled != false {
		t.Errorf("Expected enabled=false, got %v", result.Enabled)
	}
}

// Test handleDeleteFilter
func TestHandleDeleteFilter_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/filters/nonexistent-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleDeleteFilter(w, req)

	// Delete returns 200 even if filter doesn't exist (bbolt delete doesn't error on missing key)
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleDeleteFilter_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/filters/filter-id", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleDeleteFilter(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleFilterToggle
func TestHandleFilterToggle(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create a filter first
	filter := createTestFilter(t, server, "user@example.com")

	req := httptest.NewRequest("POST", "/api/v1/filters/"+filter.ID+"/toggle", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilterToggle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result EmailFilter
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Filter was created with enabled=true, toggle should make it false
	if result.Enabled != false {
		t.Errorf("Expected enabled=false after toggle, got %v", result.Enabled)
	}
}

func TestHandleFilterToggle_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters/filter-id/toggle", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilterToggle(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleFilterToggle_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/filters/filter-id/toggle", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleFilterToggle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleFilterToggle_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/filters/nonexistent-id/toggle", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilterToggle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleFilterToggle_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Path with empty filter ID results in 404 (filter not found)
	req := httptest.NewRequest("POST", "/api/v1/filters//toggle", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilterToggle(w, req)

	// Empty filter ID results in filter not found
	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Test handleFilterReorder
func TestHandleFilterReorder(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Create multiple filters
	filter1 := createTestFilter(t, server, "user@example.com")
	filter2 := createTestFilter(t, server, "user@example.com")

	body := map[string]interface{}{
		"filterIds": []string{filter2.ID, filter1.ID}, // Reverse order
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters/reorder", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilterReorder(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFilterReorder_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/filters/reorder", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleFilterReorder(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleFilterReorder_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := map[string]interface{}{
		"filterIds": []string{"filter-1", "filter-2"},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/filters/reorder", bytes.NewReader(bodyJSON))
	// No user context
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilterReorder(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleFilterReorder_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/filters/reorder", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilterReorder(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// Test filterKey helper
func TestFilterKey(t *testing.T) {
	key := filterKey("user@example.com", "filter-123")
	expected := "user@example.com:filter-123"
	if key != expected {
		t.Errorf("filterKey = %s, want %s", key, expected)
	}
}

// Test database functions directly
func TestGetUserFilters_NoDB(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	filters, err := server.getUserFilters("user@example.com")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(filters) != 0 {
		t.Errorf("Expected 0 filters, got %d", len(filters))
	}
}

func TestGetFilter_NoDB(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	_, err := server.getFilter("user@example.com", "filter-id")
	if err == nil {
		t.Error("Expected error when no database")
	}
}

func TestSaveFilter_NoDB(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	filter := &EmailFilter{
		ID:     "test-id",
		UserID: "user@example.com",
		Name:   "Test Filter",
	}

	err := server.saveFilter(filter)
	if err == nil {
		t.Error("Expected error when no database")
	}
}

func TestDeleteFilter_NoDB(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	err := server.deleteFilter("user@example.com", "filter-id")
	if err == nil {
		t.Error("Expected error when no database")
	}
}

func TestReorderFilters_NoDB(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	err := server.reorderFilters("user@example.com", []string{"filter-1", "filter-2"})
	if err == nil {
		t.Error("Expected error when no database")
	}
}

// Helper function to create a test filter
func createTestFilter(t *testing.T, server *Server, userID string) *EmailFilter {
	t.Helper()

	filter := &EmailFilter{
		ID:       "test-filter-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(time.Now().Nanosecond()),
		Name:     "Test Filter",
		UserID:   userID,
		Enabled:  true,
		MatchAll: true,
		Conditions: []FilterCondition{
			{Field: "from", Operator: "contains", Value: "test@example.com"},
		},
		Actions: []FilterAction{
			{Type: "move", Target: "Junk"},
		},
	}

	if err := server.saveFilter(filter); err != nil {
		t.Fatalf("Failed to create test filter: %v", err)
	}

	return filter
}
