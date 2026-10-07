package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// setupSelfTOTPTest builds a server with one plain account
// (user@example.com) that has no TOTP configured yet.
func setupSelfTOTPTest(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	config := Config{TokenExpiry: time.Hour}
	server := NewServer(database, nil, config)
	if err := database.CreateAccount(&db.AccountData{
		Email:     "user@example.com",
		LocalPart: "user",
		Domain:    "example.com",
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}
	return server, database
}

// selfTOTPRequest drives the registered self-service chain — the context
// adapter plus the wrapped handler — exactly as the route wires them.
func selfTOTPRequest(server *Server, handler func(http.ResponseWriter, *http.Request, string), method, user string, body interface{}) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "/api/v1/account/totp", bytes.NewReader(payload))
	if user != "" {
		ctx := context.WithValue(req.Context(), "user", user)
		ctx = context.WithValue(ctx, "isAdmin", false)
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	server.handleSelfTOTP(handler).ServeHTTP(rec, req)
	return rec
}

// The full self-service lifecycle: off -> setup -> verify -> enabled ->
// disable -> off, with the stored secret cleared on disable.
func TestSelfTOTPFullLifecycle(t *testing.T) {
	server, database := setupSelfTOTPTest(t)

	rec := selfTOTPRequest(server, server.handleTOTPStatus, http.MethodGet, "user@example.com", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected %d, got %d", http.StatusOK, rec.Code)
	}
	var status struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if status.Enabled {
		t.Fatal("expected TOTP disabled initially")
	}

	rec = selfTOTPRequest(server, server.handleTOTPSetup, http.MethodPost, "user@example.com", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var setup struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setup); err != nil {
		t.Fatalf("setup body: %v", err)
	}
	u, err := url.Parse(setup.URI)
	if err != nil || !strings.HasPrefix(setup.URI, "otpauth://") {
		t.Fatalf("setup: expected an otpauth URI, got %q", setup.URI)
	}
	secret := u.Query().Get("secret")
	if secret == "" {
		t.Fatal("setup: otpauth URI carries no secret")
	}

	code := totpCodeAt(t, secret, time.Now())
	rec = selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": code})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	account, err := database.GetAccount("example.com", "user")
	if err != nil || account == nil {
		t.Fatalf("reload after verify: %v", err)
	}
	if !account.TOTPEnabled {
		t.Error("verify did not enable TOTP")
	}
	if account.TOTPLastUsedStep == 0 {
		t.Error("verify did not consume the code's time step")
	}

	rec = selfTOTPRequest(server, server.handleTOTPStatus, http.MethodGet, "user@example.com", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if !status.Enabled {
		t.Error("status reports disabled after a successful verify")
	}

	rec = selfTOTPRequest(server, server.handleTOTPDisable, http.MethodPost, "user@example.com", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: expected %d, got %d", http.StatusOK, rec.Code)
	}
	account, err = database.GetAccount("example.com", "user")
	if err != nil || account == nil {
		t.Fatalf("reload after disable: %v", err)
	}
	if account.TOTPEnabled || account.TOTPSecret != "" {
		t.Error("disable did not clear the TOTP state")
	}
}

// A code from outside the accepted window must be rejected.
func TestSelfTOTPVerifyRejectsWrongCode(t *testing.T) {
	server, _ := setupSelfTOTPTest(t)

	rec := selfTOTPRequest(server, server.handleTOTPSetup, http.MethodPost, "user@example.com", nil)
	var setup struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &setup)
	u, _ := url.Parse(setup.URI)
	secret := u.Query().Get("secret")

	// Ten minutes in the past: far outside the window the server accepts.
	stale := totpCodeAt(t, secret, time.Now().Add(-10*time.Minute))
	rec = selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": stale})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d for a stale code, got %d", http.StatusUnauthorized, rec.Code)
	}
}

// The verify path must keep consuming steps through the self-service route
// too (RFC 6238 s5.2, the round-45 replay fix): the same code must never be
// accepted twice.
func TestSelfTOTPVerifyRejectsReplayedCode(t *testing.T) {
	server, _ := setupSelfTOTPTest(t)

	rec := selfTOTPRequest(server, server.handleTOTPSetup, http.MethodPost, "user@example.com", nil)
	var setup struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &setup)
	u, _ := url.Parse(setup.URI)
	secret := u.Query().Get("secret")

	code := totpCodeAt(t, secret, time.Now())
	body := map[string]string{"code": code}
	if rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", body); rec.Code != http.StatusOK {
		t.Fatalf("first verify: expected %d, got %d", http.StatusOK, rec.Code)
	}
	if rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed verify: expected %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

// Without an auth context the adapter must refuse before any handler runs.
func TestSelfTOTPRequiresAuthentication(t *testing.T) {
	server, _ := setupSelfTOTPTest(t)

	rec := selfTOTPRequest(server, server.handleTOTPSetup, http.MethodPost, "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d without an auth context, got %d", http.StatusUnauthorized, rec.Code)
	}
}
