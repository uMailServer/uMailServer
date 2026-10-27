package server

// F7786: the admin rate-limit endpoints were never given the server's rate
// limiter and always answered 503.

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdminRateLimitsAreWired(t *testing.T) {
	srv, pw := newJWTRotationTestServer(t, "", false, false)
	if err := srv.startAPI(); err != nil {
		t.Fatal(err)
	}
	tok := jwtRotLogin(t, srv, pw)
	get := func(path string) *json.RawMessage {
		t.Helper()
		rec := jwtRotDo(srv.apiServer, http.MethodGet, path, tok, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
		}
		var raw json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &raw)
		return &raw
	}

	var cfg struct {
		IPPerMinute int `json:"ip_per_minute"`
	}
	_ = json.Unmarshal(*get("/api/v1/admin/ratelimits/config"), &cfg)
	want := srv.rateLimiter.GetConfig().IPPerMinute
	if cfg.IPPerMinute != want || want == 0 {
		t.Fatalf("config ip_per_minute = %d, limiter has %d", cfg.IPPerMinute, want)
	}

	// A change made through the API reaches the limiter the SMTP stage uses.
	var full map[string]interface{}
	_ = json.Unmarshal(*get("/api/v1/admin/ratelimits/config"), &full)
	full["ip_per_minute"] = want + 7
	body, _ := json.Marshal(full)
	if rec := jwtRotDo(srv.apiServer, http.MethodPut, "/api/v1/admin/ratelimits/config", tok, body); rec.Code != http.StatusOK {
		t.Fatalf("PUT config: %d %s", rec.Code, rec.Body)
	}
	if got := srv.rateLimiter.GetConfig().IPPerMinute; got != want+7 {
		t.Fatalf("limiter ip_per_minute = %d after PUT, want %d", got, want+7)
	}

	get("/api/v1/admin/ratelimits/ip/192.0.2.1")
	get("/api/v1/admin/ratelimits/user/alice@test.com")
}
