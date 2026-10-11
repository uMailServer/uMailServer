package api

import (
	"bytes"
	"encoding/json"
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

// F6420 (F6384): when ConsumeTOTPStep fails with a DB error the login used to
// continue and issue a session without the OTP being consumed (replayable).
// It must fail closed.
func TestLoginFailsClosedWhenTOTPConsumeErrors(t *testing.T) {
	email := "consume@proof.invalid"
	pw := strings.Repeat("proof", 4)
	masterKey := strings.Repeat("t", 32)
	database, err := db.Open(t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.CreateDomain(&db.DomainData{Name: "proof.invalid", MaxAccounts: 10, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	enc, err := auth.EncryptTOTPSecret(totpReplayKeyB32, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err := database.CreateAccount(&db.AccountData{Email: email, LocalPart: "consume", Domain: "proof.invalid",
		PasswordHash: string(hash), IsActive: true, TOTPEnabled: true, TOTPSecret: enc}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{TokenExpiry: time.Hour, TOTPKey: masterKey})

	beforeTOTPConsume = func() { _ = database.DeleteAccount("proof.invalid", "consume") }
	defer func() { beforeTOTPConsume = nil }()

	body, _ := json.Marshal(map[string]string{"email": email, "password": pw, "totp_code": totpReplayCode(time.Now())})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	if rr.Code < 400 {
		t.Fatalf("login succeeded despite consume error: %d %s", rr.Code, rr.Body.String())
	}
	if len(rr.Result().Cookies()) != 0 || strings.Contains(rr.Body.String(), "token") {
		t.Fatalf("session issued on consume error")
	}
}
