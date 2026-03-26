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
