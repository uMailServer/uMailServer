package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// setupAccountPasswordTest builds a server with one seeded account
// (user@example.com) whose password is the value returned in currentPassword.
// JWTSecret is intentionally left empty: NewServer generates a random one,
// and this spec drives the handler directly with an injected auth context,
// so no token is ever minted or verified.
func setupAccountPasswordTest(t *testing.T) (*Server, string, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	config := Config{TokenExpiry: time.Hour}
	server := NewServer(database, nil, config)

	current := "seed-current-password"
	hashed, err := server.hashPassword(current)
	if err != nil {
		t.Fatalf("failed to hash seed password: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email:        "user@example.com",
		LocalPart:    "user",
		Domain:       "example.com",
		PasswordHash: hashed,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}
	return server, current, database
}

func postAccountPassword(server *Server, user string, current, next string) *httptest.ResponseRecorder {
	body := map[string]string{"current_password": current, "new_password": next}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/password", bytes.NewReader(payload))
	if user != "" {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}
	rec := httptest.NewRecorder()
	server.handleAccountPassword(rec, req)
	return rec
}

// The self-service change must persist the new password hash and drop the
// old one.
func TestAccountPasswordChangesPassword(t *testing.T) {
	server, current, database := setupAccountPasswordTest(t)
	next := "brand-new-password"

	rec := postAccountPassword(server, "user@example.com", current, next)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	account, err := database.GetAccount("example.com", "user")
	if err != nil || account == nil {
		t.Fatalf("failed to reload account: %v", err)
	}
	if matches, _ := server.verifyPassword(next, account.PasswordHash); !matches {
		t.Error("new password does not verify against the stored hash")
	}
	if matches, _ := server.verifyPassword(current, account.PasswordHash); matches {
		t.Error("old password still verifies against the stored hash")
	}
}

// Re-authentication: the current password must match.
func TestAccountPasswordRejectsWrongCurrentPassword(t *testing.T) {
	server, current, _ := setupAccountPasswordTest(t)

	rec := postAccountPassword(server, "user@example.com", current, "brand-new-password")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

// Client-side length validation must be mirrored server-side.
func TestAccountPasswordRejectsShortNewPassword(t *testing.T) {
	server, current, _ := setupAccountPasswordTest(t)

	rec := postAccountPassword(server, "user@example.com", current, "short")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// Both fields are required.
func TestAccountPasswordRejectsMissingFields(t *testing.T) {
	server, current, _ := setupAccountPasswordTest(t)

	rec := postAccountPassword(server, "user@example.com", current, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

// The handler reads the account from the auth context — no context, no
// service.
func TestAccountPasswordRequiresAuthentication(t *testing.T) {
	server, current, _ := setupAccountPasswordTest(t)

	rec := postAccountPassword(server, "", current, "brand-new-password")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}