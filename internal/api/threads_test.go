package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Test handleThreads
func TestHandleThreads_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result ThreadListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.Limit != 20 {
		t.Errorf("Expected default limit 20, got %d", result.Limit)
	}

	if result.Offset != 0 {
		t.Errorf("Expected default offset 0, got %d", result.Offset)
	}
}

func TestHandleThreads_WithParams(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads?limit=50&offset=10&mailbox=Sent", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result ThreadListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.Limit != 50 {
		t.Errorf("Expected limit 50, got %d", result.Limit)
	}

	if result.Offset != 10 {
		t.Errorf("Expected offset 10, got %d", result.Offset)
	}
}

func TestHandleThreads_InvalidLimit(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Limit > 100 should be capped to default (20)
	req := httptest.NewRequest("GET", "/api/v1/threads?limit=200", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result ThreadListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result.Limit != 20 {
		t.Errorf("Expected default limit 20 for invalid limit, got %d", result.Limit)
	}
}

func TestHandleThreads_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/threads", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreads(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleThreads_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleThreads(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleThreadDetail
func TestHandleThreadDetail_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Path format: /api/v1/threads/{id}
	req := httptest.NewRequest("GET", "/api/v1/threads/thread-123?mailbox=INBOX", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDetail(w, req)

	// Returns 404 because the stub returns empty messages
	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleThreadDetail_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Empty thread ID after the prefix
	req := httptest.NewRequest("GET", "/api/v1/threads/", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleThreadDetail_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/threads/thread-123", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDetail(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleThreadDetail_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/thread-123", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleThreadDetail(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleThreadSearch
func TestHandleThreadSearch_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/search?q=test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadSearch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result["query"] != "test" {
		t.Errorf("Expected query 'test', got %v", result["query"])
	}
}

func TestHandleThreadSearch_MissingQuery(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/search", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadSearch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleThreadSearch_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/threads/search?q=test", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadSearch(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleThreadSearch_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/search?q=test", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleThreadSearch(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleThreadMarkRead
func TestHandleThreadMarkRead_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Path format: /api/v1/threads/{id}/read
	req := httptest.NewRequest("POST", "/api/v1/threads/thread-123/read?mailbox=INBOX", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadMarkRead(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if result["status"] != "success" {
		t.Errorf("Expected status 'success', got %s", result["status"])
	}
}

func TestHandleThreadMarkRead_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Path with empty thread ID: /api/v1/threads//read
	req := httptest.NewRequest("POST", "/api/v1/threads//read", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadMarkRead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleThreadMarkRead_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/thread-123/read", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadMarkRead(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleThreadMarkRead_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/threads/thread-123/read", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleThreadMarkRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleThreadDelete
func TestHandleThreadDelete_Success(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/threads/thread-123?mailbox=INBOX", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDelete(w, req)

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

func TestHandleThreadDelete_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	// Empty thread ID
	req := httptest.NewRequest("DELETE", "/api/v1/threads/", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleThreadDelete_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/thread-123", nil)
	// The handler checks method internally, but handleThreadPath routes based on method
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadDelete(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleThreadDelete_Unauthorized(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/threads/thread-123", nil)
	// No user context
	w := httptest.NewRecorder()

	server.handleThreadDelete(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// Test handleThreadPath router
func TestHandleThreadPath_Get(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/threads/thread-123", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadPath(w, req)

	// Returns 404 because stub returns empty messages
	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleThreadPath_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("DELETE", "/api/v1/threads/thread-123", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadPath(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleThreadPath_MarkRead(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/threads/thread-123/read", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadPath(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleThreadPath_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	server := NewTestServer(t, tmpDir)

	req := httptest.NewRequest("PATCH", "/api/v1/threads/thread-123", nil)
	req = req.WithContext(withUser(req.Context(), "user@example.com"))
	w := httptest.NewRecorder()

	server.handleThreadPath(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Test decodeJSON helper
func TestDecodeJSON(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name     string
		json     string
		wantErr  bool
		expected TestStruct
	}{
		{
			name:     "valid JSON",
			json:     `{"name":"test","value":42}`,
			wantErr:  false,
			expected: TestStruct{Name: "test", Value: 42},
		},
		{
			name:    "invalid JSON",
			json:    `{"name":"test","value":}`,
			wantErr: true,
		},
		{
			name:    "unknown fields not allowed",
			json:    `{"name":"test","value":42,"extra":"field"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", bytes.NewReader([]byte(tt.json)))
			req.Header.Set("Content-Type", "application/json")

			var result TestStruct
			err := decodeJSON(req, &result)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %+v, got %+v", tt.expected, result)
				}
			}
		})
	}
}
