package api

// The account portal (web/account) was embedded but served by no route.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func TestAccountPortalServed(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	s.accountFS = NewEmbedFSAdapter(fstest.MapFS{
		"index.html":         {Data: []byte("<title>Account Settings</title>")},
		"assets/index-1.js":  {Data: []byte("console.log(1)")},
		"assets/index-1.css": {Data: []byte("body{}")},
	})
	s.webmailFS = NewEmbedFSAdapter(fstest.MapFS{"index.html": {Data: []byte("<title>Webmail</title>")}})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	for _, c := range []struct {
		path   string
		status int
		ctype  string
		body   string
	}{
		{"/account/", 200, "text/html", "Account Settings"},
		{"/account/password", 200, "text/html", "Account Settings"}, // SPA route
		{"/account/assets/index-1.js", 200, "application/javascript", "console.log"},
		{"/account/assets/index-1.css", 200, "text/css", "body{}"},
		{"/account/assets/missing.js", 404, "", ""},
		{"/", 200, "text/html", "Webmail"},
	} {
		rec := get(c.path)
		if rec.Code != c.status {
			t.Errorf("GET %s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if c.ctype != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), c.ctype) {
			t.Errorf("GET %s: Content-Type %q", c.path, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("GET %s: body %q", c.path, rec.Body.String())
		}
	}
	if rec := get("/account"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/account/" {
		t.Errorf("GET /account: %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
}
