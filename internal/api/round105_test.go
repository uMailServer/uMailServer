package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func r105Call(method, path, body, user string, admin bool, h func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = context.WithValue(ctx, "isAdmin", admin)
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	return rec
}

// F5870 (High): a non-admin must not raise their own quota.
func TestR105NonAdminCannotChangeOwnQuota(t *testing.T) {
	s, d := apiUpdateFixtureServer(t)
	rec := r105Call("PUT", "/api/v1/accounts/user@example.test", `{"quota_limit":999999}`, "user@example.test", false, s.handleAccountDetail)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d want 403", rec.Code)
	}
	a, _ := d.GetAccount("example.test", "user")
	if a.QuotaLimit != 0 {
		t.Fatalf("quota changed to %d", a.QuotaLimit)
	}
	rec = r105Call("PUT", "/api/v1/accounts/user@example.test", `{"quota_limit":5}`, "root@example.test", true, s.handleAccountDetail)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status %d", rec.Code)
	}
}

// F5871: a non-admin probing another account must not learn whether it exists.
func TestR105UpdateAccountNoExistenceOracle(t *testing.T) {
	s, _ := apiUpdateFixtureServer(t)
	a := r105Call("PUT", "/api/v1/accounts/ghost@example.test", `{}`, "user@example.test", false, s.handleAccountDetail)
	b := r105Call("PUT", "/api/v1/accounts/user@example.test", `{}`, "other@example.test", false, s.handleAccountDetail)
	if a.Code != http.StatusForbidden || b.Code != http.StatusForbidden {
		t.Fatalf("got %d and %d, want both 403", a.Code, b.Code)
	}
}

// F5872: dynamic CORS origin responses must carry Vary: Origin.
func TestR105CORSVaryOrigin(t *testing.T) {
	s, _ := apiUpdateFixtureServer(t)
	s.config.CorsOrigins = []string{"https://a.example"}
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, origin := range []string{"https://a.example", "https://evil.example", ""} {
		req := httptest.NewRequest("GET", "/x", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Errorf("origin %q: missing Vary: Origin", origin)
		}
	}
}

// F5873: push subscribe/unsubscribe client errors map to 4xx, not 500.
func TestR105PushErrorMapping(t *testing.T) {
	s, _ := apiUpdateFixtureServer(t)
	body := `{"endpoint":"https://push.example/x","p256dh":"k","auth":"a"}`
	cases := []struct {
		err  string
		want int
	}{
		{"push endpoint address 10.0.0.1 is not allowed", 400},
		{"push endpoint host \"localhost\" is not allowed", 400},
		{"invalid subscription ID", 400},
		{"subscription ID already in use", 409},
		{"failed to save subscription: disk", 500},
	}
	for _, c := range cases {
		s.SetPushService(&MockPushService{SubscribeError: errors.New(c.err)})
		rec := r105Call("POST", "/api/v1/push/subscribe", body, "user@example.test", false, s.handlePushSubscribe)
		if rec.Code != c.want {
			t.Errorf("%q: got %d want %d", c.err, rec.Code, c.want)
		}
	}
	s.SetPushService(&MockPushService{UnsubscribeError: errors.New("subscription not found")})
	rec := r105Call("DELETE", "/api/v1/push/unsubscribe?id=x", "", "user@example.test", false, s.handlePushUnsubscribe)
	if rec.Code != 404 {
		t.Errorf("unsubscribe not found: got %d want 404", rec.Code)
	}
}

// F5874: alias creation honours an explicit is_active=false.
func TestR105CreateAliasInactive(t *testing.T) {
	s, d := apiUpdateFixtureServer(t)
	rec := r105Call("POST", "/api/v1/aliases", `{"alias":"info@example.test","target":"user@example.test","is_active":false}`, "root@example.test", true, s.handleAliases)
	if rec.Code != 201 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	a, err := d.GetAlias("example.test", "info")
	if err != nil || a.IsActive {
		t.Fatalf("alias active=%v err=%v", a != nil && a.IsActive, err)
	}
}
