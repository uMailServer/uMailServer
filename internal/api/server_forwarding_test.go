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

// setupForwardingTest builds a server with one plain account
// (user@example.com) and no forwarding configured.
func setupForwardingTest(t *testing.T) (*Server, *db.DB) {
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

func forwardingRequest(server *Server, method, user string, body interface{}) *httptest.ResponseRecorder {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "/api/v1/account/forwarding", bytes.NewReader(payload))
	if user != "" {
		ctx := context.WithValue(req.Context(), "user", user)
		ctx = context.WithValue(ctx, "isAdmin", false)
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	server.handleForwarding(rec, req)
	return rec
}

// The full lifecycle: off -> set address + keep_copy -> persisted -> clear
// disables and clears the stored address.
func TestForwardingLifecycle(t *testing.T) {
	server, database := setupForwardingTest(t)

	rec := forwardingRequest(server, http.MethodGet, "user@example.com", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial GET: expected %d, got %d", http.StatusOK, rec.Code)
	}
	var status struct {
		ForwardingOn bool   `json:"forwarding_on"`
		ForwardTo    string `json:"forward_to"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if status.ForwardingOn || status.ForwardTo != "" {
		t.Fatal("expected forwarding off initially")
	}

	target := "backup@example.com"
	rec = forwardingRequest(server, http.MethodPut, "user@example.com", map[string]interface{}{
		"forward_to": target,
		"keep_copy":  true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	account, err := database.GetAccount("example.com", "user")
	if err != nil || account == nil {
		t.Fatalf("reload after PUT: %v", err)
	}
	if account.ForwardTo != target {
		t.Errorf("ForwardTo = %q, want %q", account.ForwardTo, target)
	}
	if !account.ForwardKeepCopy {
		t.Error("ForwardKeepCopy was not persisted")
	}

	rec = forwardingRequest(server, http.MethodGet, "user@example.com", nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if !status.ForwardingOn || status.ForwardTo != target {
		t.Errorf("GET after PUT: forwarding_on=%v forward_to=%q", status.ForwardingOn, status.ForwardTo)
	}

	rec = forwardingRequest(server, http.MethodPut, "user@example.com", map[string]interface{}{
		"forward_to": "",
		"keep_copy":  true, // must be ignored when forwarding is off
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("clearing PUT: expected %d, got %d", http.StatusOK, rec.Code)
	}
	account, err = database.GetAccount("example.com", "user")
	if err != nil || account == nil {
		t.Fatalf("reload after clearing: %v", err)
	}
	if account.ForwardTo != "" || account.ForwardKeepCopy {
		t.Errorf("clearing did not reset state: forward_to=%q keep_copy=%v", account.ForwardTo, account.ForwardKeepCopy)
	}
}

// Self-service input must be a parseable address; the admin path skips this
// check, the self-service path must not accept garbage.
func TestForwardingRejectsInvalidAddress(t *testing.T) {
	server, _ := setupForwardingTest(t)

	rec := forwardingRequest(server, http.MethodPut, "user@example.com", map[string]interface{}{
		"forward_to": "not-an-email",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected %d for an invalid address, got %d", http.StatusBadRequest, rec.Code)
	}
}

// Without an auth context the handler must refuse before touching data.
func TestForwardingRequiresAuthentication(t *testing.T) {
	server, _ := setupForwardingTest(t)

	rec := forwardingRequest(server, http.MethodPut, "", map[string]interface{}{
		"forward_to": "backup@example.com",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d without an auth context, got %d", http.StatusUnauthorized, rec.Code)
	}
}

// Only GET and PUT are supported.
func TestForwardingMethodGuard(t *testing.T) {
	server, _ := setupForwardingTest(t)

	rec := forwardingRequest(server, http.MethodDelete, "user@example.com", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected %d for DELETE, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}
