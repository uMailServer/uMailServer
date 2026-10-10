package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func newSecurityTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/mcp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database)
	s.SetAuthToken("user-tok")
	s.SetAdminAuthToken("admin-tok")
	return s, database
}

func securityTestCall(s *Server, token, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	s.HandleHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

// F5015: resources/read must not disclose password hashes, TOTP/APOP
// secrets or DKIM private keys.
func TestResourceReadRedactsSecrets(t *testing.T) {
	s, database := newSecurityTestServer(t)
	if err := database.CreateDomain(&db.DomainData{Name: "example.com", DKIMPrivateKey: "DKIM-PRIVATE-SECRET"}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "u@example.com", LocalPart: "u", Domain: "example.com",
		PasswordHash: "HASHSECRET", APOPHash: "APOPSECRET", TOTPSecret: "TOTPSECRET"}); err != nil {
		t.Fatal(err)
	}
	// F5045: these resources are admin-only, so only the admin token is
	// exercised here; the user token is covered by TestDirectoryReadsRequireAdmin.
	for _, tok := range []string{"admin-tok"} {
		for uri, want := range map[string]string{"umailserver://accounts": "u@example.com", "umailserver://domains": "example.com"} {
			code, body := securityTestCall(s, tok, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
			if code != http.StatusOK || !strings.Contains(body, want) {
				t.Fatalf("%s %s: code=%d body=%s", tok, uri, code, body)
			}
			for _, secret := range []string{"DKIM-PRIVATE-SECRET", "HASHSECRET", "APOPSECRET", "TOTPSECRET"} {
				if strings.Contains(body, secret) {
					t.Errorf("%s %s leaked %s", tok, uri, secret)
				}
			}
		}
	}
	if a, err := database.GetAccount("example.com", "u"); err != nil || a.PasswordHash != "HASHSECRET" {
		t.Fatalf("stored account mutated: %v %v", a, err)
	}
}

// F5017: add_domain/add_account reject path-like names at the boundary.
func TestAddDomainAndAccountRejectPathLikeNames(t *testing.T) {
	s, database := newSecurityTestServer(t)
	tool := func(name, args string) int {
		code, _ := securityTestCall(s, "admin-tok", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args))
		return code
	}
	if code := tool("add_domain", `{"name":"example.com"}`); code != http.StatusOK {
		t.Fatalf("valid domain rejected: %d", code)
	}
	for _, d := range []string{"../../etc", `evil\dir`, "a/b.com", strings.Repeat("a", 254)} {
		if code := tool("add_domain", fmt.Sprintf(`{"name":%q}`, d)); code == http.StatusOK {
			t.Errorf("domain %q accepted", d)
		}
		if _, err := database.GetDomain(d); err == nil {
			t.Errorf("domain %q stored", d)
		}
	}
	for _, e := range []string{"../../x@example.com", "a/b@example.com", "x\r\ny@example.com"} {
		if code := tool("add_account", fmt.Sprintf(`{"email":%q,"password":"Str0ng!Passw0rd"}`, e)); code == http.StatusOK {
			t.Errorf("email %q accepted", e)
		}
	}
	if code := tool("add_account", `{"email":"user@example.com","password":"Str0ng!Passw0rd"}`); code != http.StatusOK {
		t.Fatalf("valid account rejected: %d", code)
	}
}

// adminRequest marks req as authenticated with the admin token, as
// HandleHTTP does for the admin bearer token.
func adminRequest(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), adminCtxKeyVal, true))
}

// F5045: list_domains is admin-only, so every other read of the domain /
// account directory must be admin-only too.
func TestDirectoryReadsRequireAdmin(t *testing.T) {
	s, database := newSecurityTestServer(t)
	if err := database.CreateDomain(&db.DomainData{Name: "secret-corp.test", MaxAccounts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "ceo@secret-corp.test", LocalPart: "ceo", Domain: "secret-corp.test"}); err != nil {
		t.Fatal(err)
	}
	bodies := []string{
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"umailserver://domains"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"umailserver://accounts"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_accounts","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_account_info","arguments":{"email":"ceo@secret-corp.test"}}}`,
	}
	for _, b := range bodies {
		code, body := securityTestCall(s, "user-tok", b)
		if code != http.StatusForbidden || strings.Contains(body, "secret-corp.test") {
			t.Errorf("user token: code=%d body=%s for %s", code, body, b)
		}
		code, body = securityTestCall(s, "admin-tok", b)
		if code != http.StatusOK || !strings.Contains(body, "secret-corp.test") {
			t.Errorf("admin token: code=%d body=%s for %s", code, body, b)
		}
	}
	if code, _ := securityTestCall(s, "user-tok", `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"umailserver://status"}}`); code != http.StatusOK {
		t.Errorf("status resource with user token: %d", code)
	}
}

// F5046 / F5047: JSON-RPC 2.0 error objects echo the request id (null when
// unreadable) with reserved codes, and string ids round-trip.
func TestJSONRPCErrorIDsAndCodes(t *testing.T) {
	s, _ := newSecurityTestServer(t)
	cases := []struct {
		body, wantID string
		wantCode     int
	}{
		{`{"jsonrpc":"2.0","id":7,"method":"no/such"}`, "7", -32601},
		{`{"jsonrpc":"2.0","id":"a-1","method":"no/such"}`, `"a-1"`, -32601},
		{`{"jsonrpc":"2.0","id":7,`, "null", -32700},
		{`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":"oops"}`, "9", -32602},
		{`{"jsonrpc":"2.0","id":3,"method":5}`, "null", -32600},
		{`{"jsonrpc":"2.0","id":{"x":1},"method":"initialize"}`, "null", -32600},
		{`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"umailserver://nope"}}`, "5", -32002},
	}
	for _, c := range cases {
		_, body := securityTestCall(s, "user-tok", c.body)
		var resp struct {
			ID    json.RawMessage `json:"id"`
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		if string(resp.ID) != c.wantID || resp.Error.Code != c.wantCode {
			t.Errorf("%s: id=%s code=%d, want id=%s code=%d", c.body, resp.ID, resp.Error.Code, c.wantID, c.wantCode)
		}
	}
	code, body := securityTestCall(s, "user-tok", `{"jsonrpc":"2.0","id":"req-abc","method":"initialize"}`)
	if code != http.StatusOK || !strings.Contains(body, `"id":"req-abc"`) || !strings.Contains(body, "protocolVersion") {
		t.Errorf("string id request: code=%d body=%s", code, body)
	}
}

// F5250: add_domain / add_account must store active records; inactive
// accounts cannot authenticate and inactive domains are not delivered locally.
func TestAddDomainAndAccountCreateActiveRecords(t *testing.T) {
	s, database := newSecurityTestServer(t)
	tool := func(name, args string) int {
		code, _ := securityTestCall(s, "admin-tok", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args))
		return code
	}
	if code := tool("add_domain", `{"name":"mcp.test"}`); code != http.StatusOK {
		t.Fatalf("add_domain: %d", code)
	}
	if code := tool("add_account", `{"email":"bob@mcp.test","password":"Str0ng!Passw0rd"}`); code != http.StatusOK {
		t.Fatalf("add_account: %d", code)
	}
	if d, err := database.GetDomain("mcp.test"); err != nil || !d.IsActive {
		t.Errorf("domain not active: %+v %v", d, err)
	}
	if a, err := database.GetAccount("mcp.test", "bob"); err != nil || !a.IsActive || a.IsAdmin {
		t.Errorf("account not active (or admin): %+v %v", a, err)
	}
}

// F5251: deleting a missing account/domain (including a case variant of a
// stored address) must not report success.
func TestDeleteMissingAccountOrDomainFails(t *testing.T) {
	s, database := newSecurityTestServer(t)
	if err := database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(&db.AccountData{Email: "victim@example.com", LocalPart: "victim", Domain: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_account","arguments":{"email":"ghost@example.com"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_account","arguments":{"email":"Victim@Example.com"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_domain","arguments":{"name":"nosuch.test"}}}`,
	} {
		if code, body := securityTestCall(s, "admin-tok", b); code == http.StatusOK || strings.Contains(body, "deleted successfully") || !strings.Contains(body, "not found") {
			t.Errorf("code=%d body=%s for %s", code, body, b)
		}
	}
	if _, err := database.GetAccount("example.com", "victim"); err != nil {
		t.Fatalf("victim account removed: %v", err)
	}
	code, _ := securityTestCall(s, "admin-tok", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_account","arguments":{"email":"victim@example.com"}}}`)
	if _, err := database.GetAccount("example.com", "victim"); code != http.StatusOK || err == nil {
		t.Fatalf("existing delete: code=%d err=%v", code, err)
	}
}
