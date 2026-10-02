package api

// Regression test for the TOTP verify-path replay residue:
// handleTOTPVerify validated the code with auth.ValidateTOTP but never
// consumed its time step, while the login path (handleLogin) enforces
// RFC 6238 §5.2 replay protection via account.TOTPLastUsedStep. A code
// successfully accepted by /totp/verify therefore remained valid for
// /auth/login within the same window — the OTP was accepted twice, which
// RFC 6238 §5.2 forbids ("The verifier MUST NOT accept the second attempt
// of the OTP after the successful validation has been issued for the first
// OTP"). The fix consumes the matched step in the verify handler, persisting
// it in the same UpdateAccount that enables TOTP.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// totpCodeAt computes the 6-digit TOTP code for a base32 secret at now,
// RFC 4226 dynamic truncation over HMAC-SHA1 (the server's default algo).
func totpCodeAt(t *testing.T, secretB32 string, now time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimRight(secretB32, "=")))
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	counter := uint64(now.Unix()) / 30
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0F
	code := (uint32(sum[offset])&0x7F)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}

func TestTOTPVerifyConsumesCodeAgainstLoginReplay(t *testing.T) {
	server, database := helperSetupTOTPAccount(t)
	defer database.Close()

	ctx := context.WithValue(context.Background(), "user", "user@test.com")
	ctx = context.WithValue(ctx, "isAdmin", false)

	// 1. Setup TOTP — the otpauth URI carries the secret.
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

	now := time.Now()
	code := totpCodeAt(t, secret, now)

	// 2. Verify with the code — accepted and TOTP enabled (control).
	verifyBody := strings.NewReader(`{"code": "` + code + `"}`)
	req = httptest.NewRequest("POST", "/api/v1/accounts/user@test.com/totp/verify", verifyBody).WithContext(ctx)
	rec = httptest.NewRecorder()
	server.handleTOTPVerify(rec, req, "user@test.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("FAIL: verify status %d, want 200 (control; body %s)", rec.Code, rec.Body.String())
	}

	// 3. DEFECT: the login path must not accept the same OTP a second time
	// (RFC 6238 §5.2). Pre-fix it authenticated the session.
	login := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}{Email: "user@test.com", Password: "password123", TOTPCode: code}
	loginJSON, err := json.Marshal(login)
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}
	req = httptest.NewRequest("POST", "/auth/login", strings.NewReader(string(loginJSON)))
	rec = httptest.NewRecorder()
	server.handleLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("FAIL: login with the OTP already accepted by /totp/verify returned %d, want 401 — the code was accepted twice (RFC 6238 §5.2)", rec.Code)
	}
}
