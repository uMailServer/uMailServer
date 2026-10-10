package api

// Regression tests promoted from the audit proofs in .temp_files (F4935-F4939).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/db"
)

const a4938Secret = "audit-4938-secret"

func a4938Setup(t *testing.T) (*Server, string) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: a4938Secret, TokenExpiry: time.Hour})
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", IsActive: true}); err != nil {
		t.Fatalf("INVALID domain: %v", err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "bob@ex.com", LocalPart: "bob", Domain: "ex.com", IsActive: true}); err != nil {
		t.Fatalf("INVALID account: %v", err)
	}
	seedSessionAccount(t, database, "root@ex.com", true)
	if err := database.CreateAlias(&db.AliasData{Alias: "sales", Domain: "ex.com", Target: "bob@ex.com", IsActive: true}); err != nil {
		t.Fatalf("INVALID alias: %v", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "root@ex.com", "admin": true, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	tok.Header["kid"] = "default"
	str, err := tok.SignedString([]byte(a4938Secret))
	if err != nil {
		t.Fatalf("INVALID sign: %v", err)
	}
	return s, str
}

func a4938Post(s *Server, tok, path, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// Control: creating a new alias succeeds with 201.
func TestCreateDuplicateConflict_Control(t *testing.T) {
	s, tok := a4938Setup(t)
	if c, b := a4938Post(s, tok, "/api/v1/aliases", `{"alias":"info@ex.com","target":"bob@ex.com"}`); c != http.StatusCreated {
		t.Fatalf("INVALID control: alias create status=%d body=%s", c, b)
	}
}

// Defect: creating an alias that already exists returns 500 instead of 409.
func TestCreateDuplicateConflict_DuplicateAlias(t *testing.T) {
	s, tok := a4938Setup(t)
	c, b := a4938Post(s, tok, "/api/v1/aliases", `{"alias":"sales@ex.com","target":"bob@ex.com"}`)
	t.Logf("EXPECTED: status=409 | ACTUAL: status=%d body=%s", c, b)
	if c != http.StatusConflict {
		t.Fatalf("DEFECT F4938: duplicate alias status=%d, want 409", c)
	}
}

// Edge: duplicate domain returns 409 and leaves the stored domain intact.
func TestCreateDuplicateConflict_DuplicateDomain(t *testing.T) {
	s, tok := a4938Setup(t)
	c, b := a4938Post(s, tok, "/api/v1/domains", `{"name":"ex.com"}`)
	if c != http.StatusConflict {
		t.Fatalf("DEFECT F4938: duplicate domain status=%d body=%s, want 409", c, b)
	}
	d, err := s.db.GetDomain("ex.com")
	if err != nil || d.DKIMSelector != "" {
		t.Fatalf("stored domain changed: %+v err=%v", d, err)
	}
}

// Edge: a real store failure on create is still a 500, not a 409.
func TestCreateDuplicateConflict_StoreFailureStill500(t *testing.T) {
	s, tok := a4938Setup(t)
	// The alias handler reads domain/target before CreateAlias, so a closed DB
	// fails earlier; exercise domain create, which only writes.
	s.db.Close()
	c, _ := a4938Post(s, tok, "/api/v1/domains", `{"name":"new.com"}`)
	if c == http.StatusConflict || c == http.StatusCreated {
		t.Fatalf("closed DB create domain status=%d, want an error status other than 409", c)
	}
}
