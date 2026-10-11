package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

// F6800/F6801: autodiscover answers only hosted domains and reads the POST body.
func TestAutodiscoverHostedOnly(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.CreateDomain(&db.DomainData{Name: "ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(database, nil, Config{JWTSecret: "x", TokenExpiry: time.Hour})
	do := func(method, url, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleAutodiscover(rec, req)
		return rec
	}
	if c := do(http.MethodGet, "/autodiscover/autodiscover.xml?email=a@evil.test", "").Code; c != http.StatusNotFound {
		t.Fatalf("foreign domain: %d", c)
	}
	if c := do(http.MethodGet, "/autodiscover/autodiscover.xml?email=a@ex.com", "").Code; c != http.StatusOK {
		t.Fatalf("hosted domain: %d", c)
	}
	body := `<Autodiscover><Request><EMailAddress>A@ex.com</EMailAddress></Request></Autodiscover>`
	rec := do(http.MethodPost, "/autodiscover/autodiscover.xml", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mail.ex.com") {
		t.Fatalf("POST body: %d %s", rec.Code, rec.Body.String())
	}
}
