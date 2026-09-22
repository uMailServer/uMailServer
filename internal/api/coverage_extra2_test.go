package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// TestHandleRefresh_NilContext tests handleRefresh when context values are nil.
func TestHandleRefresh_NilContext(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "test-secret"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	// No user/isAdmin values in context
	rec := httptest.NewRecorder()
	server.handleRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 even with nil context values, got %d", rec.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["token"] == nil {
		t.Error("Expected token in response")
	}
}

// TestHandleRefresh_InvalidMethod tests handleRefresh with non-POST method.
func TestHandleRefresh_InvalidMethod2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()
	server.handleRefresh(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rec.Code)
	}
}

// TestHandleLogin_InvalidMethod2 tests handleLogin with non-POST method.
func TestHandleLogin_InvalidMethod2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()
	server.handleLogin(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rec.Code)
	}
}

// TestHandleLogin_WrongPassword tests login with wrong password.
func TestHandleLogin_WrongPassword(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	domain := &db.DomainData{Name: "wp.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@wp.com", LocalPart: "user", Domain: "wp.com",
		PasswordHash: string(hash), IsActive: true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"email": "user@wp.com", "password": "wrongpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong password, got %d", rec.Code)
	}
}

// TestUpdateAccount_WithPasswordChange tests updateAccount with password change.
func TestUpdateAccount_WithPasswordChange(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "pwchg.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	account := &db.AccountData{
		Email: "u@pwchg.com", LocalPart: "u", Domain: "pwchg.com",
		PasswordHash: "oldhash", IsActive: true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"password": "newpassword", "is_admin": false, "is_active": true,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/u@pwchg.com", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), "user", "u@pwchg.com"))
	rec := httptest.NewRecorder()
	server.updateAccount(rec, req, "u@pwchg.com")

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	updated, _ := database.GetAccount("pwchg.com", "u")
	if updated.PasswordHash == "oldhash" {
		t.Error("Expected password hash to be updated")
	}
	// Verify the new hash is valid bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("newpassword")); err != nil {
		t.Errorf("New password hash does not match: %v", err)
	}
}

// TestUpdateAccount_InvalidBody tests updateAccount with malformed JSON.
func TestUpdateAccount_InvalidBody(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "inv.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	account := &db.AccountData{
		Email: "u@inv.com", LocalPart: "u", Domain: "inv.com",
		PasswordHash: "hash", IsActive: true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/u@inv.com", bytes.NewReader([]byte("invalid json")))
	req = req.WithContext(context.WithValue(req.Context(), "user", "u@inv.com"))
	rec := httptest.NewRecorder()
	server.updateAccount(rec, req, "u@inv.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid body, got %d", rec.Code)
	}
}

// TestHandleSearch_WithLimitOffset tests handleSearch with limit and offset parameters.
func TestHandleSearch_WithLimitOffset(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=hello&folder=INBOX&limit=5&offset=10", nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	ctx = context.WithValue(ctx, "isAdmin", true)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	// Without search service wired, expect 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rec.Code)
	}
}

// TestRetryQueueEntry_Success2 tests retryQueueEntry successful path.
func TestRetryQueueEntry_Success2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	entry := &db.QueueEntry{
		ID:         "retry-s2",
		From:       "s@test.com",
		To:         []string{"r@test.com"},
		Status:     "failed",
		RetryCount: 3,
		LastError:  "conn refused",
		NextRetry:  time.Now().Add(1 * time.Hour),
		CreatedAt:  time.Now(),
	}
	if err := database.Enqueue(entry); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/queue/retry-s2", nil)
	rec := httptest.NewRecorder()
	server.retryQueueEntry(rec, req, "retry-s2")

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	json.NewDecoder(rec.Body).Decode(&result)
	if result["status"] != "retrying" {
		t.Errorf("Expected status=retrying, got %s", result["status"])
	}

	// Verify the entry was updated
	updated, _ := database.GetQueueEntry("retry-s2")
	if updated.Status != "pending" {
		t.Errorf("Expected status=pending after retry, got %s", updated.Status)
	}
	if updated.RetryCount != 0 {
		t.Errorf("Expected retry_count=0 after retry, got %d", updated.RetryCount)
	}
}

// TestUpdateDomain_Success tests updateDomain with valid data.
func TestUpdateDomain_Success2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "updsucc.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{"max_accounts": 50, "is_active": false})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/domains/updsucc.com", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.updateDomain(rec, req, "updsucc.com")

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["max_accounts"] != 50.0 {
		t.Errorf("Expected max_accounts=50, got %v", result["max_accounts"])
	}
	if v, ok := result["is_active"].(bool); !ok || v {
		t.Errorf("Expected is_active=false, got %v", result["is_active"])
	}
}

// TestUpdateDomain_InvalidBody tests updateDomain with malformed JSON.
func TestUpdateDomain_InvalidBody(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "invdom.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/domains/invdom.com", bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	server.updateDomain(rec, req, "invdom.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

// TestHandleLogin_InvalidBody tests handleLogin with malformed JSON.
func TestHandleLogin_InvalidBody2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("bad json")))
	rec := httptest.NewRecorder()
	server.handleLogin(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400, got %d", rec.Code)
	}
}

// TestHandleSearch_InvalidLimit tests handleSearch with non-numeric limit.
func TestHandleSearch_InvalidLimit(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&limit=abc&offset=xyz", nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	// Without search service wired, expect 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rec.Code)
	}
}

// TestHandleSearch_NegativeLimit tests handleSearch with negative limit (should use default).
func TestHandleSearch_NegativeLimit(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	// Negative limit should be ignored and default used
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&limit=-5", nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	// Without search service wired, expect 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rec.Code)
	}
}

// TestHandleSearch_NegativeOffset tests handleSearch with negative offset (should use 0).
func TestHandleSearch_NegativeOffset(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	// Negative offset should be ignored and 0 used
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&offset=-10", nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	// Without search service wired, expect 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rec.Code)
	}
}

// TestHandleSearch_QueryTooLong tests handleSearch with query exceeding 500 chars.
func TestHandleSearch_QueryTooLong(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	// Query too long (>500 chars)
	longQuery := strings.Repeat("a", 501)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q="+longQuery, nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for too-long query, got %d", rec.Code)
	}
}

// TestHandleSearch_LimitCappedAt100 tests handleSearch with limit > 100 (should cap).
func TestHandleSearch_LimitCappedAt100(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{JWTSecret: "secret"})

	// Limit > 100 should be capped to 100
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&limit=200", nil)
	ctx := context.WithValue(req.Context(), "user", "test@example.com")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleSearch(rec, req)

	// Without search service wired, expect 503
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", rec.Code)
	}
}

// TestCreateDomain_EmptyName tests createDomain with empty name.
func TestCreateDomain_EmptyName(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	body, _ := json.Marshal(map[string]interface{}{"name": "", "max_accounts": 10})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.createDomain(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty name, got %d", rec.Code)
	}
}

// TestCreateAccount_MissingFields tests createAccount with missing email/password.
func TestCreateAccount_MissingFields(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	// Missing email
	body, _ := json.Marshal(map[string]string{"password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.createAccount(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing email, got %d", rec.Code)
	}

	// Missing password
	body, _ = json.Marshal(map[string]string{"email": "test@test.com"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	server.createAccount(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing password, got %d", rec.Code)
	}
}

// TestHandleWebmail tests the webmail handler.
func TestHandleWebmail2(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	req := httptest.NewRequest(http.MethodGet, "/webmail", nil)
	rec := httptest.NewRecorder()
	server.handleWebmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("Expected text/html content type, got %s", rec.Header().Get("Content-Type"))
	}
}

// TestUpdateDomain_NegativeMaxAccounts tests max_accounts bounds.
func TestUpdateDomain_NegativeMaxAccounts(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "negdom.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/domains/negdom.com", bytes.NewReader([]byte(`{"max_accounts":-1,"is_active":true}`)))
	rec := httptest.NewRecorder()
	server.updateDomain(rec, req, "negdom.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative max_accounts, got %d", rec.Code)
	}
}

// TestUpdateDomain_MaxAccountsTooHigh tests upper bound validation.
func TestUpdateDomain_MaxAccountsTooHigh(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "highdom.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/domains/highdom.com", bytes.NewReader([]byte(`{"max_accounts":2000000,"is_active":true}`)))
	rec := httptest.NewRecorder()
	server.updateDomain(rec, req, "highdom.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for max_accounts too high, got %d", rec.Code)
	}
}

// TestUpdateDomain_DeactivateWithAccounts tests deactivation blocked when accounts exist.
func TestUpdateDomain_DeactivateWithAccounts(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	domain := &db.DomainData{Name: "actdom.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "user@actdom.com", Domain: "actdom.com", LocalPart: "user", PasswordHash: "hash"}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/domains/actdom.com", bytes.NewReader([]byte(`{"max_accounts":10,"is_active":false}`)))
	rec := httptest.NewRecorder()
	server.updateDomain(rec, req, "actdom.com")

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409 for deactivation with accounts, got %d", rec.Code)
	}
}

// TestCreateDomain_MaxAccountsTooHigh tests upper bound on create.
func TestCreateDomain_MaxAccountsTooHigh(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	server := NewServer(database, nil, Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/domains", bytes.NewReader([]byte(`{"name":"huge.com","max_accounts":2000000}`)))
	rec := httptest.NewRecorder()
	server.createDomain(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for max_accounts too high on create, got %d", rec.Code)
	}
}
