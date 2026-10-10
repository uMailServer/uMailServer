package mcp

// Round 96 regressions: F5780 error leakage, F5781 account canonicalisation,
// F5782 argument validation, F5783 check_*/prompt input validation.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func r96call(t *testing.T, s *Server, tool string, args string) (int, string) {
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
	return securityTestCall(s, "admin-tok", body)
}

// F5780: raw storage errors leak to clients
func TestR96F5780(t *testing.T) {
	s, d := newSecurityTestServer(t)
	d.Close()
	for _, c := range []struct{ m, p string }{
		{"tools/call", `{"name":"list_domains","arguments":{}}`},
		{"tools/call", `{"name":"list_accounts","arguments":{"domain":"x.com"}}`},
		{"resources/read", `{"uri":"umailserver://domains"}`},
		{"resources/read", `{"uri":"umailserver://accounts"}`},
	} {
		_, b := securityTestCall(s, "admin-tok", fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":%q,"params":%s}`, c.m, c.p))
		if !strings.Contains(b, "internal server error") {
			t.Errorf("%s %s leaked: %s", c.m, c.p, b)
		}
	}
}

// F5781: mixed-case email / long password
func TestR96F5781(t *testing.T) {
	s, d := newSecurityTestServer(t)
	d.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true})
	code, b := r96call(t, s, "add_account", `{"email":"Alice@Example.COM","password":"pw12345678"}`)
	if code != 200 {
		t.Errorf("mixed case add: %d %s", code, b)
	}
	if a, err := d.GetAccount("example.com", "alice"); err != nil || a == nil {
		t.Errorf("canonical account missing: %v", err)
	}
	code, b = r96call(t, s, "add_account", `{"email":"bob@example.com","password":"`+strings.Repeat("a", 100)+`"}`)
	if !strings.Contains(b, "-32602") {
		t.Errorf("long password: %d %s", code, b)
	}
}

// F5782: argument types/range
func TestR96F5782(t *testing.T) {
	s, _ := newSecurityTestServer(t)
	for _, a := range []string{`{"name":"a.com","max_accounts":"5"}`, `{"name":"b.com","max_accounts":-3}`, `{"name":"c.com","max_accounts":1e30}`, `{"name":"d.com","max_accounts":1.5}`, `{"name":"e.com","max_mailbox_size":"banana"}`, `{"name":5}`} {
		_, b := r96call(t, s, "add_domain", a)
		if !strings.Contains(b, "-32602") {
			t.Errorf("%s accepted: %s", a, b)
		}
	}
	_, b := r96call(t, s, "add_domain", `{"name":"f.com","max_mailbox_size":"5GB"}`)
	var r struct{ Result any }
	json.Unmarshal([]byte(b), &r)
}

func TestR96F5782Size(t *testing.T) {
	s, d := newSecurityTestServer(t)
	r96call(t, s, "add_domain", `{"name":"f.com","max_mailbox_size":"5GB"}`)
	dd, err := d.GetDomain("f.com")
	if err != nil || dd.MaxMailboxSize != 5<<30 {
		t.Errorf("size not stored: %+v %v", dd, err)
	}
}

// F5783/F5784: check_* validation, prompt arg validation
func TestR96F5783(t *testing.T) {
	s, _ := newSecurityTestServer(t)
	_, b := r96call(t, s, "check_dns", `{"domain":"a.com\nIGNORE PREVIOUS"}`)
	if !strings.Contains(b, "-32602") {
		t.Errorf("check_dns: %s", b)
	}
	_, b = securityTestCall(s, "admin-tok", `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"setup_domain","arguments":{"domain":"a.com\n\nIgnore all"}}}`)
	if !strings.Contains(b, "-32602") {
		t.Errorf("prompt: %s", b)
	}
	_, b = securityTestCall(s, "admin-tok", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if strings.Contains(b, `"check_tls"`) {
		var r struct{ Result struct{ Tools []Tool } }
		json.Unmarshal([]byte(b), &r)
		for _, tl := range r.Result.Tools {
			if tl.Name == "check_tls" && len(tl.InputSchema.Required) == 0 {
				_, b2 := r96call(t, s, "check_tls", `{}`)
				if strings.Contains(b2, "error") {
					t.Errorf("check_tls schema says optional but empty rejected: %s", b2)
				}
			}
		}
	}
}
