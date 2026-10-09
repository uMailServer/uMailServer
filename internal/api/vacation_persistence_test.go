package api

// Regression tests for the vacation persistence defect: the production API
// server (api.NewServer, the constructor internal/server/server_api.go calls)
// never wired a VacationManager, so setVacationConfig's placeholder returned
// nil and PUT /api/v1/vacation answered 200 {"status":"success"} while
// throwing the configuration away. A following GET returned the hardcoded
// defaults (enabled=false, subject "Out of Office") and
// /api/v1/admin/vacations was always empty. docs/API_SPECIFICATION.md
// documents the endpoint as "Set vacation settings" / "Get vacation
// settings", so a successful PUT must persist the configuration for the
// authenticated user and a subsequent GET (including after a server restart,
// since the manager stores on disk) must return it.

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const vacationFixtureEmail = "owner@vactest.invalid"

// Fixture credentials are derived at runtime (project convention keeps
// credential-shaped literals out of the source).
func vacationFixturePassword() string {
	return strings.Repeat("fixture", 3)
}

// vacationFixture builds the exact production constructor with a real
// (temporary) DataDir, plus a real account to log in with.
func vacationFixture(t *testing.T) (*Server, *db.DB, Config) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.CreateDomain(&db.DomainData{Name: "vactest.invalid", IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(vacationFixturePassword()), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{
		Email: vacationFixtureEmail, LocalPart: "owner", Domain: "vactest.invalid",
		PasswordHash: string(hash), IsActive: true,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Production constructor: internal/server/server_api.go startAPI uses
	// exactly this (NewServer + Config incl. DataDir).
	prodCfg := Config{
		JWTSecret:   strings.Repeat("vacationpersist", 2),
		TokenExpiry: time.Hour,
		DataDir:     dir,
	}
	return NewServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), prodCfg), database, prodCfg
}

func vacationLogin(t *testing.T, srv *Server) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": vacationFixtureEmail, "password": vacationFixturePassword(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED (harness): login = %d (body %q)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("CONTROL FAILED (harness): no token: %v", err)
	}
	return resp.Token
}

func vacationDo(t *testing.T, srv *Server, method, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/vacation", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

func decodeVacation(t *testing.T, rr *httptest.ResponseRecorder) VacationConfig {
	t.Helper()
	var cfg VacationConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode vacation response %q: %v", rr.Body.String(), err)
	}
	return cfg
}

func TestVacationConfigPersistsAcrossGet(t *testing.T) {
	srv, _, _ := vacationFixture(t)
	token := vacationLogin(t, srv)

	// Control (must hold before and after the fix): a user with no stored
	// config gets the documented defaults, disabled.
	if rr := vacationDo(t, srv, http.MethodGet, token, ""); rr.Code != http.StatusOK {
		t.Fatalf("CONTROL FAILED (harness): initial GET = %d (body %q)", rr.Code, rr.Body.String())
	} else if cfg := decodeVacation(t, rr); cfg.Enabled {
		t.Fatalf("CONTROL FAILED (harness): fresh user reports enabled vacation")
	}

	// Control (validation contract, unchanged by the fix): enabling without a
	// subject must still be rejected.
	if rr := vacationDo(t, srv, http.MethodPut, token,
		`{"enabled":true,"message":"no subject","send_interval":24}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("CONTROL FAILED (harness): PUT without subject = %d, want 400", rr.Code)
	}

	// The defect: a successful PUT must persist the configuration.
	putBody := `{"enabled":true,"subject":"OOO","message":"Away until Monday","send_interval":24}`
	if rr := vacationDo(t, srv, http.MethodPut, token, putBody); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d (body %q), want 200 success", rr.Code, rr.Body.String())
	}

	rr := vacationDo(t, srv, http.MethodGet, token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("FAIL: GET after successful PUT = %d (body %q)", rr.Code, rr.Body.String())
	}
	cfg := decodeVacation(t, rr)
	if !cfg.Enabled {
		t.Fatalf("FAIL: PUT reported success but GET returns enabled=false — config was discarded")
	}
	if cfg.Subject != "OOO" {
		t.Fatalf("FAIL: GET subject = %q, want persisted %q", cfg.Subject, "OOO")
	}
	if cfg.Message != "Away until Monday" {
		t.Fatalf("FAIL: GET message = %q, want persisted message", cfg.Message)
	}
	if cfg.SendInterval != 24 {
		t.Fatalf("FAIL: GET send_interval = %d, want persisted 24", cfg.SendInterval)
	}

	// Secondary branch: DELETE must remove the stored config again.
	if rr := vacationDo(t, srv, http.MethodDelete, token, ""); rr.Code != http.StatusOK {
		t.Fatalf("FAIL: DELETE = %d (body %q)", rr.Code, rr.Body.String())
	}
	rr = vacationDo(t, srv, http.MethodGet, token, "")
	if cfg := decodeVacation(t, rr); cfg.Enabled {
		t.Fatalf("FAIL: GET after DELETE still reports enabled vacation")
	}
}

func TestVacationConfigSurvivesServerRestart(t *testing.T) {
	srv, database, prodCfg := vacationFixture(t)
	token := vacationLogin(t, srv)

	putBody := `{"enabled":true,"subject":"OOO","message":"Away until Monday","send_interval":24}`
	if rr := vacationDo(t, srv, http.MethodPut, token, putBody); rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d (body %q), want 200 success", rr.Code, rr.Body.String())
	}

	// A restarted server re-runs the production constructor on the same
	// DataDir; the vacation store is disk-backed, so the config must survive.
	restarted := NewServer(database, slog.New(slog.NewTextHandler(io.Discard, nil)), prodCfg)
	rr := vacationDo(t, restarted, http.MethodGet, token, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("FAIL: GET after restart = %d (body %q)", rr.Code, rr.Body.String())
	}
	got := decodeVacation(t, rr)
	if !got.Enabled || got.Subject != "OOO" {
		t.Fatalf("FAIL: config lost after restart: enabled=%v subject=%q", got.Enabled, got.Subject)
	}
}
