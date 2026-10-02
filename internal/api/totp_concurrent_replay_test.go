package api

// Regression test for the TOTP login-path concurrent replay TOCTOU:
// handleLogin performs the RFC 6238 §5.2 replay check against the account
// SNAPSHOT fetched at the start of the request, and the write of
// TOTPLastUsedStep happens only after the deliberately slow password hash.
// Two concurrent logins presenting the same valid code therefore both read
// the pre-state, both pass the `step <= TOTPLastUsedStep` check, and both
// are issued tokens — the OTP is accepted twice. The fix consumes the step
// atomically (compare-and-swap inside one bbolt transaction), so exactly
// one concurrent login may consume a given step; the loser is a replay and
// must be rejected with 401.
//
// Determinism: with the start barrier, both handlers fetch the account
// within microseconds of each other while the password hash (bcrypt,
// ~60ms) delays both writes — the fetch-before-write interleaving that
// makes both pass is forced by construction, not luck.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoginConcurrentReplayOnlyOneSucceeds(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)

	// Setup stores the (encrypted) secret; the otpauth URI carries it.
	req := httptest.NewRequest("POST", "/api/v1/accounts/user@test.com/totp/setup", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	server.handleTOTPSetup(rec, req, "user@test.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("FAIL: setup status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var setupResp struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &setupResp); err != nil {
		t.Fatalf("FAIL: parse setup response: %v", err)
	}
	parsed, err := url.Parse(setupResp.URI)
	if err != nil {
		t.Fatalf("FAIL: parse otpauth uri: %v", err)
	}
	secret := parsed.Query().Get("secret")
	if secret == "" {
		t.Fatalf("FAIL: otpauth uri has no secret: %s", setupResp.URI)
	}

	// Enable TOTP directly in storage with TOTPLastUsedStep at its zero
	// value so the CURRENT window's code is fresh for both racers.
	account, err := database.GetAccount("test.com", "user")
	if err != nil {
		t.Fatalf("FAIL: get account: %v", err)
	}
	account.TOTPEnabled = true
	if err := database.UpdateAccount(account); err != nil {
		t.Fatalf("FAIL: enable TOTP: %v", err)
	}

	code := totpCodeAt(t, secret, time.Now())
	start := make(chan struct{})
	type result struct {
		status int
	}
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start // both racers launch together
			login := struct {
				Email    string `json:"email"`
				Password string `json:"password"`
				TOTPCode string `json:"totp_code"`
			}{Email: "user@test.com", Password: "password123", TOTPCode: code}
			body, _ := json.Marshal(login)
			r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			server.handleLogin(w, r)
			results <- result{status: w.Code}
		}()
	}
	ready.Wait()
	close(start)

	succeeded := 0
	replayed := 0
	for i := 0; i < 2; i++ {
		r := <-results
		switch r.status {
		case http.StatusOK:
			succeeded++
		case http.StatusUnauthorized:
			replayed++
		default:
			t.Fatalf("FAIL: unexpected login status %d (want 200 or 401 only)", r.status)
		}
	}

	// Post-fix contract: exactly one concurrent login may consume the step.
	if succeeded != 1 {
		t.Fatalf("FAIL: %d of 2 concurrent logins with one code succeeded (%d replayed), want exactly 1 — the OTP was accepted %d times (RFC 6238 §5.2)", succeeded, replayed, succeeded)
	}
	if replayed != 1 {
		t.Fatalf("FAIL: %d logins rejected, want 1", replayed)
	}
}
