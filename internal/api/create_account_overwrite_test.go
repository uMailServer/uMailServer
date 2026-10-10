package api

// Regression test for the duplicate-account creation defect: the accounts
// storage CreateAccount was a bare Put (no existence check) and the admin
// API create handler had no existence pre-check, so a duplicate
// POST /api/v1/accounts silently REPLACED the existing account — new
// password hash, new APOP hash, IsAdmin/IsActive from the request — and
// answered success. Creating an address that already exists must be
// rejected with 409 Conflict and leave the stored account untouched.
//
// Hermetic: real temp accounts database; the handler is driven directly
// with the authMiddleware-injected context (production sets "user" and
// "isAdmin" from the JWT claims).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func startAccountCreateServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	accountsDB, err := db.Open(t.TempDir() + "/accounts.db")
	if err != nil {
		t.Fatalf("open accounts db: %v", err)
	}
	t.Cleanup(func() { _ = accountsDB.Close() })
	server := NewServer(accountsDB, nil, Config{})
	// F5372: createAccount requires a hosted domain.
	if err := accountsDB.CreateDomain(&db.DomainData{Name: "test.com", IsActive: true}); err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	const (
		email       = "alice@test.com"
		originalPwd = "Original-Pass-1!"
	)
	hash, err := server.hashPassword(originalPwd)
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	if err := accountsDB.CreateAccount(&db.AccountData{
		Email:        email,
		LocalPart:    "alice",
		Domain:       "test.com",
		PasswordHash: hash,
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	return server, accountsDB
}

func postCreateAccount(t *testing.T, server *Server, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"email":    email,
		"password": password,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	r := httptest.NewRequest("POST", "/api/v1/accounts", bytes.NewReader(body))
	ctx := context.WithValue(context.Background(), "user", "admin@test.com")
	ctx = context.WithValue(ctx, "isAdmin", true)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	server.createAccount(w, r)
	return w
}

func TestDuplicateAccountCreationIsRejected(t *testing.T) {
	server, accountsDB := startAccountCreateServer(t)

	seeded, err := accountsDB.GetAccount("test.com", "alice")
	if err != nil {
		t.Fatalf("FAIL: seed account missing: %v", err)
	}
	originalHash := seeded.PasswordHash

	// DEFECT: creating the same address again must conflict, not silently
	// replace the stored account.
	w := postCreateAccount(t, server, "alice@test.com", "Replacement-Pass-9!")
	if w.Code != http.StatusConflict {
		t.Fatalf("FAIL: duplicate account creation returned %d, want 409 Conflict (body: %s) — the existing account was silently replaced", w.Code, w.Body.String())
	}

	stored, err := accountsDB.GetAccount("test.com", "alice")
	if err != nil {
		t.Fatalf("FAIL: account missing after conflict: %v", err)
	}
	if stored.PasswordHash != originalHash {
		t.Fatalf("FAIL: phantom replace — the request was rejected but the stored PasswordHash changed from %q to %q", originalHash, stored.PasswordHash)
	}
}

func TestFreshAccountCreationStillSucceeds(t *testing.T) {
	server, accountsDB := startAccountCreateServer(t)

	w := postCreateAccount(t, server, "bob@test.com", "Bob-Pass-1234!")
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("FAIL: fresh create returned %d, want 200/201 (body: %s)", w.Code, w.Body.String())
	}
	if _, err := accountsDB.GetAccount("test.com", "bob"); err != nil {
		t.Fatalf("FAIL: fresh account missing after create: %v", err)
	}
}
