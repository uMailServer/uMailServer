package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/vacation"
)

// Test handleVacation dispatcher
func TestHandleVacation_Get(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleVacation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result VacationConfig
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.Subject != "Out of Office" {
		t.Errorf("Expected default subject, got %s", result.Subject)
	}
}

func TestHandleVacation_Put(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	startDate := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	endDate := time.Now().Add(7 * 24 * time.Hour).Format(time.RFC3339)

	body := VacationConfig{
		Enabled:          true,
		StartDate:        &startDate,
		EndDate:          &endDate,
		Subject:          "Vacation",
		Message:          "I'm on vacation",
		SendInterval:     24,
		ExcludeAddresses: []string{"boss@example.com"},
		IgnoreLists:      true,
		IgnoreBulk:       true,
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/vacation", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleVacation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleVacation_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleVacation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result["status"] != "deleted" {
		t.Errorf("Expected status 'deleted', got %s", result["status"])
	}
}

func TestHandleVacation_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("PATCH", "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleVacation(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test handleGetVacation
func TestHandleGetVacation_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/vacation", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleGetVacation(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetVacation_WithDates(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/vacation", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleGetVacation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result VacationConfig
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Stub implementation returns default values with empty dates
	if result.Subject != "Out of Office" {
		t.Errorf("Expected default subject 'Out of Office', got %s", result.Subject)
	}

	// Dates should be nil/zero for default config
	if result.StartDate != nil {
		t.Error("Expected start_date to be nil for default config")
	}
}

// Test handleSetVacation
func TestHandleSetVacation_InvalidBody(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("PUT", "/api/v1/vacation", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleSetVacation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetVacation_MissingSubject(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := VacationConfig{
		Enabled: true,
		Subject: "",
		Message: "Test message",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/vacation", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleSetVacation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetVacation_MissingMessage(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := VacationConfig{
		Enabled: true,
		Subject: "Test Subject",
		Message: "",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/vacation", bytes.NewReader(bodyJSON))
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleSetVacation(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetVacation_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	body := VacationConfig{
		Enabled: true,
		Subject: "Test",
		Message: "Test message",
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest("PUT", "/api/v1/vacation", bytes.NewReader(bodyJSON))
	// No user context
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handleSetVacation(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleDeleteVacation
func TestHandleDeleteVacation_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/vacation", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleDeleteVacation(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleAdminVacations
func TestHandleAdminVacations_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/admin/vacations", nil)
	req = req.WithContext(withUser(req.Context(), "admin@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), true))
	w := httptest.NewRecorder()

	server.handleAdminVacations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if _, ok := result["active_vacations"]; !ok {
		t.Error("Expected active_vacations in response")
	}
}

func TestHandleAdminVacations_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/admin/vacations", nil)
	req = req.WithContext(withUser(req.Context(), "admin@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), true))
	w := httptest.NewRecorder()

	server.handleAdminVacations(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleAdminVacations_NotAdmin(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/admin/vacations", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	req = req.WithContext(withIsAdmin(req.Context(), false))
	w := httptest.NewRecorder()

	server.handleAdminVacations(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// Test vacation helper functions
func TestGetVacationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	config, err := server.getVacationConfig("user@example.com")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if config.Subject != "Out of Office" {
		t.Errorf("Expected default subject 'Out of Office', got %s", config.Subject)
	}

	if config.SendInterval != 7*24*time.Hour {
		t.Errorf("Expected default interval 7 days, got %v", config.SendInterval)
	}
}

func TestSetVacationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	config := &vacation.Config{
		Enabled:      true,
		Subject:      "Custom Subject",
		Message:      "Custom Message",
		SendInterval: 48 * time.Hour,
	}

	err := server.setVacationConfig("user@example.com", config)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestDeleteVacationConfig(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	err := server.deleteVacationConfig("user@example.com")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestListActiveVacations(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	vacations := server.listActiveVacations()
	if vacations == nil {
		t.Error("Expected empty slice, got nil")
	}

	if len(vacations) != 0 {
		t.Errorf("Expected 0 vacations, got %d", len(vacations))
	}
}

// Helper function
