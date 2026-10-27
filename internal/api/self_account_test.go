package api

// GET /api/v1/account: the account portal's Profile page had no way to learn
// who was signed in.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestSelfAccountGet(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	seedSessionAccount(t, database, "x@ex.com", true)
	seedSessionAccount(t, database, "y@ex.com", false)
	acct, _ := database.GetAccount("ex.com", "y")
	acct.QuotaLimit, acct.TOTPEnabled, acct.TOTPSecret = 5000, true, "TOTPSECRET"
	acct.PasswordHash = "$2a$10$secrethash"
	if err := database.UpdateAccount(acct); err != nil {
		t.Fatal(err)
	}
	if err := database.IncrementQuota("ex.com", "y", 1234); err != nil {
		t.Fatal(err)
	}

	do := func(method string, asX bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/account", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminMetricsToken(t, asX))
		req.RemoteAddr = "127.0.0.1:4000"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, "TOTPSECRET") || strings.Contains(body, "secrethash") || strings.Contains(body, "password") {
		t.Fatalf("credentials in the response: %s", body)
	}
	var got SelfAccount
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Email != "y@ex.com" || got.QuotaUsed != 1234 || got.QuotaLimit != 5000 || !got.TwoFactorEnabled || got.IsAdmin || got.CreatedAt.IsZero() {
		t.Fatalf("account %+v", got)
	}
	// Each user sees their own account.
	_ = json.Unmarshal(do(http.MethodGet, true).Body.Bytes(), &got)
	if got.Email != "x@ex.com" || !got.IsAdmin {
		t.Fatalf("admin's own account %+v", got)
	}
	if rec := do(http.MethodPost, false); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD, DELETE" {
		t.Fatalf("POST: %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
	// DELETE still reaches the self-delete handler (it wants the password).
	if rec := do(http.MethodDelete, false); rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE without password: %d", rec.Code)
	}
}
