package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/db"
)

func FuzzMCPRequest(f *testing.F) {
	dir, err := os.MkdirTemp("", "mcpfuzz")
	if err != nil {
		f.Fatal(err)
	}
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { database.Close(); os.RemoveAll(dir) })
	srv := NewServer(database)
	srv.SetAuthToken("user-token")
	srv.SetAdminAuthToken("admin-token")

	for _, s := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":"x","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_domain","arguments":{"name":"a.test","max_accounts":5,"max_mailbox_size":"1GB"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_account","arguments":{"email":"u@a.test","password":"pw"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_account_info","arguments":{"email":"u@a.test"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_account","arguments":{"email":"u@@a.test"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_queue_status","arguments":{"limit":1e308}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"retry_queue_item","arguments":{"id":"../x"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_accounts","arguments":{"domain":"a.test"}}}`,
		`{"jsonrpc":"2.0","id":[1],"method":"tools/call"}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"umailserver://accounts"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"setup_domain","arguments":{"domain":"a.test"}}}`,
		`{"jsonrpc":"2.0","id":null,"method":"prompts/list","params":null}`,
		`[{"jsonrpc":"2.0","id":1,"method":"x"}]`,
	} {
		f.Add([]byte(s), true)
	}
	f.Fuzz(func(t *testing.T, body []byte, admin bool) {
		if len(body) > 1<<15 {
			return
		}
		// check_dns/check_tls perform real network lookups; flush/reload touch
		// external services. Skip them so the fuzzer stays hermetic.
		for _, bad := range []string{"check_", "flush_queue", "reload_config"} {
			if strings.Contains(string(body), bad) {
				return
			}
		}
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		tok := "user-token"
		if admin {
			tok = "admin-token"
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		start := time.Now()
		srv.HandleHTTP(rec, req)
		// add_account runs bcrypt, which is slow under -race.
		if d := time.Since(start); d > 15*time.Second {
			t.Fatalf("request took %v", d)
		}
		switch rec.Code {
		case http.StatusAccepted:
			if rec.Body.Len() != 0 {
				t.Fatalf("202 with body")
			}
			return
		case http.StatusTooManyRequests:
			return
		}
		var resp MCPResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response not JSON (status %d): %v: %q", rec.Code, err, rec.Body.String())
		}
		if resp.JSONRPC != "2.0" {
			t.Fatalf("bad jsonrpc %q", resp.JSONRPC)
		}
		// Business-rule failures (duplicate/missing domain, ...) use -32603 by
		// design; anything that leaks storage/runtime internals is a defect.
		if resp.Error != nil {
			m := strings.ToLower(resp.Error.Message)
			for _, leak := range []string{"bolt", "bucket", "key too", "runtime error", "panic", "goroutine", "/home/", "/tmp/"} {
				if strings.Contains(m, leak) {
					t.Fatalf("internal detail leaked (%d): %s", rec.Code, resp.Error.Message)
				}
			}
		}
		if !admin && resp.Error == nil && strings.Contains(string(body), "umailserver://accounts") {
			// non-admin must not read admin resources
			if _, ok := adminResources["umailserver://accounts"]; ok {
				t.Fatalf("non-admin read admin resource")
			}
		}
	})
}

func FuzzMCPArgs(f *testing.F) {
	f.Add(`{"a":1,"b":"x","c":1.5,"d":null,"e":1e400}`, "a")
	f.Add(`{"a":9223372036854775807}`, "a")
	f.Add(`{"a":-1e19}`, "a")
	f.Fuzz(func(t *testing.T, doc, key string) {
		var m map[string]interface{}
		if json.Unmarshal([]byte(doc), &m) != nil {
			return
		}
		if n, err := argInt(m, key, 0, 1000); err == nil && (n < 0 || n > 1000) {
			t.Fatalf("argInt out of range: %d", n)
		}
		argString(m, key)
		validateDomainName(key)
		validateEmailAddress(key, key)
	})
}
