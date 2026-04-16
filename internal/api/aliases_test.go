package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// setupAliasTestServer creates a test server with database for alias tests
func setupAliasTestServer(t *testing.T) (*Server, *db.DB, func()) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	config := Config{
		JWTSecret:   "test-secret",
		TokenExpiry: time.Hour,
	}

	server := NewServer(database, nil, config)

	cleanup := func() {
		database.Close()
	}

	return server, database, cleanup
}

// TestHandleAliases_GET tests listing aliases via GET
func TestHandleAliases_GET(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases", nil)
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 alias, got %d", len(result))
	}
}

// TestHandleAliases_POST tests creating an alias via POST
func TestHandleAliases_POST(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	reqBody := map[string]interface{}{"alias": "alias@example.com", "target": "user@example.com", "is_active": true}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}
}

// TestHandleAliases_InvalidMethod tests that invalid methods are rejected
func TestHandleAliases_InvalidMethod(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/aliases", nil)
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestHandleAliases_POST_InvalidBody tests POST with invalid JSON
func TestHandleAliases_POST_InvalidBody(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_MissingAlias tests POST without alias
func TestHandleAliases_POST_MissingAlias(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	reqBody := map[string]interface{}{"target": "user@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_InvalidAliasFormat tests POST with invalid alias format
func TestHandleAliases_POST_InvalidAliasFormat(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	reqBody := map[string]interface{}{"alias": "invalid", "target": "user@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_MissingTarget tests POST without target
func TestHandleAliases_POST_MissingTarget(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	reqBody := map[string]interface{}{"alias": "alias@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_InvalidTargetFormat tests POST with invalid target format
func TestHandleAliases_POST_InvalidTargetFormat(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	reqBody := map[string]interface{}{"alias": "alias@example.com", "target": "invalid"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_NonexistentDomain tests POST with non-existent domain
func TestHandleAliases_POST_NonexistentDomain(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	reqBody := map[string]interface{}{"alias": "alias@nonexistent.com", "target": "user@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliases_POST_NonexistentTarget tests POST with non-existent target account
func TestHandleAliases_POST_NonexistentTarget(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	reqBody := map[string]interface{}{"alias": "alias@example.com", "target": "nonexistent@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/aliases", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliasDetail_GET tests getting a specific alias
func TestHandleAliasDetail_GET(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases/alias@example.com", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// TestHandleAliasDetail_GET_InvalidAlias tests GET with invalid alias format
func TestHandleAliasDetail_GET_InvalidAlias(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases/invalid", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliasDetail_GET_NotFound tests GET for non-existent alias
func TestHandleAliasDetail_GET_NotFound(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases/nonexistent@example.com", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// TestHandleAliasDetail_PUT tests updating an alias
func TestHandleAliasDetail_PUT(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create another account for target
	account2 := &db.AccountData{
		Email:        "user2@example.com",
		LocalPart:    "user2",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account2); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	reqBody := map[string]interface{}{"target": "user2@example.com", "is_active": false}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/aliases/alias@example.com", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestHandleAliasDetail_PUT_InvalidBody tests PUT with invalid JSON
func TestHandleAliasDetail_PUT_InvalidBody(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/aliases/alias@example.com", bytes.NewReader([]byte("invalid")))
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliasDetail_PUT_NotFound tests PUT for non-existent alias
func TestHandleAliasDetail_PUT_NotFound(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	reqBody := map[string]interface{}{"target": "user@example.com"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/aliases/nonexistent@example.com", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// TestHandleAliasDetail_PUT_InvalidTarget tests PUT with invalid target format
func TestHandleAliasDetail_PUT_InvalidTarget(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	reqBody := map[string]interface{}{"target": "invalid"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/aliases/alias@example.com", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliasDetail_DELETE tests deleting an alias
func TestHandleAliasDetail_DELETE(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Create an account
	account := &db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: "hash",
		IsActive:     true,
		CreatedAt:    time.Now(),
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("failed to create account: %v", err)
	}

	// Create an alias
	alias := &db.AliasData{
		Alias:     "alias",
		Domain:    "example.com",
		Target:    "user@example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
	}
	if err := database.CreateAlias(alias); err != nil {
		t.Fatalf("failed to create alias: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/aliases/alias@example.com", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}
}

// TestHandleAliasDetail_DELETE_InvalidAlias tests DELETE with invalid alias format
func TestHandleAliasDetail_DELETE_InvalidAlias(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/aliases/invalid", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestHandleAliasDetail_InvalidMethod tests that invalid methods are rejected
func TestHandleAliasDetail_InvalidMethod(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/aliases/alias@example.com", nil)
	w := httptest.NewRecorder()

	server.handleAliasDetail(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

// TestListAliases_Empty tests listing aliases when none exist
func TestListAliases_Empty(t *testing.T) {
	server, _, cleanup := setupAliasTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases", nil)
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(result))
	}
}

// TestListAliases_DBError tests listAliases when database returns error
func TestListAliases_DBError(t *testing.T) {
	server, database, cleanup := setupAliasTestServer(t)
	defer cleanup()

	// Create a domain first
	domain := &db.DomainData{Name: "example.com", IsActive: true, CreatedAt: time.Now()}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Close database to trigger error
	database.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/aliases", nil)
	w := httptest.NewRecorder()

	server.handleAliases(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}
