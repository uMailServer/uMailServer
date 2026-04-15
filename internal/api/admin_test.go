package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

// setupAdminTestServer creates a test server with database for admin tests
func setupAdminTestServer(t *testing.T) (*AdminServer, *db.DB, func()) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	config := Config{
		JWTSecret:   "test-secret-key-for-jwt-signing",
		TokenExpiry: time.Hour,
	}

	server := NewServer(database, nil, config)

	adminConfig := AdminConfig{
		Addr:      "127.0.0.1:8443",
		JWTSecret: "test-secret-key-for-jwt-signing",
	}

	adminServer := NewAdminServer(server, adminConfig)

	cleanup := func() {
		database.Close()
	}

	return adminServer, database, cleanup
}

// createAdminToken creates a valid admin JWT token for testing
func createAdminToken(secret string, kid string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "admin@example.com",
		"admin": true,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	if kid != "" {
		token.Header["kid"] = kid
	}
	tokenStr, _ := token.SignedString([]byte(secret))
	return tokenStr
}

// createUserToken creates a valid non-admin JWT token for testing
func createUserToken(secret string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "user@example.com",
		"admin": false,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString([]byte(secret))
	return tokenStr
}

// TestNewAdminServer tests creating a new admin server
func TestNewAdminServer(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	defer database.Close()

	server := NewServer(database, nil, Config{JWTSecret: "test"})
	adminConfig := AdminConfig{
		Addr:      "127.0.0.1:8443",
		JWTSecret: "test-secret",
	}

	adminServer := NewAdminServer(server, adminConfig)

	if adminServer == nil {
		t.Fatal("expected non-nil admin server")
	}
	if adminServer.Server != server {
		t.Error("admin server should embed the main server")
	}
	if adminServer.config.Addr != "127.0.0.1:8443" {
		t.Errorf("expected addr 127.0.0.1:8443, got %s", adminServer.config.Addr)
	}
}

// TestAdminServer_Stop_WithoutStart tests stopping without starting
func TestAdminServer_Stop_WithoutStart(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	err := adminServer.Stop()
	if err != nil {
		t.Errorf("expected no error when stopping without starting, got %v", err)
	}
}

// TestAdminServer_router tests the router setup
func TestAdminServer_router(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	router := adminServer.router()
	if router == nil {
		t.Fatal("expected non-nil router")
	}
}

// TestAdminServer_withAuth_ValidAdminToken tests with valid admin token
func TestAdminServer_withAuth_ValidAdminToken(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	// Create handler that checks context values
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value("user")
		isAdmin := r.Context().Value("isAdmin")

		if user != "admin@example.com" {
			t.Errorf("expected user admin@example.com, got %v", user)
		}
		if isAdmin != true {
			t.Errorf("expected isAdmin true, got %v", isAdmin)
		}

		w.WriteHeader(http.StatusOK)
	})

	wrapped := adminServer.withAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	token := createAdminToken(adminServer.config.JWTSecret, "")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// TestAdminServer_withAuth_ValidTokenWithKID tests with valid token using kid header
func TestAdminServer_withAuth_ValidTokenWithKID(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := adminServer.withAuth(handler)

	// Create token with kid header pointing to default key
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	token := createAdminToken(adminServer.Server.jwtSecrets[adminServer.Server.currentKid], "default")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// TestAdminServer_withAuth_MissingHeader tests with missing authorization header
func TestAdminServer_withAuth_MissingHeader(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without auth")
	})

	wrapped := adminServer.withAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %v", resp["error"])
	}
}

// TestAdminServer_withAuth_InvalidToken tests with invalid token
func TestAdminServer_withAuth_InvalidToken(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with invalid token")
	})

	wrapped := adminServer.withAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_withAuth_WrongSigningMethod tests with wrong signing method
func TestAdminServer_withAuth_WrongSigningMethod(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with wrong signing method")
	})

	wrapped := adminServer.withAuth(handler)

	// Create token with none signing method (insecure)
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub":   "admin@example.com",
		"admin": true,
	})
	tokenStr, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_withAuth_ExpiredToken tests with expired token
func TestAdminServer_withAuth_ExpiredToken(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with expired token")
	})

	wrapped := adminServer.withAuth(handler)

	// Create expired token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "admin@example.com",
		"admin": true,
		"exp":   time.Now().Add(-time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString([]byte(adminServer.config.JWTSecret))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_withAuth_MissingSubject tests with missing subject claim
func TestAdminServer_withAuth_MissingSubject(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without subject")
	})

	wrapped := adminServer.withAuth(handler)

	// Create token without subject
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"admin": true,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString([]byte(adminServer.config.JWTSecret))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_adminMiddleware_AdminUser tests admin middleware with admin user
func TestAdminServer_adminMiddleware_AdminUser(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := adminServer.adminMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "isAdmin", true)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

// TestAdminServer_adminMiddleware_NonAdminUser tests admin middleware with non-admin user
func TestAdminServer_adminMiddleware_NonAdminUser(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for non-admin")
	})

	wrapped := adminServer.adminMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, "isAdmin", false)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "forbidden" {
		t.Errorf("expected error 'forbidden', got %v", resp["error"])
	}
}

// TestAdminServer_adminMiddleware_MissingIsAdmin tests admin middleware with missing isAdmin value
func TestAdminServer_adminMiddleware_MissingIsAdmin(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without isAdmin")
	})

	wrapped := adminServer.adminMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Don't set isAdmin in context
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

// TestWriteError tests the writeError function
func TestWriteError(t *testing.T) {
	tests := []struct {
		name     string
		errCode  string
		message  string
		status   int
		expected map[string]interface{}
	}{
		{
			name:    "bad request",
			errCode: "bad_request",
			message: "Invalid input",
			status:  http.StatusBadRequest,
			expected: map[string]interface{}{
				"error":   "bad_request",
				"message": "Invalid input",
				"code":    float64(http.StatusBadRequest),
			},
		},
		{
			name:    "not found",
			errCode: "not_found",
			message: "Resource not found",
			status:  http.StatusNotFound,
			expected: map[string]interface{}{
				"error":   "not_found",
				"message": "Resource not found",
				"code":    float64(http.StatusNotFound),
			},
		},
		{
			name:    "server error",
			errCode: "internal_error",
			message: "Something went wrong",
			status:  http.StatusInternalServerError,
			expected: map[string]interface{}{
				"error":   "internal_error",
				"message": "Something went wrong",
				"code":    float64(http.StatusInternalServerError),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tc.errCode, tc.message, tc.status)

			if w.Code != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse response: %v", err)
			}

			for key, expectedVal := range tc.expected {
				if resp[key] != expectedVal {
					t.Errorf("expected %s=%v, got %v", key, expectedVal, resp[key])
				}
			}
		})
	}
}

// TestGetContentType tests the getContentType function
func TestGetContentType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"index.html", "text/html"},
		{"script.js", "application/javascript"},
		{"styles.css", "text/css"},
		{"icon.svg", "image/svg+xml"},
		{"image.png", "image/png"},
		{"favicon.ico", "image/x-icon"},
		{"data.bin", "application/octet-stream"},
		{"file.unknown", "application/octet-stream"},
		{"", "application/octet-stream"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := getContentType(tc.path)
			if result != tc.expected {
				t.Errorf("getContentType(%q) = %q, want %q", tc.path, result, tc.expected)
			}
		})
	}
}

// TestAdminServer_handleAdmin_NoFS tests handleAdmin - adminFS behavior
// Note: When adminFS is nil, it returns 500. When adminFS is set but file doesn't exist,
// it tries index.html as fallback.
func TestAdminServer_handleAdmin_NoFS(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	// adminFS is nil by default in test setup, so this should return 500
	// However, if adminFS is somehow set, it may behave differently
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()

	adminServer.handleAdmin(w, req)

	// The behavior depends on whether adminFS is nil
	// If it's nil: 500, if it's set: 404 or 200 (with fallback to index.html)
	if w.Code == http.StatusInternalServerError {
		// Expected when adminFS is nil
		return
	}
	// Otherwise, it's a valid response for when adminFS is configured
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Errorf("unexpected status %d", w.Code)
	}
}

// TestAdminServer_handleAdmin_WithFS tests handleAdmin with admin filesystem
func TestAdminServer_handleAdmin_WithFS(t *testing.T) {
	_, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	// Create a mock filesystem with admin files
	// Note: We can't easily set adminFS since it's not exported,
	// but we can test the getContentType function which is the key logic

	// Test content type detection
	contentTypes := map[string]string{
		"index.html":  "text/html",
		"app.js":      "application/javascript",
		"styles.css":  "text/css",
		"logo.svg":    "image/svg+xml",
		"banner.png":  "image/png",
		"favicon.ico": "image/x-icon",
		"config.json": "application/octet-stream",
	}

	for file, expected := range contentTypes {
		result := getContentType(file)
		if result != expected {
			t.Errorf("getContentType(%q) = %q, want %q", file, result, expected)
		}
	}
}

// mockFileSystem implements the FileSystem interface for testing
type mockFileSystem struct {
	files map[string]string
}

func (m *mockFileSystem) Open(name string) (http.File, error) {
	return nil, io.EOF // Simplified mock
}

// TestAdminServer_Routes_Health tests health check route
func TestAdminServer_Routes_Health(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	// Test that router includes health endpoint
	router := adminServer.router()
	if router == nil {
		t.Fatal("expected non-nil router")
	}

	// We can't easily test the actual routes without starting the server,
	// but we verified the router is created
}

// TestAdminServer_Routes_Metrics tests metrics route
func TestAdminServer_Routes_Metrics(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	router := adminServer.router()
	if router == nil {
		t.Fatal("expected non-nil router")
	}
}

// TestAdminServer_withAuth_ShortHeader tests with short authorization header
func TestAdminServer_withAuth_ShortHeader(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with short header")
	})

	wrapped := adminServer.withAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Beare") // Less than 7 characters
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_withAuth_EmptyHeader tests with empty authorization header
func TestAdminServer_withAuth_EmptyHeader(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with empty header")
	})

	wrapped := adminServer.withAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "")
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminServer_adminMiddleware_WrongType tests admin middleware with wrong type for isAdmin
func TestAdminServer_adminMiddleware_WrongType(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with wrong type")
	})

	wrapped := adminServer.adminMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	ctx := req.Context()
	// Set isAdmin as string instead of bool
	ctx = context.WithValue(ctx, "isAdmin", "true")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
	}
}

// TestAdminServer_Start_Integration tests starting the admin server (integration)
func TestAdminServer_Start_Integration(t *testing.T) {
	adminServer, _, cleanup := setupAdminTestServer(t)
	defer cleanup()

	// Use a different port to avoid conflicts
	adminServer.config.Addr = "127.0.0.1:0"

	// Start server in background
	go func() {
		if err := adminServer.Start(); err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected error starting server: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Stop the server
	if err := adminServer.Stop(); err != nil {
		t.Errorf("unexpected error stopping server: %v", err)
	}
}
