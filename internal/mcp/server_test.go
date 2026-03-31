package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

func TestMCPServer(t *testing.T) {
	// Create a temporary database for testing
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	t.Run("Initialize", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
		}
		body, _ := json.Marshal(reqBody)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.HandleHTTP)
		handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &resp)

		if resp["result"] == nil {
			t.Error("Expected result, got nil")
		}
	})

	t.Run("ToolsList", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/list",
		}
		body, _ := json.Marshal(reqBody)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.HandleHTTP)
		handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &resp)

		result, ok := resp["result"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected result to be a map")
		}

		tools, ok := result["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Error("Expected tools array with items")
		}
	})

	t.Run("ToolsCall", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name":      "get_server_stats",
				"arguments": map[string]interface{}{},
			},
		}
		body, _ := json.Marshal(reqBody)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.HandleHTTP)
		handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
	})

	t.Run("UnknownMethod", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      4,
			"method":  "unknown",
		}
		body, _ := json.Marshal(reqBody)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.HandleHTTP)
		handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", rr.Code)
		}
	})

	t.Run("CORS", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.HandleHTTP)
		handler.ServeHTTP(rr, httptest.NewRequest("OPTIONS", "/mcp", nil))

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200 for OPTIONS, got %d", rr.Code)
		}

		if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("Expected CORS header")
		}
	})
}

func TestMCPServerInvalidMethod(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	// Test GET method (should fail)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rr.Code)
	}
}

func TestMCPServerInvalidJSON(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	// Test invalid JSON
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte("invalid json"))))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}

func TestMCPServerListAccountsWithDomain(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "list_accounts",
			"arguments": map[string]interface{}{
				"domain": "example.com",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestMCPServerListDomains(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "list_domains",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestMCPServerUnknownTool(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "unknown_tool",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

func TestMCPServerInvalidToolCallParams(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)

	// Test with invalid params (not valid JSON for ToolCallRequest)
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "tools/call",
		"params":  "invalid_params",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	// Should return error due to invalid params
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

func TestNewServer(t *testing.T) {
	tmpDB, err := os.CreateTemp("", "mcp-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, err := db.Open(tmpDB.Name())
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	server := NewServer(database)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", server.version)
	}
}

// --- Additional coverage tests ---

func TestToolGetStatsWithDomains(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create some domains and accounts
	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateDomain(&db.DomainData{Name: "test.org", MaxAccounts: 5})
	database.CreateAccount(&db.AccountData{Email: "user1@example.com", Domain: "example.com", PasswordHash: "hash"})
	database.CreateAccount(&db.AccountData{Email: "user2@test.org", Domain: "test.org", PasswordHash: "hash"})

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "get_server_stats",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestToolListAccountsAll(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateAccount(&db.AccountData{Email: "user1@example.com", Domain: "example.com", PasswordHash: "hash", IsAdmin: true})
	database.CreateAccount(&db.AccountData{Email: "user2@example.com", Domain: "example.com", PasswordHash: "hash", IsAdmin: false})

	server := NewServer(database)

	// List all accounts (no domain filter)
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "list_accounts",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestToolListAccountsNoAccounts(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// List accounts on empty database
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      12,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "list_accounts",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Should contain "No accounts found"
	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["result"] == nil {
		t.Error("Expected result")
	}
}

func TestToolListDomainsWithDomains(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateDomain(&db.DomainData{Name: "test.org", MaxAccounts: 5})

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      13,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "list_domains",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestToolListDomainsNoDomains(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      14,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "list_domains",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}
