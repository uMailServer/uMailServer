package api

// Regression tests for the cluster-route authorization gap: the four
// /api/v1/cluster/* routes were registered without the adminMiddleware
// wrapper that every other administrative endpoint in the route table uses,
// so ANY authenticated user could reach handleClusterFailover (which releases
// HA leadership), handleClusterHeartbeat (records instance health) and
// handleClusterStatus (discloses cluster topology). Per the codebase's own
// convention, administrative endpoints return 403 {"error":"admin access
// required"} for non-admin tokens.
//
// Tokens are obtained through the production login path (handleLogin) so no
// signing secret appears in this file.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/umailserver/umailserver/internal/db"
)

func TestClusterRoutesRequireAdmin(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/cluster-authz.db")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	domain := &db.DomainData{
		Name:        "test.com",
		MaxAccounts: 10,
		IsActive:    true,
	}
	if err := database.CreateDomain(domain); err != nil {
		t.Fatalf("failed to create domain: %v", err)
	}

	// Regular (non-admin) account.
	userHash, err := bcrypt.GenerateFromPassword([]byte("cluster-authz-user-fixture"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email:        "user@test.com",
		LocalPart:    "user",
		Domain:       "test.com",
		PasswordHash: string(userHash),
		IsActive:     true,
	}); err != nil {
		t.Fatalf("failed to create user account: %v", err)
	}

	// Admin account.
	adminHash, err := bcrypt.GenerateFromPassword([]byte("cluster-authz-admin-fixture"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email:        "admin@test.com",
		LocalPart:    "admin",
		Domain:       "test.com",
		PasswordHash: string(adminHash),
		IsActive:     true,
		IsAdmin:      true,
	}); err != nil {
		t.Fatalf("failed to create admin account: %v", err)
	}

	server := NewServer(database, nil, Config{TokenExpiry: time.Hour})

	login := func(email, password string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.handleLogin(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login for %s = %d, want %d (body %q)", email, rec.Code, http.StatusOK, rec.Body.String())
		}
		var result map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
			t.Fatalf("decode login response: %v", err)
		}
		token, ok := result["token"].(string)
		if !ok || token == "" {
			t.Fatalf("login response has no token: %v", result)
		}
		return token
	}

	userToken := login("user@test.com", "cluster-authz-user-fixture")
	adminToken := login("admin@test.com", "cluster-authz-admin-fixture")

	do := func(method, path, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		// csrfMiddleware requires application/json on unsafe methods.
		if method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		return rr
	}

	// Auth gate: no token is rejected.
	if rr := do(http.MethodGet, "/api/v1/cluster/status", ""); rr.Code != http.StatusUnauthorized {
		t.Errorf("GET cluster/status without token = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	// Control: the admin gate works on an existing admin-gated endpoint.
	if rr := do(http.MethodGet, "/api/v1/queue", userToken); rr.Code != http.StatusForbidden {
		t.Errorf("GET /api/v1/queue as non-admin = %d, want %d", rr.Code, http.StatusForbidden)
	}

	// All four cluster routes must reject non-admin tokens.
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/cluster/status"},
		{http.MethodGet, "/api/v1/cluster/instances"},
		{http.MethodPost, "/api/v1/cluster/failover"},
		{http.MethodPost, "/api/v1/cluster/heartbeat"},
	} {
		if rr := do(tc.method, tc.path, userToken); rr.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin = %d, want %d (body %q)", tc.method, tc.path, rr.Code, http.StatusForbidden, rr.Body.String())
		}
	}

	// Admin tokens must still pass the gate (nil clusterMgr yields the
	// handlers' disabled-cluster responses, not 403).
	if rr := do(http.MethodGet, "/api/v1/cluster/status", adminToken); rr.Code == http.StatusForbidden {
		t.Errorf("GET cluster/status as admin = 403, want non-403 (admin should pass the gate)")
	}
	if rr := do(http.MethodPost, "/api/v1/cluster/failover", adminToken); rr.Code == http.StatusForbidden {
		t.Errorf("POST cluster/failover as admin = 403, want non-403 (admin should pass the gate)")
	}
}
