package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/vacation"
)

// --- Vacation Manager Interface Tests ---

func TestVacationMgr_GetConfig_Error(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockVacationMgr := &MockVacationManager{
		GetConfigError: errors.New("database connection failed"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, mockVacationMgr, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetVacation(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestVacationMgr_SetConfig_Error(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockVacationMgr := &MockVacationManager{
		SetConfigError: errors.New("failed to save vacation config"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, mockVacationMgr, nil, nil, nil, nil)

	body := VacationConfig{
		Enabled: true,
		Subject: "Vacation",
		Message: "I'm on vacation",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/vacation", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleSetVacation(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestVacationMgr_DeleteConfig_Error(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockVacationMgr := &MockVacationManager{
		DeleteConfigError: errors.New("failed to delete vacation config"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, mockVacationMgr, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleDeleteVacation(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandleAdminVacations_WithMockListActiveError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockVacationMgr := &MockVacationManager{
		ListActiveError: errors.New("failed to list active vacations"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, mockVacationMgr, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/vacations", nil)
	req = req.WithContext(withUser(req.Context(), "admin@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), true))
	w := httptest.NewRecorder()

	server.handleAdminVacations(w, req)

	// Should still return 200 with empty list (error is logged but handled gracefully)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// --- Push Service Interface Tests ---

func TestHandlePushVAPID_WithMockService(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{
		GetVAPIDPublicKeyResult: "test-vapid-public-key-12345",
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/push/vapid-public-key", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushVAPID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result map[string]string
	json.NewDecoder(w.Body).Decode(&result)
	if result["publicKey"] != "test-vapid-public-key-12345" {
		t.Errorf("Expected public key, got %s", result["publicKey"])
	}
}

func TestHandlePushSubscribe_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{
		SubscribeError: errors.New("failed to subscribe"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	body := map[string]string{
		"endpoint": "https://fcm.googleapis.com/fcm/send/test",
		"p256dh":   "test-p256dh-key",
		"auth":     "test-auth-secret",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandlePushUnsubscribe_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{
		UnsubscribeError: errors.New("failed to unsubscribe"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/push/unsubscribe?id=test-sub-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushUnsubscribe(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandlePushTest_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{
		SendNotificationError: errors.New("failed to send notification"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushTest(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

// --- Filter Manager Interface Tests ---

func TestHandleGetFilters_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFilterMgr := &MockFilterManager{
		GetUserFiltersError: errors.New("failed to get filters"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, mockFilterMgr, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/filters", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetFilters(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandleGetFilter_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFilterMgr := &MockFilterManager{
		GetFilterError: errors.New("filter not found"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, mockFilterMgr, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/filters/test-filter-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetFilter(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleCreateFilter_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFilterMgr := &MockFilterManager{
		SaveFilterError: errors.New("failed to save filter"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, mockFilterMgr, nil, nil, nil)

	body := map[string]interface{}{
		"name": "Test Filter",
		"conditions": []map[string]string{
			{"field": "from", "operator": "contains", "value": "test@example.com"},
		},
		"actions": []map[string]string{
			{"type": "move", "target": "Junk"},
		},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/filters", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleCreateFilter(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandleDeleteFilter_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFilterMgr := &MockFilterManager{
		DeleteFilterError: errors.New("failed to delete filter"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, mockFilterMgr, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/filters/test-filter-id", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleDeleteFilter(w, req)

	// Delete returns 404 on error
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleFilterReorder_WithMockError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFilterMgr := &MockFilterManager{
		ReorderFiltersError: errors.New("failed to reorder filters"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, mockFilterMgr, nil, nil, nil)

	body := map[string]interface{}{
		"filterIds": []string{"filter-1", "filter-2"},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/filters/reorder", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleFilterReorder(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

// --- Vacation Manager with mock vacation config ---

func TestHandleGetVacation_WithMockConfig(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	startDate := time.Now().Add(24 * time.Hour)
	endDate := time.Now().Add(7 * 24 * time.Hour)

	mockVacationMgr := &MockVacationManager{
		GetConfigResult: &vacation.Config{
			Enabled:      true,
			Subject:      "Mock Vacation",
			Message:      "I'm on mock vacation",
			StartDate:    startDate,
			EndDate:      endDate,
			SendInterval: 24 * time.Hour,
		},
	}

	server := NewServerWithInterfaces(database, nil, Config{}, mockVacationMgr, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetVacation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var result VacationConfig
	json.NewDecoder(w.Body).Decode(&result)
	if result.Subject != "Mock Vacation" {
		t.Errorf("Expected subject 'Mock Vacation', got %s", result.Subject)
	}
	if !result.Enabled {
		t.Error("Expected enabled to be true")
	}
}

// --- Push Service notification tracking ---

func TestHandlePushSubscribe_TracksUserID(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	body := map[string]string{
		"endpoint": "https://fcm.googleapis.com/fcm/send/test",
		"p256dh":   "test-p256dh-key",
		"auth":     "test-auth-secret",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/subscribe", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePushSubscribe(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	if len(mockPushSvc.SubscribeCalls) != 1 || mockPushSvc.SubscribeCalls[0] != "user@example.com" {
		t.Errorf("Expected Subscribe called with user@example.com, got %v", mockPushSvc.SubscribeCalls)
	}
}

func TestHandlePushTest_TracksUserID(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockPushSvc := &MockPushService{}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, mockPushSvc, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/push/test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handlePushTest(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if len(mockPushSvc.SendNotificationCalls) != 1 || mockPushSvc.SendNotificationCalls[0].UserID != "user@example.com" {
		t.Errorf("Expected SendNotification called with user@example.com, got %v", mockPushSvc.SendNotificationCalls)
	}
}

// --- MockFS Tests ---

func TestMockFS_Open(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{
			"index.html": "<html><body>Test</body></html>",
			"test.js":    "console.log('test');",
		},
	}

	// Test opening existing file
	file, err := mockFS.Open("index.html")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer file.Close()

	content := make([]byte, 100)
	n, _ := file.Read(content)
	if n == 0 {
		t.Error("Expected to read content")
	}
}

func TestMockFS_OpenError(t *testing.T) {
	mockFS := &MockFS{
		OpenError: errors.New("mock fs error"),
	}

	_, err := mockFS.Open("index.html")
	if err == nil {
		t.Error("Expected error")
	}
}

func TestMockFS_NotFound(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{},
	}

	_, err := mockFS.Open("nonexistent.html")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestMockFS_IndexFallback(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{
			"index.html": "<html>Index</html>",
		},
	}

	// Test accessing non-existent path returns index.html
	file, err := mockFS.Open("any/path/")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	defer file.Close()

	content := make([]byte, 100)
	n, _ := file.Read(content)
	if n == 0 {
		t.Error("Expected to read content")
	}
}

// --- MockFS ReadFile and Exists tests ---

func TestMockFS_ReadFile(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{
			"test.txt": "Hello World",
		},
	}

	content, err := mockFS.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if string(content) != "Hello World" {
		t.Errorf("Expected 'Hello World', got '%s'", string(content))
	}
}

func TestMockFS_ReadFile_NotFound(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{},
	}

	_, err := mockFS.ReadFile("nonexistent.txt")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestMockFS_Exists(t *testing.T) {
	mockFS := &MockFS{
		Files: map[string]string{
			"exists.txt": "content",
		},
	}

	if !mockFS.Exists("exists.txt") {
		t.Error("Expected exists.txt to exist")
	}
	if mockFS.Exists("nonexistent.txt") {
		t.Error("Expected nonexistent.txt to not exist")
	}
}

// --- Webmail/Admin handler tests with mock FS ---

func TestHandleWebmail_WithMockFS(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFS := &MockFS{
		Files: map[string]string{
			"index.html": "<!doctype html><html>Webmail</html>",
		},
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, nil, mockFS, nil)

	req := httptest.NewRequest(http.MethodGet, "/webmail/", nil)
	w := httptest.NewRecorder()

	server.handleWebmail(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandleWebmail_WithMockFSError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFS := &MockFS{
		OpenError: errors.New("mock fs open error"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, nil, mockFS, nil)

	req := httptest.NewRequest(http.MethodGet, "/webmail/", nil)
	w := httptest.NewRecorder()

	server.handleWebmail(w, req)

	// Should return 500 on OpenError
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestHandleAdmin_WithMockFS(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFS := &MockFS{
		Files: map[string]string{
			"index.html": "<!doctype html><html>Admin</html>",
		},
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, nil, nil, mockFS)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()

	server.handleAdmin(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandleAdmin_WithMockFSError(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	mockFS := &MockFS{
		OpenError: errors.New("mock fs open error"),
	}

	server := NewServerWithInterfaces(database, nil, Config{}, nil, nil, nil, nil, mockFS)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()

	server.handleAdmin(w, req)

	// Should return 500 on OpenError
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}
