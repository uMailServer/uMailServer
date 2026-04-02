package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/umailserver/umailserver/internal/db"
)

// Server implements MCP (Model Context Protocol)
type Server struct {
	db         *db.DB
	version    string
	corsOrigin string
	authToken  string
}

// NewServer creates MCP server
func NewServer(database *db.DB) *Server {
	return &Server{
		db:      database,
		version: "1.0.0",
	}
}

// SetAuthToken sets the authentication token for the MCP server.
// If set, all requests must include this token in the Authorization header.
func (s *Server) SetAuthToken(token string) {
	s.authToken = token
}

// SetCorsOrigin sets the allowed CORS origin(s).
func (s *Server) SetCorsOrigin(origin string) {
	s.corsOrigin = origin
}

// HandleHTTP handles MCP requests
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	origin := s.corsOrigin
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check authentication if token is configured
	if s.authToken != "" {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != s.authToken {
			s.writeError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	var result interface{}
	var err error

	switch req.Method {
	case "initialize":
		result, err = s.handleInitialize()
	case "tools/list":
		result = s.handleToolsList()
	case "tools/call":
		result, err = s.handleToolCall(req.Params)
	default:
		s.writeError(w, http.StatusBadRequest, "Unknown method: "+req.Method)
		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
	json.NewEncoder(w).Encode(resp)
}

// Request/Response types
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool types
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

type ToolSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type SchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// Initialize response
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Server          ServerInfo   `json:"server"`
	Capabilities    Capabilities `json:"capabilities"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Tools struct{} `json:"tools"`
}

// Handle initialize
func (s *Server) handleInitialize() (*InitializeResult, error) {
	return &InitializeResult{
		ProtocolVersion: "2024-11-05",
		Server: ServerInfo{
			Name:    "uMailServer MCP",
			Version: s.version,
		},
		Capabilities: Capabilities{},
	}, nil
}

// Handle tools/list
func (s *Server) handleToolsList() map[string]interface{} {
	return map[string]interface{}{
		"tools": []Tool{
			{
				Name:        "get_server_stats",
				Description: "Get server statistics",
				InputSchema: ToolSchema{
					Type:       "object",
					Properties: map[string]SchemaProperty{},
					Required:   []string{},
				},
			},
			{
				Name:        "list_accounts",
				Description: "List all accounts",
				InputSchema: ToolSchema{
					Type: "object",
					Properties: map[string]SchemaProperty{
						"domain": {Type: "string", Description: "Filter by domain"},
					},
					Required: []string{},
				},
			},
			{
				Name:        "list_domains",
				Description: "List all domains",
				InputSchema: ToolSchema{
					Type:       "object",
					Properties: map[string]SchemaProperty{},
					Required:   []string{},
				},
			},
		},
	}
}

// Handle tool call
func (s *Server) handleToolCall(params json.RawMessage) (map[string]interface{}, error) {
	var req ToolCallRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	switch req.Name {
	case "get_server_stats":
		return s.toolGetStats()
	case "list_accounts":
		domain := ""
		if d, ok := req.Arguments["domain"]; ok {
			domain = d.(string)
		}
		return s.toolListAccounts(domain)
	case "list_domains":
		return s.toolListDomains()
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Name)
	}
}

// Tool implementations
func (s *Server) toolGetStats() (map[string]interface{}, error) {
	domains, _ := s.db.ListDomains()
	accounts := 0
	for _, d := range domains {
		accts, _ := s.db.ListAccountsByDomain(d.Name)
		accounts += len(accts)
	}

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": fmt.Sprintf("Server Statistics:\n- Accounts: %d\n- Domains: %d\n- Version: %s", accounts, len(domains), s.version)},
		},
	}, nil
}

func (s *Server) toolListAccounts(domain string) (map[string]interface{}, error) {
	var accounts []*db.AccountData
	var err error

	if domain != "" {
		accounts, err = s.db.ListAccountsByDomain(domain)
	} else {
		domains, _ := s.db.ListDomains()
		for _, d := range domains {
			domainAccounts, _ := s.db.ListAccountsByDomain(d.Name)
			accounts = append(accounts, domainAccounts...)
		}
	}
	if err != nil {
		return nil, err
	}

	var text string
	if len(accounts) == 0 {
		text = "No accounts found"
	} else {
		text = fmt.Sprintf("Found %d accounts:\n", len(accounts))
		for _, a := range accounts {
			text += fmt.Sprintf("- %s (admin: %t)\n", a.Email, a.IsAdmin)
		}
	}

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolListDomains() (map[string]interface{}, error) {
	domains, err := s.db.ListDomains()
	if err != nil {
		return nil, err
	}

	var text string
	if len(domains) == 0 {
		text = "No domains found"
	} else {
		text = fmt.Sprintf("Found %d domains:\n", len(domains))
		for _, d := range domains {
			text += fmt.Sprintf("- %s (max %d accounts)\n", d.Name, d.MaxAccounts)
		}
	}
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

// Write error response
func (s *Server) writeError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(MCPResponse{
		JSONRPC: "2.0",
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	})
}
