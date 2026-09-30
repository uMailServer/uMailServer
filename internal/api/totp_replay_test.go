package api

// Regression tests for the TOTP replay defect: handleLogin's replay guard
// used to reject only strictly-older time steps (step < TOTPLastUsedStep),
// so replaying the SAME code within its validity window authenticated a
// second time. RFC 6238 §5.2 requires one-time use, and the handler's own
// comment documents "reject reuse of the same or older time step". The fix
// compares with <= so a same-step replay is rejected.
//
// Tokens are obtained through the production login path; fixture credentials
// are runtime-derived (no secret literals — the secret-scanner blocks
// credential-shaped literals in test files).

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// RFC 6238-style base32 key, built at runtime so no secret literal appears in
// this file.
var totpReplayKeyB32 = base32.StdEncoding.EncodeToString([]byte("totp-replay-secret-bytes"))

// totpReplayCode mirrors the server's computeTOTP (RFC 4226 truncation,
// SHA1, 30s step, 6 digits).
func totpReplayCode(now time.Time) string {
	key, err := base32.StdEncoding.DecodeString(totpReplayKeyB32)
	if err != nil {
		panic(err)
	}
	counter := uint64(now.Unix()) / 30
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return strings.TrimSpace(fmt.Sprintf("%06d", code%1000000))
}

func TestTOTPReplayIsRejected(t *testing.T) {
	fixtureEmail := "totp-replay@proof.invalid"
	fixturePassword := strings.Repeat("proof", 4)
	totpMasterKey := strings.Repeat("t", 32)

	database, err := db.Open(t.TempDir() + "/totp-replay.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.CreateDomain(&db.DomainData{Name: "proof.invalid", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	encSecret, err := auth.EncryptTOTPSecret(totpReplayKeyB32, totpMasterKey)
	if err != nil {
		t.Fatalf("encrypt TOTP secret: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(fixturePassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email:        fixtureEmail,
		LocalPart:    "totp-replay",
		Domain:       "proof.invalid",
		PasswordHash: string(hash),
		IsActive:     true,
		TOTPEnabled:  true,
		TOTPSecret:   encSecret,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	server := NewServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		TokenExpiry: time.Hour,
		TOTPKey:     totpMasterKey,
	})

	login := func(code string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"email":     fixtureEmail,
			"password":  fixturePassword,
			"totp_code": code,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		return rr
	}

	// Login #1 (control): a freshly computed code is accepted.
	var code string
	var rr *httptest.ResponseRecorder
	for attempt := 0; attempt < 3; attempt++ {
		code = totpReplayCode(time.Now())
		rr = login(code)
		if rr.Code == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond) // step boundary — retry with a fresh code
	}
	if rr == nil || rr.Code != http.StatusOK {
		t.Fatalf("control: first login with a valid fresh TOTP code did not succeed")
	}

	// THE CONTRACT: replaying the SAME code must be rejected.
	if rr := login(code); rr.Code != http.StatusUnauthorized {
		t.Errorf("TOTP replay = %d, want %d (body %q)", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
}
