package mcp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// F7222: business failures are isError results (HTTP 200), not -32603/500.
func TestR245ToolBusinessErrorIsResult(t *testing.T) {
	s, database := newSecurityTestServer(t)
	if err := database.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	code, body := securityTestCall(s, "admin-tok", `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"add_domain","arguments":{"name":"example.com"}}}`)
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	var r struct {
		Error  *MCPError `json:"error"`
		Result struct {
			IsError bool                `json:"isError"`
			Content []map[string]string `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	if r.Error != nil || !r.Result.IsError || len(r.Result.Content) != 1 || r.Result.Content[0]["text"] == "" {
		t.Fatalf("unexpected: %s", body)
	}
}
