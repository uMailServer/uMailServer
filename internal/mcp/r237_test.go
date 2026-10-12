package mcp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func r237Post(s *Server, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	rr := httptest.NewRecorder()
	s.HandleHTTP(rr, req)
	return rr
}

// F7140: MCP requires receivers to answer ping with an empty result.
func TestR237PingAnswered(t *testing.T) {
	s := NewServer(nil)
	rr := r237Post(s, `{"jsonrpc":"2.0","id":7,"method":"ping"}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"result":{}`) {
		t.Fatalf("ping: %d %s", rr.Code, rr.Body.String())
	}
}

// F7141: trailing data after the request value is a parse error, not
// silently ignored (a second concatenated request would be dropped).
func TestR237TrailingDataRejected(t *testing.T) {
	s := NewServer(nil)
	rr := r237Post(s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "-32700") {
		t.Fatalf("trailing data: %d %s", rr.Code, rr.Body.String())
	}
	if rr := r237Post(s, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n  \n"); rr.Code != http.StatusOK {
		t.Fatalf("trailing whitespace: %d", rr.Code)
	}
}

// F7142: accounts/domains are stored lower-case; delete/get lookups must
// canonicalise like add_* and the REST API.
func TestR237CaseInsensitiveLookups(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := NewServer(database)
	if _, err := s.toolAddDomain("Example.com", 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.toolAddAccount("Bob@Example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.toolGetAccountInfo("BOB@example.com"); err != nil {
		t.Fatalf("get_account_info case variant: %v", err)
	}
	if _, err := s.toolDeleteAccount("BOB@EXAMPLE.com"); err != nil {
		t.Fatalf("delete_account case variant: %v", err)
	}
	if _, err := s.toolDeleteDomain("EXAMPLE.com"); err != nil {
		t.Fatalf("delete_domain case variant: %v", err)
	}
}
