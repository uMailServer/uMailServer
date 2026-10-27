package api

// The admin Settings page had no endpoint and showed hard-coded
// placeholders. GET /api/v1/admin/settings now serves the effective,
// secret-free configuration to admins.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// adminSettingsRouters returns the main API and the admin-port routers.
func adminSettingsRouters(t *testing.T, f func() ServerSettings) map[string]http.Handler {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	if f != nil {
		s.SetSettingsFunc(f)
	}
	seedSessionAccount(t, database, "x@ex.com", true)
	seedSessionAccount(t, database, "y@ex.com", false)
	return map[string]http.Handler{
		"api":   s,
		"admin": NewAdminServer(s, AdminConfig{Addr: "127.0.0.1:0", JWTSecret: adminMetricsSecret}).router(),
	}
}

func TestAdminSettingsEndpoint(t *testing.T) {
	for name, h := range adminSettingsRouters(t, func() ServerSettings {
		return ServerSettings{Hostname: "mx.ex.com", MaxRecipients: 42}
	}) {
		t.Run(name, func(t *testing.T) { checkAdminSettings(t, h) })
	}
}

func checkAdminSettings(t *testing.T, h http.Handler) {
	const path = "/api/v1/admin/settings"

	rec := adminMetricsGet(h, path, adminMetricsToken(t, true))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET status=%d body=%s", rec.Code, rec.Body)
	}
	var got ServerSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not settings JSON: %v: %s", err, rec.Body)
	}
	if got.Hostname != "mx.ex.com" || got.MaxRecipients != 42 {
		t.Fatalf("settings %+v", got)
	}

	if rec := adminMetricsGet(h, path, adminMetricsToken(t, false)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d, want 403", rec.Code)
	}
	if rec := adminMetricsGet(h, path, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminMetricsToken(t, true))
	req.RemoteAddr = "127.0.0.1:4000"
	put := httptest.NewRecorder()
	h.ServeHTTP(put, req)
	if put.Code != http.StatusMethodNotAllowed || put.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("PUT status=%d Allow=%q, want 405 with Allow: GET, HEAD", put.Code, put.Header().Get("Allow"))
	}
}

func TestAdminSettingsUnavailable(t *testing.T) {
	for name, h := range adminSettingsRouters(t, nil) {
		if rec := adminMetricsGet(h, "/api/v1/admin/settings", adminMetricsToken(t, true)); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status=%d, want 503 without a settings source", name, rec.Code)
		}
	}
}
