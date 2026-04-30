package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// TestIsTOTPLockedOut_NoFailures tests TOTP lockout with no failures
func TestIsTOTPLockedOut_NoFailures(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// No failures recorded - should not be locked out
	if server.isTOTPLockedOut("user@example.com") {
		t.Error("expected not locked out with no failures")
	}
}

// TestIsTOTPLockedOut_BelowThreshold tests TOTP lockout below threshold
func TestIsTOTPLockedOut_BelowThreshold(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Record 4 failures (below threshold of 5)
	for i := 0; i < 4; i++ {
		server.recordTOTPFailure("user@example.com")
	}

	// Should not be locked out yet
	if server.isTOTPLockedOut("user@example.com") {
		t.Error("expected not locked out below threshold")
	}
}

// TestIsTOTPLockedOut_AtThreshold tests TOTP lockout at threshold
func TestIsTOTPLockedOut_AtThreshold(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Record 5 failures (at threshold)
	for i := 0; i < 5; i++ {
		server.recordTOTPFailure("user@example.com")
	}

	// Should be locked out
	if !server.isTOTPLockedOut("user@example.com") {
		t.Error("expected locked out at threshold")
	}
}

// TestIsTOTPLockedOut_AfterLockoutDuration tests TOTP lockout after duration expires
func TestIsTOTPLockedOut_AfterLockoutDuration(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Manually set an old failure time
	server.totpMu.Lock()
	server.totpAttempts = map[string]*totpAttempt{
		"user@example.com": {
			count:    10,                                // Well over threshold
			lastSeen: time.Now().Add(-10 * time.Minute), // But 10 minutes ago
		},
	}
	server.totpMu.Unlock()

	// Should not be locked out anymore (lockout duration expired)
	if server.isTOTPLockedOut("user@example.com") {
		t.Error("expected not locked out after duration expired")
	}
}

// TestRecordTOTPFailure_CreatesEntry tests that recordTOTPFailure creates entry
func TestRecordTOTPFailure_CreatesEntry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Record first failure
	server.recordTOTPFailure("user@example.com")

	// Should have 1 failure
	server.totpMu.Lock()
	attempt := server.totpAttempts["user@example.com"]
	server.totpMu.Unlock()

	if attempt == nil || attempt.count != 1 {
		t.Errorf("expected count 1, got %v", attempt)
	}
}

// TestRecordTOTPFailure_IncrementsCount tests that recordTOTPFailure increments count
func TestRecordTOTPFailure_IncrementsCount(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Record multiple failures
	server.recordTOTPFailure("user@example.com")
	server.recordTOTPFailure("user@example.com")
	server.recordTOTPFailure("user@example.com")

	// Should have 3 failures
	server.totpMu.Lock()
	attempt := server.totpAttempts["user@example.com"]
	server.totpMu.Unlock()

	if attempt == nil || attempt.count != 3 {
		t.Errorf("expected count 3, got %v", attempt)
	}
}

// TestClearTOTPFailures tests clearing TOTP failures
func TestClearTOTPFailures(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Record failures
	server.recordTOTPFailure("user@example.com")
	server.recordTOTPFailure("user@example.com")

	// Clear them
	server.clearTOTPFailures("user@example.com")

	// Should not be locked out
	if server.isTOTPLockedOut("user@example.com") {
		t.Error("expected not locked out after clearing")
	}

	// Verify entry is deleted
	server.totpMu.Lock()
	_, exists := server.totpAttempts["user@example.com"]
	server.totpMu.Unlock()

	if exists {
		t.Error("expected TOTP attempt entry to be deleted")
	}
}

// TestClearTOTPFailures_NoEntry tests clearing when no entry exists
func TestClearTOTPFailures_NoEntry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Clear non-existent entry - should not panic
	server.clearTOTPFailures("nonexistent@example.com")
}

// TestClearAccountLoginFailures tests clearing account login failures
func TestClearAccountLoginFailures(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Set up some login failures
	server.accountLoginMu.Lock()
	server.accountLoginAttempts = map[string]*loginAttempt{
		"user@example.com": {count: 3, lastSeen: time.Now()},
	}
	server.accountLoginMu.Unlock()

	// Clear them
	server.clearAccountLoginFailures("user@example.com")

	// Verify entry is deleted
	server.accountLoginMu.Lock()
	_, exists := server.accountLoginAttempts["user@example.com"]
	server.accountLoginMu.Unlock()

	if exists {
		t.Error("expected login attempt entry to be deleted")
	}
}

// TestClearAccountLoginFailures_NoEntry tests clearing when no entry exists
func TestClearAccountLoginFailures_NoEntry(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})

	// Clear non-existent entry - should not panic
	server.clearAccountLoginFailures("nonexistent@example.com")
}

// TestRevokeToken_InMemory tests token revocation in memory
func TestRevokeToken_InMemory(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	tokenHash := "test-token-hash-123"

	// Revoke token
	server.RevokeToken(tokenHash, time.Now().Add(time.Hour))

	// Should be revoked
	if !server.IsTokenRevoked(tokenHash) {
		t.Error("expected token to be revoked")
	}
}

// TestIsTokenRevoked_NotRevoked tests checking non-revoked token
func TestIsTokenRevoked_NotRevoked(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	tokenHash := "test-token-hash-456"

	// Should not be revoked
	if server.IsTokenRevoked(tokenHash) {
		t.Error("expected token to not be revoked")
	}
}

// TestIsTokenRevoked_Expired tests checking expired revoked token
func TestIsTokenRevoked_Expired(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	tokenHash := "test-token-hash-expired"

	// Manually add expired token
	server.tokenBlacklistMu.Lock()
	server.tokenBlacklist = map[string]time.Time{
		tokenHash: time.Now().Add(-time.Hour), // Expired 1 hour ago
	}
	server.tokenBlacklistMu.Unlock()

	// Should not be revoked anymore (expired and cleaned up)
	if server.IsTokenRevoked(tokenHash) {
		t.Error("expected expired token to not be revoked")
	}

	// Verify it was cleaned up
	server.tokenBlacklistMu.Lock()
	_, exists := server.tokenBlacklist[tokenHash]
	server.tokenBlacklistMu.Unlock()

	if exists {
		t.Error("expected expired token to be cleaned up")
	}
}

// TestCleanupExpiredTokensAuth tests cleaning up expired tokens
func TestCleanupExpiredTokensAuth(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Add expired and valid tokens
	server.tokenBlacklistMu.Lock()
	server.tokenBlacklist = map[string]time.Time{
		"expired-token": time.Now().Add(-time.Hour),
		"valid-token":   time.Now().Add(time.Hour),
	}
	server.tokenBlacklistMu.Unlock()

	// Clean up
	server.CleanupExpiredTokens()

	// Verify expired was removed, valid remains
	server.tokenBlacklistMu.Lock()
	_, expiredExists := server.tokenBlacklist["expired-token"]
	_, validExists := server.tokenBlacklist["valid-token"]
	server.tokenBlacklistMu.Unlock()

	if expiredExists {
		t.Error("expected expired token to be removed")
	}
	if !validExists {
		t.Error("expected valid token to remain")
	}
}

// TestCheckLoginRateLimit_FirstAttempt tests first login attempt
func TestCheckLoginRateLimit_FirstAttempt(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// First attempt should be allowed
	if !server.checkLoginRateLimit("192.168.1.1") {
		t.Error("expected first attempt to be allowed")
	}
}

// TestCheckLoginRateLimit_UnderLimit tests attempts under limit
func TestCheckLoginRateLimit_UnderLimit(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Make 4 attempts (under limit of 5)
	for i := 0; i < 4; i++ {
		if !server.checkLoginRateLimit("192.168.1.1") {
			t.Errorf("expected attempt %d to be allowed", i+1)
		}
	}
}

// TestCheckLoginRateLimit_AtLimit tests attempts at limit
func TestCheckLoginRateLimit_AtLimit(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Make 5 attempts (at limit)
	for i := 0; i < 5; i++ {
		server.checkLoginRateLimit("192.168.1.1")
	}

	// 6th attempt should be blocked
	if server.checkLoginRateLimit("192.168.1.1") {
		t.Error("expected 6th attempt to be blocked")
	}
}

// TestCheckLoginRateLimit_AfterWindow tests attempts after window expires
func TestCheckLoginRateLimit_AfterWindow(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Make 5 attempts to hit limit
	server.loginMu.Lock()
	server.loginAttempts = map[string]*loginAttempt{
		"192.168.1.1": {count: 5, lastSeen: time.Now().Add(-10 * time.Minute)}, // 10 minutes ago
	}
	server.loginMu.Unlock()

	// Should be allowed again (window expired)
	if !server.checkLoginRateLimit("192.168.1.1") {
		t.Error("expected attempt to be allowed after window expired")
	}
}

// TestRecordLoginFailure_CreatesEntry tests recording first failure
func TestRecordLoginFailure_CreatesEntry(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	server.recordLoginFailure("192.168.1.1")

	server.loginMu.Lock()
	attempt := server.loginAttempts["192.168.1.1"]
	server.loginMu.Unlock()

	if attempt == nil || attempt.count != 1 {
		t.Errorf("expected count 1, got %v", attempt)
	}
}

// TestRecordLoginFailure_IncrementsCount tests incrementing failure count
func TestRecordLoginFailure_IncrementsCount(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	server.recordLoginFailure("192.168.1.1")
	server.recordLoginFailure("192.168.1.1")
	server.recordLoginFailure("192.168.1.1")

	server.loginMu.Lock()
	attempt := server.loginAttempts["192.168.1.1"]
	server.loginMu.Unlock()

	if attempt == nil || attempt.count != 3 {
		t.Errorf("expected count 3, got %v", attempt)
	}
}

// TestCheckAccountLoginRateLimit_FirstAttempt tests first account login attempt
func TestCheckAccountLoginRateLimit_FirstAttempt(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	if !server.checkAccountLoginRateLimit("user@example.com") {
		t.Error("expected first attempt to be allowed")
	}
}

// TestCheckAccountLoginRateLimit_AtLimit tests account at limit
func TestCheckAccountLoginRateLimit_AtLimit(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Hit the limit
	server.accountLoginMu.Lock()
	server.accountLoginAttempts = map[string]*loginAttempt{
		"user@example.com": {count: 5, lastSeen: time.Now()},
	}
	server.accountLoginMu.Unlock()

	if server.checkAccountLoginRateLimit("user@example.com") {
		t.Error("expected attempt to be blocked at limit")
	}
}

// TestCheckAccountLoginRateLimit_AfterWindow tests account after window expires
func TestCheckAccountLoginRateLimit_AfterWindow(t *testing.T) {
	server := NewServer(nil, nil, Config{JWTSecret: "test"})

	// Hit the limit but expired
	server.accountLoginMu.Lock()
	server.accountLoginAttempts = map[string]*loginAttempt{
		"user@example.com": {count: 5, lastSeen: time.Now().Add(-10 * time.Minute)},
	}
	server.accountLoginMu.Unlock()

	if !server.checkAccountLoginRateLimit("user@example.com") {
		t.Error("expected attempt to be allowed after window expired")
	}
}

// --- handleLogout tests ---

func TestHandleLogout_MethodNotAllowed(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()

	server.handleLogout(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestHandleLogout_WithCookie(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	// Create request with cookie
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "jwt", Value: token})
	w := httptest.NewRecorder()

	server.handleLogout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Check cookie is cleared
	cookie := w.Result().Cookies()[0]
	if cookie.Name != "jwt" || cookie.MaxAge != -1 {
		t.Errorf("Expected jwt cookie to be cleared, got %v", cookie)
	}
}

func TestHandleLogout_WithBearerToken(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test-secret", TokenExpiry: time.Hour})

	domain := &db.DomainData{Name: "mailtest.com", MaxAccounts: 10, IsActive: true}
	_ = database.CreateDomain(domain)
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	account := &db.AccountData{
		Email: "user@mailtest.com", LocalPart: "user", Domain: "mailtest.com",
		PasswordHash: string(hash), IsActive: true, IsAdmin: true,
	}
	_ = database.CreateAccount(account)

	token := helperLogin(t, server, "user@mailtest.com", "password123")

	// Create request with Authorization header
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	server.handleLogout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestHandleLogout_NoToken(t *testing.T) {
	server := NewServer(nil, nil, Config{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()

	server.handleLogout(w, req)

	// Should still succeed with no token
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 even without token, got %d", w.Code)
	}
}
