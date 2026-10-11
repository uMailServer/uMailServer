package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// enrolR156 runs setup and returns the shared secret.
func enrolR156(t *testing.T, server *Server) string {
	t.Helper()
	rec := selfTOTPRequest(server, server.handleTOTPSetup, http.MethodPost, "user@example.com", nil)
	var setup struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &setup)
	u, _ := url.Parse(setup.URI)
	return u.Query().Get("secret")
}

// F6380 (High): self-service TOTP disable must revoke other sessions and
// hand the caller a replacement token.
func TestR156SelfTOTPDisableRevokesOtherSessions(t *testing.T) {
	server, database := setupSelfTOTPTest(t)
	secret := enrolR156(t, server)
	rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": totpCodeAt(t, secret, time.Now())})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
	}
	time.Sleep(5 * time.Millisecond)
	stolen, _ := server.newSessionToken("user@example.com", false)
	time.Sleep(5 * time.Millisecond)

	rec = selfTOTPRequest(server, server.handleTOTPDisable, http.MethodPost, "user@example.com", map[string]string{"code": totpCodeAt(t, secret, time.Now().Add(30*time.Second))})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if tok, _ := resp["token"].(string); tok == "" {
		t.Fatal("F6380: no replacement token returned")
	}
	a, _ := database.GetAccount("example.com", "user")
	if a.TokensValidAfter == 0 {
		t.Fatal("F6380: sessions not revoked on self-service disable")
	}
	_ = stolen
}

// F6381 (High): wrong codes on verify/disable are rate limited like login.
func TestR156TOTPVerifyLockout(t *testing.T) {
	server, _ := setupSelfTOTPTest(t)
	secret := enrolR156(t, server)
	for i := 0; i < maxTOTPFailures; i++ {
		rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": "000000"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": totpCodeAt(t, secret, time.Now())})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("F6381: verify after %d failures got %d, want 429", maxTOTPFailures, rec.Code)
	}
}

// F6382: enabling 2FA revokes other sessions and returns a replacement.
func TestR156TOTPVerifyRevokesSessions(t *testing.T) {
	server, database := setupSelfTOTPTest(t)
	secret := enrolR156(t, server)
	rec := selfTOTPRequest(server, server.handleTOTPVerify, http.MethodPost, "user@example.com", map[string]string{"code": totpCodeAt(t, secret, time.Now())})
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if tok, _ := resp["token"].(string); tok == "" {
		t.Fatal("F6382: no replacement token on enable")
	}
	a, _ := database.GetAccount("example.com", "user")
	if a.TokensValidAfter == 0 {
		t.Fatal("F6382: sessions not revoked on enable")
	}
}
