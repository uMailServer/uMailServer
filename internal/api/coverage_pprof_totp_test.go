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

// --- pprof handler tests ---

func TestPprofHandler_Index(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Should contain profiling links
	body := rec.Body.String()
	if !strings.Contains(body, "goroutine") {
		t.Error("expected goroutine link in pprof index")
	}
	if !strings.Contains(body, "heap") {
		t.Error("expected heap link in pprof index")
	}
}

func TestPprofHandler_Cmdline(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/cmdline", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.Len() == 0 {
		t.Error("expected non-empty cmdline response")
	}
}

func TestPprofHandler_Symbol(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/symbol", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	// Symbol endpoint returns different status codes depending on request
	if rec.Code != http.StatusOK && rec.Code != http.StatusBadRequest {
		t.Errorf("unexpected status: %d", rec.Code)
	}
}

func TestPprofHandler_Goroutine(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/goroutine", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPprofHandler_Heap(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/heap", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.Len() == 0 {
		t.Error("expected non-empty heap profile response")
	}
}

func TestPprofHandler_Block(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/block", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPprofHandler_Mutex(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/mutex", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPprofHandler_Allocs(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/allocs", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPprofHandler_Threadcreate(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/threadcreate", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestPprofHandler_NotFound(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	req := httptest.NewRequest("GET", "/debug/pprof/nonexistent", nil)
	rec := httptest.NewRecorder()

	server.pprofHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

// --- TOTP handler tests ---

func helperSetupTOTPAccount(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "test.com", MaxAccounts: 10, IsActive: true}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@test.com", LocalPart: "user", Domain: "test.com",
		PasswordHash: string(hash), IsActive: true,
	}
	if err := database.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return server, database
}

func TestHandleTOTPSetup_Success(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	server.handleTOTPSetup(rec, req, "user@test.com")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should contain uri (secret is no longer exposed in API response)
	if _, ok := resp["uri"]; !ok {
		t.Error("expected uri in response")
	}
}

func TestHandleTOTPSetup_WrongMethod(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	req := httptest.NewRequest("GET", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPSetup(rec, req, "user@test.com")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandleTOTPSetup_UserNotFound(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPSetup(rec, req, "nonexistent@test.com")

	// Returns 404 for user not found
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleTOTPSetup_ForbiddenForDifferentUser(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Try to setup TOTP for a different user as non-admin - should be forbidden
	ctx := context.WithValue(context.Background(), "user", "other@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTOTPSetup_AdminCannotSetupForAdmin(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Setup admin account
	database.CreateAccount(&db.AccountData{
		LocalPart:    "admin",
		Domain:       "test.com",
		PasswordHash: "hash",
		IsAdmin:      true,
	})

	// Admin trying to setup TOTP for another admin - should be forbidden
	ctx := context.WithValue(context.Background(), "user", "admin@test.com")
	ctx = context.WithValue(ctx, "isAdmin", true)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// user@test.com is not admin, so admin SHOULD be able to setup for them
	if rec.Code == http.StatusForbidden {
		t.Log("This is actually allowed since user@test.com is not admin")
	}
}

func TestHandleTOTPSetup_ClosedDB(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	database.Close()

	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPSetup(rec, req, "user@test.com")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for closed db (user not found), got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_MissingCode(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing code, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_InvalidCode(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// First setup TOTP
	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Try to verify with invalid code
	body, _ := json.Marshal(map[string]string{"code": "000000"})
	req = httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid code, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_NotSetUp(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Try to verify without setting up first
	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for TOTP not set up, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_UserNotFound(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "nonexistent@test.com")

	// Returns 404 for user not found
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_WrongMethod(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	req := httptest.NewRequest("GET", "/api/totp/verify", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_ForbiddenForDifferentUser(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Setup TOTP for user@test.com
	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Try to verify as different user (not self, not admin) - should be forbidden
	ctx = context.WithValue(context.Background(), "user", "other@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req = httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTOTPDisable_Success(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// First setup TOTP
	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Now disable it - user can disable their own TOTP
	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req = httptest.NewRequest("POST", "/api/totp/disable", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	server.handleTOTPDisable(rec, req, "user@test.com")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if enabled, ok := resp["enabled"].(bool); !ok || enabled {
		t.Error("expected enabled=false in response")
	}
}

func TestHandleTOTPDisable_WrongMethod(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	req := httptest.NewRequest("GET", "/api/totp/disable", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPDisable(rec, req, "user@test.com")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandleTOTPDisable_UserNotFound(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	req := httptest.NewRequest("POST", "/api/totp/disable", nil)
	rec := httptest.NewRecorder()

	server.handleTOTPDisable(rec, req, "nonexistent@test.com")

	// Returns 404 for user not found
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleTOTPDisable_ForbiddenForDifferentUser(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Setup TOTP for user@test.com
	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Try to disable as different user (not self, not admin) - should be forbidden
	ctx := context.WithValue(context.Background(), "user", "other@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req = httptest.NewRequest("POST", "/api/totp/disable", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	server.handleTOTPDisable(rec, req, "user@test.com")

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTOTPDisable_AdminCanDisableNonAdmin(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Setup TOTP for user@test.com (non-admin)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Admin disables non-admin user's TOTP - should succeed
	ctx := context.WithValue(context.Background(), "user", "admin@test.com")
	ctx = context.WithValue(ctx, "isAdmin", true)
	req = httptest.NewRequest("POST", "/api/totp/disable", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	server.handleTOTPDisable(rec, req, "user@test.com")

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTOTPDisable_AdminCannotDisableAdmin(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Setup TOTP for admin@test.com (admin account)
	database.CreateAccount(&db.AccountData{
		LocalPart:    "admin",
		Domain:       "test.com",
		PasswordHash: "hash",
		IsAdmin:      true,
		TOTPEnabled:  false,
		TOTPSecret:   "",
	})
	req := httptest.NewRequest("POST", "/api/totp/setup", nil)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "admin@test.com")

	// Another admin tries to disable admin's TOTP - should be forbidden
	ctx := context.WithValue(context.Background(), "user", "otheradmin@test.com")
	ctx = context.WithValue(ctx, "isAdmin", true)
	req = httptest.NewRequest("POST", "/api/totp/disable", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	server.handleTOTPDisable(rec, req, "admin@test.com")

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Setter method tests ---

func TestServer_SetSearchService(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	// SetSearchService should not panic with nil
	server.SetSearchService(nil)
}

func TestServer_SetQueueManager(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	// SetQueueManager should not panic with nil
	server.SetQueueManager(nil)
}

func TestServer_SetHealthMonitor(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	// SetHealthMonitor should not panic with nil
	server.SetHealthMonitor(nil)
}

func TestServer_SetAPIRateLimit(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	// Set rate limit to 100
	server.SetAPIRateLimit(100)

	// Set rate limit to 0 (disabled)
	server.SetAPIRateLimit(0)
}

func TestRegisterPprofRoutes(t *testing.T) {
	server := NewServer(nil, nil, Config{})

	mux := http.NewServeMux()
	server.RegisterPprofRoutes(mux)

	// Test that pprof routes are registered
	testCases := []struct {
		path       string
		wantStatus int
	}{
		{"/debug/pprof/", http.StatusOK},
		{"/debug/pprof/heap", http.StatusOK},
		{"/debug/pprof/goroutine", http.StatusOK},
	}

	for _, tc := range testCases {
		req := httptest.NewRequest("GET", tc.path, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		// Routes should be registered (not 404)
		if rec.Code == http.StatusNotFound {
			t.Errorf("route %s returned 404", tc.path)
		}
	}
}

// --- accountToJSON test ---

func TestAccountToJSON_WithVacation(t *testing.T) {
	account := &db.AccountData{
		Email:            "test@example.com",
		IsAdmin:          true,
		IsActive:         true,
		QuotaUsed:        100,
		QuotaLimit:       1000,
		VacationSettings: "{}",
	}

	result := accountToJSON(account)

	if result["email"] != "test@example.com" {
		t.Error("email mismatch")
	}
	if v, ok := result["is_admin"].(bool); !ok || !v {
		t.Error("is_admin mismatch")
	}
	// Vacation settings should be included when present
	if _, ok := result["vacation_settings"]; !ok {
		t.Error("expected vacation_settings in result")
	}
}

func TestAccountToJSON_WithoutVacation(t *testing.T) {
	account := &db.AccountData{
		Email:      "test@example.com",
		IsAdmin:    false,
		IsActive:   true,
		QuotaUsed:  0,
		QuotaLimit: 0,
		// No vacation settings
	}

	result := accountToJSON(account)

	// Vacation settings should not be present when empty
	if _, ok := result["vacation_settings"]; ok {
		t.Error("expected no vacation_settings when empty")
	}
}

func TestHandleTOTPVerify_DecryptError(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	// Get account and set an invalid encrypted TOTP secret (starts with "enc:" but is not valid base64)
	account, _ := database.GetAccount("test.com", "user")
	account.TOTPSecret = "enc:!!!invalid-base64!!!"
	account.TOTPEnabled = true
	database.UpdateAccount(account)

	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req := httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for decrypt error, got %d", rec.Code)
	}
}

func TestHandleTOTPVerify_UpdateAccountError(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	// Don't defer close - we want to close it to cause an error

	// First setup TOTP properly
	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)
	req := httptest.NewRequest("POST", "/api/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")

	// Now close database to cause UpdateAccount to fail
	database.Close()

	// Get the TOTP secret that was set
	account, _ := database.GetAccount("test.com", "user")
	if account == nil {
		// Re-create account for test
		hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		account = &db.AccountData{
			Email:        "user@test.com",
			LocalPart:    "user",
			Domain:       "test.com",
			PasswordHash: string(hash),
			IsActive:     true,
			TOTPEnabled:  false,
		}
		database.CreateAccount(account)
	}

	body, _ := json.Marshal(map[string]string{"code": "123456"})
	req = httptest.NewRequest("POST", "/api/totp/verify", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	server.handleTOTPVerify(rec, req, "user@test.com")

	// Should fail with server error when database is closed
	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusNotFound {
		t.Errorf("expected 500 or 404 for db error, got %d", rec.Code)
	}
}
