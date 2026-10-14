package mcp

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// F7450: delete_account / delete_domain must refuse to remove the last
// active administrator, as the REST API does (409).
func TestR271DeleteLastAdminRefused(t *testing.T) {
	call := func(s *Server, tool string, args map[string]interface{}) string {
		body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]interface{}{"name": tool, "arguments": args}})
		rr := httptest.NewRecorder()
		s.HandleHTTP(rr, adminRequest(httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))))
		return rr.Body.String()
	}
	setup := func(admins ...string) (*Server, *db.DB) {
		d, err := db.Open(filepath.Join(t.TempDir(), "r271.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		if err := d.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10, IsActive: true}); err != nil {
			t.Fatal(err)
		}
		for _, lp := range admins {
			if err := d.CreateAccount(&db.AccountData{Email: lp + "@example.com", LocalPart: lp, Domain: "example.com", PasswordHash: "x", IsAdmin: true, IsActive: true}); err != nil {
				t.Fatal(err)
			}
		}
		return NewServer(d), d
	}
	has := func(d *db.DB, lp string) bool { _, err := d.GetAccount("example.com", lp); return err == nil }

	s, d := setup("root")
	if out := call(s, "delete_account", map[string]interface{}{"email": "root@example.com"}); !strings.Contains(out, "last active administrator") || !has(d, "root") {
		t.Fatalf("last admin account deleted: %s", out)
	}
	if out := call(s, "delete_domain", map[string]interface{}{"name": "example.com"}); !strings.Contains(out, "last active administrator") || !has(d, "root") {
		t.Fatalf("domain holding last admin deleted: %s", out)
	}

	s, d = setup("a1", "a2")
	if out := call(s, "delete_account", map[string]interface{}{"email": "a1@example.com"}); !strings.Contains(out, "deleted successfully") || has(d, "a1") {
		t.Fatalf("one of two admins should be deletable: %s", out)
	}
	if out := call(s, "delete_account", map[string]interface{}{"email": "a2@example.com"}); !strings.Contains(out, "last active administrator") || !has(d, "a2") {
		t.Fatalf("remaining admin deleted: %s", out)
	}
}
