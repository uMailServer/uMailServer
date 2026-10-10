package api

// Regression tests promoted from the audit proofs in .temp_files (F4935-F4939).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a4939Secret = "audit-4939-secret"

func a4939Setup(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	seedSessionAccount(t, database, "bob@ex.com", false)
	return NewServer(database, nil, Config{JWTSecret: a4939Secret, TokenExpiry: time.Hour})
}

// a4939Token signs a token exactly like handleLogin does, for second sec.
func a4939Token(t *testing.T, sub string, sec int64) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "admin": false, "exp": sec + int64(time.Hour/time.Second), "iat": sec,
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a4939Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return str
}

func a4939Req(s *Server, method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// a4939RefreshSameSecond refreshes a token issued in the current second and
// returns (old, new). The pair is retried (bounded, no sleeps) until old and
// the refresh land in the same wall-clock second, which is the precondition.
func a4939RefreshSameSecond(t *testing.T, s *Server) (string, string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		before := time.Now().Unix()
		old := a4939Token(t, "bob@ex.com", before)
		rec := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", old)
		after := time.Now().Unix()
		if rec.Code != http.StatusOK {
			t.Fatalf("INVALID refresh status=%d body=%s", rec.Code, rec.Body.String())
		}
		if before != after {
			continue // crossed a second boundary; precondition not met
		}
		var out struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
			t.Fatalf("INVALID refresh body: %v", err)
		}
		return old, out.Token
	}
	t.Fatalf("INVALID: could not land old token and refresh in one second")
	return "", ""
}

// Control: a token issued in an earlier second refreshes into a usable token.
func TestRefreshSameSecond_Control(t *testing.T) {
	s := a4939Setup(t)
	old := a4939Token(t, "bob@ex.com", time.Now().Unix()-10)
	rec := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", old)
	var out struct {
		Token string `json:"token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("INVALID control refresh status=%d", rec.Code)
	}
	if c := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", out.Token).Code; c != http.StatusOK {
		t.Fatalf("INVALID control: refreshed token rejected (%d)", c)
	}
}

// Defect: refreshing a token issued in the same second returns a token
// byte-identical to the one just revoked, so the client is logged out.
func TestRefreshSameSecond_SameSecondRefreshUsable(t *testing.T) {
	s := a4939Setup(t)
	old, fresh := a4939RefreshSameSecond(t, s)
	c := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", fresh).Code
	t.Logf("EXPECTED: new token != old and usable (200) | ACTUAL: identical=%v status=%d", old == fresh, c)
	if c != http.StatusOK {
		t.Fatalf("DEFECT F4939: token returned by refresh is already revoked (status=%d)", c)
	}
}

// Edge: two refreshes chained in the same second both yield distinct usable
// tokens and the old token stays revoked.
func TestRefreshSameSecond_ChainedRefresh(t *testing.T) {
	s := a4939Setup(t)
	old, fresh := a4939RefreshSameSecond(t, s)
	if old == fresh {
		t.Fatalf("refresh returned the revoked token")
	}
	if c := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", old).Code; c != http.StatusUnauthorized {
		t.Fatalf("old token reuse status=%d, want 401", c)
	}
	rec := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", fresh)
	var out struct {
		Token string `json:"token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Token == fresh {
		t.Fatalf("second refresh status=%d distinct=%v", rec.Code, out.Token != fresh)
	}
	if c := a4939Req(s, http.MethodPost, "/api/v1/auth/refresh", out.Token).Code; c != http.StatusOK {
		t.Fatalf("third-generation token rejected (%d)", c)
	}
}
