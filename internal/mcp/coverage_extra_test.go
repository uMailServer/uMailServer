package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
)

// Test toolAddDomain
func TestToolAddDomain(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Test adding domain
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "add_domain",
			"arguments": map[string]interface{}{
				"name":             "test.com",
				"max_accounts":     float64(50),
				"max_mailbox_size": "1GB",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify response contains success message
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] != nil {
		t.Errorf("Unexpected error: %v", resp["error"])
	}
}

// Test toolAddDomain missing name
func TestToolAddDomain_MissingName(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Test adding domain without name
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "add_domain",
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

// Test toolDeleteDomain
func TestToolDeleteDomain(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create a domain first
	database.CreateDomain(&db.DomainData{Name: "delete-me.com", MaxAccounts: 10})

	server := NewServer(database)

	// Test deleting domain
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "delete_domain",
			"arguments": map[string]interface{}{
				"name": "delete-me.com",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// Test toolAddAccount
func TestToolAddAccount(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create a domain first
	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})

	server := NewServer(database)

	// Test adding account
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "add_account",
			"arguments": map[string]interface{}{
				"email":    "test@example.com",
				"password": "securepassword123",
				"is_admin": true,
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify response
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] != nil {
		t.Errorf("Unexpected error: %v", resp["error"])
	}
}

// Test toolAddAccount missing fields
func TestToolAddAccount_MissingFields(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Test adding account without email
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "add_account",
			"arguments": map[string]interface{}{
				"password": "securepassword123",
			},
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

// Test toolAddAccount invalid email
func TestToolAddAccount_InvalidEmail(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Test adding account with invalid email
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "add_account",
			"arguments": map[string]interface{}{
				"email":    "invalid-email",
				"password": "securepassword123",
			},
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

// Test toolAddAccount nonexistent domain
func TestToolAddAccount_NonexistentDomain(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Test adding account with non-existent domain
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "add_account",
			"arguments": map[string]interface{}{
				"email":    "test@nonexistent.com",
				"password": "securepassword123",
			},
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

// Test toolDeleteAccount
func TestToolDeleteAccount(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create domain and account
	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateAccount(&db.AccountData{Email: "delete@example.com", Domain: "example.com", LocalPart: "delete", PasswordHash: "hash"})

	server := NewServer(database)

	// Test deleting account
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "delete_account",
			"arguments": map[string]interface{}{
				"email": "delete@example.com",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// Test toolGetAccountInfo
func TestToolGetAccountInfo(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create domain and account
	database.CreateDomain(&db.DomainData{Name: "example.com", MaxAccounts: 10})
	database.CreateAccount(&db.AccountData{
		Email:        "info@example.com",
		Domain:       "example.com",
		LocalPart:    "info",
		PasswordHash: "hash",
		IsAdmin:      true,
		QuotaUsed:    1024,
		QuotaLimit:   1000000,
	})

	server := NewServer(database)

	// Test getting account info
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "get_account_info",
			"arguments": map[string]interface{}{
				"email": "info@example.com",
			},
		},
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify response contains account info
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array with items")
	}
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "info@example.com") {
		t.Errorf("Expected response to contain email, got: %s", text)
	}
}

// Test toolGetQueueStatus
func TestToolGetQueueStatus(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "get_queue_status",
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

// Test toolRetryQueueItem
func TestToolRetryQueueItem(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "retry_queue_item",
			"arguments": map[string]interface{}{
				"id": "queue-item-123",
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

// Test toolFlushQueue
func TestToolFlushQueue(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "flush_queue",
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

// Test toolCheckDNS
func TestToolCheckDNS(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "check_dns",
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

// Test toolCheckDNS missing domain
func TestToolCheckDNS_MissingDomain(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "check_dns",
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

// Test toolCheckTLS
func TestToolCheckTLS(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "check_tls",
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

// Test toolGetSystemStatus
func TestToolGetSystemStatus(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "get_system_status",
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

// Test toolReloadConfig
func TestToolReloadConfig(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "reload_config",
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

// Test handleResourcesList
func TestHandleResourcesList(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/list",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}
	resources, ok := result["resources"].([]interface{})
	if !ok || len(resources) == 0 {
		t.Error("Expected resources array with items")
	}
}

// Test handleResourceRead for domains
func TestHandleResourceRead_Domains(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create a domain
	database.CreateDomain(&db.DomainData{Name: "res-test.com", MaxAccounts: 10})

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": "umailserver://domains",
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

// Test handleResourceRead for accounts
func TestHandleResourceRead_Accounts(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	// Create domain and account
	database.CreateDomain(&db.DomainData{Name: "res-test.com", MaxAccounts: 10})
	database.CreateAccount(&db.AccountData{Email: "user@res-test.com", Domain: "res-test.com", LocalPart: "user", PasswordHash: "hash"})

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": "umailserver://accounts",
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

// Test handleResourceRead for config
func TestHandleResourceRead_Config(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": "umailserver://config",
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

// Test handleResourceRead for status
func TestHandleResourceRead_Status(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": "umailserver://status",
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

// Test handleResourceRead unknown resource
func TestHandleResourceRead_Unknown(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri": "umailserver://unknown",
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

// Test handlePromptsList
func TestHandlePromptsList(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/list",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}
	prompts, ok := result["prompts"].([]interface{})
	if !ok || len(prompts) == 0 {
		t.Error("Expected prompts array with items")
	}
}

// Test handlePromptGet setup_domain
func TestHandlePromptGet_SetupDomain(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name": "setup_domain",
			"arguments": map[string]string{
				"domain": "testdomain.com",
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

	// Verify response contains setup guide
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be a map")
	}
	if !strings.Contains(result["description"].(string), "testdomain.com") {
		t.Error("Expected description to contain domain name")
	}
}

// Test handlePromptGet troubleshoot_delivery
func TestHandlePromptGet_TroubleshootDelivery(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name": "troubleshoot_delivery",
			"arguments": map[string]string{
				"email": "user@example.com",
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

// Test handlePromptGet security_audit
func TestHandlePromptGet_SecurityAudit(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name":      "security_audit",
			"arguments": map[string]string{},
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

// Test handlePromptGet unknown prompt
func TestHandlePromptGet_Unknown(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name":      "unknown_prompt",
			"arguments": map[string]string{},
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

// Test handlePromptGet with invalid params
func TestHandlePromptGet_InvalidParams(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "prompts/get",
		"params":  "invalid",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

// Test handleResourceRead with invalid params
func TestHandleResourceRead_InvalidParams(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "resources/read",
		"params":  "invalid",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

// Test writeError
func TestWriteError(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	// Trigger an error by sending invalid method
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
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

	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["error"] == nil {
		t.Error("Expected error in response")
	}
}

// Test handleToolCall with invalid params
func TestHandleToolCall_InvalidParams(t *testing.T) {
	tmpDB, _ := os.CreateTemp("", "mcp-test-*.db")
	defer os.Remove(tmpDB.Name())
	tmpDB.Close()

	database, _ := db.Open(tmpDB.Name())
	defer database.Close()

	server := NewServer(database)

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  "not_valid_json_for_tool_call",
	}
	body, _ := json.Marshal(reqBody)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.HandleHTTP)
	handler.ServeHTTP(rr, httptest.NewRequest("POST", "/mcp", bytes.NewReader(body)))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}
