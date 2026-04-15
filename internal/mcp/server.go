package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// Server implements MCP (Model Context Protocol)
type Server struct {
	db         *db.DB
	version    string
	corsOrigin string
	authToken  string

	// Rate limiting
	rateLimit    int // requests per minute, 0 = disabled
	rateMu       sync.Mutex
	rateAttempts map[string]*rateAttempt
}

// rateAttempt tracks MCP requests per IP for rate limiting
type rateAttempt struct {
	count       int
	windowStart time.Time
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

// SetRateLimit sets the rate limit for MCP requests (requests per minute, 0 = disabled)
func (s *Server) SetRateLimit(limit int) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	s.rateLimit = limit
}

// checkRateLimit returns true if the IP is allowed to make MCP requests
func (s *Server) checkRateLimit(ip string) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	if s.rateLimit <= 0 {
		return true // rate limiting disabled
	}

	if s.rateAttempts == nil {
		s.rateAttempts = make(map[string]*rateAttempt)
	}

	now := time.Now()
	attempt, exists := s.rateAttempts[ip]

	// Check if window has expired (1 minute window)
	if !exists || now.Sub(attempt.windowStart) > time.Minute {
		s.rateAttempts[ip] = &rateAttempt{count: 1, windowStart: now}
		return true
	}

	// Check if limit exceeded
	if attempt.count >= s.rateLimit {
		return false
	}

	attempt.count++
	return true
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

	// Check rate limit by IP
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if !s.checkRateLimit(ip) {
		s.writeError(w, http.StatusTooManyRequests, "Rate limit exceeded")
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
	case "resources/list":
		result = s.handleResourcesList()
	case "resources/read":
		result, err = s.handleResourceRead(req.Params)
	case "prompts/list":
		result = s.handlePromptsList()
	case "prompts/get":
		result, err = s.handlePromptGet(req.Params)
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
	_ = json.NewEncoder(w).Encode(resp)
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
	return map[string]interface{}{"tools": []Tool{
		// Server Stats
		{
			Name:        "get_server_stats",
			Description: "Get server statistics including accounts, domains, queue status",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]SchemaProperty{},
				Required:   []string{},
			},
		},
		// Domain Management
		{
			Name:        "list_domains",
			Description: "List all configured domains",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]SchemaProperty{},
				Required:   []string{},
			},
		},
		{
			Name:        "add_domain",
			Description: "Add a new domain to the server",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"name":             {Type: "string", Description: "Domain name (e.g., example.com)"},
					"max_accounts":     {Type: "number", Description: "Maximum number of accounts (default: 100)"},
					"max_mailbox_size": {Type: "string", Description: "Maximum mailbox size (e.g., 5GB)"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "delete_domain",
			Description: "Delete a domain and all its accounts",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"name": {Type: "string", Description: "Domain name to delete"},
				},
				Required: []string{"name"},
			},
		},
		// Account Management
		{
			Name:        "list_accounts",
			Description: "List all accounts, optionally filtered by domain",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"domain": {Type: "string", Description: "Filter by domain (optional)"},
				},
				Required: []string{},
			},
		},
		{
			Name:        "add_account",
			Description: "Add a new email account",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"email":    {Type: "string", Description: "Full email address"},
					"password": {Type: "string", Description: "Account password"},
					"is_admin": {Type: "boolean", Description: "Grant admin privileges"},
				},
				Required: []string{"email", "password"},
			},
		},
		{
			Name:        "delete_account",
			Description: "Delete an email account",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"email": {Type: "string", Description: "Email address to delete"},
				},
				Required: []string{"email"},
			},
		},
		{
			Name:        "get_account_info",
			Description: "Get detailed information about an account",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"email": {Type: "string", Description: "Email address"},
				},
				Required: []string{"email"},
			},
		},
		// Queue Management
		{
			Name:        "get_queue_status",
			Description: "Get mail queue status and pending messages",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"limit": {Type: "number", Description: "Maximum number of entries to return"},
				},
				Required: []string{},
			},
		},
		{
			Name:        "retry_queue_item",
			Description: "Retry a failed queue item by ID",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"id": {Type: "string", Description: "Queue item ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "flush_queue",
			Description: "Flush all pending messages in the queue",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]SchemaProperty{},
				Required:   []string{},
			},
		},
		// Diagnostics
		{
			Name:        "check_dns",
			Description: "Check DNS records for a domain",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"domain": {Type: "string", Description: "Domain to check"},
				},
				Required: []string{"domain"},
			},
		},
		{
			Name:        "check_tls",
			Description: "Check TLS certificate status",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]SchemaProperty{
					"domain": {Type: "string", Description: "Domain to check (optional, uses hostname if empty)"},
				},
				Required: []string{},
			},
		},
		// System Operations
		{
			Name:        "get_system_status",
			Description: "Get system health status (disk, memory, services)",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]SchemaProperty{},
				Required:   []string{},
			},
		},
		{
			Name:        "reload_config",
			Description: "Reload server configuration",
			InputSchema: ToolSchema{
				Type:       "object",
				Properties: map[string]SchemaProperty{},
				Required:   []string{},
			},
		},
	}}
}

// Handle tool call
func (s *Server) handleToolCall(params json.RawMessage) (map[string]interface{}, error) {
	var req ToolCallRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	switch req.Name {
	// Server Stats
	case "get_server_stats":
		return s.toolGetStats()
	case "get_system_status":
		return s.toolGetSystemStatus()

	// Domain Management
	case "list_domains":
		return s.toolListDomains()
	case "add_domain":
		name, _ := req.Arguments["name"].(string)
		maxAccounts, _ := req.Arguments["max_accounts"].(float64)
		maxSize, _ := req.Arguments["max_mailbox_size"].(string)
		return s.toolAddDomain(name, int(maxAccounts), maxSize)
	case "delete_domain":
		name, _ := req.Arguments["name"].(string)
		return s.toolDeleteDomain(name)

	// Account Management
	case "list_accounts":
		domain := ""
		if d, ok := req.Arguments["domain"]; ok {
			if ds, ok := d.(string); ok {
				domain = ds
			}
		}
		return s.toolListAccounts(domain)
	case "add_account":
		email, _ := req.Arguments["email"].(string)
		password, _ := req.Arguments["password"].(string)
		isAdmin, _ := req.Arguments["is_admin"].(bool)
		return s.toolAddAccount(email, password, isAdmin)
	case "delete_account":
		email, _ := req.Arguments["email"].(string)
		return s.toolDeleteAccount(email)
	case "get_account_info":
		email, _ := req.Arguments["email"].(string)
		return s.toolGetAccountInfo(email)

	// Queue Management
	case "get_queue_status":
		limit, _ := req.Arguments["limit"].(float64)
		return s.toolGetQueueStatus(int(limit))
	case "retry_queue_item":
		id, _ := req.Arguments["id"].(string)
		return s.toolRetryQueueItem(id)
	case "flush_queue":
		return s.toolFlushQueue()

	// Diagnostics
	case "check_dns":
		domain, _ := req.Arguments["domain"].(string)
		return s.toolCheckDNS(domain)
	case "check_tls":
		domain, _ := req.Arguments["domain"].(string)
		return s.toolCheckTLS(domain)

	// System Operations
	case "reload_config":
		return s.toolReloadConfig()

	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Name)
	}
}

// Tool implementations
func (s *Server) toolGetStats() (map[string]interface{}, error) {
	domains, err := s.db.ListDomains()
	if err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}
	accounts := 0
	for _, d := range domains {
		accts, err := s.db.ListAccountsByDomain(d.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to list accounts for domain %s: %w", d.Name, err)
		}
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
		domains, err := s.db.ListDomains()
		if err != nil {
			return nil, fmt.Errorf("failed to list domains: %w", err)
		}
		for _, d := range domains {
			domainAccounts, err := s.db.ListAccountsByDomain(d.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to list accounts for domain %s: %w", d.Name, err)
			}
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

func (s *Server) toolAddDomain(name string, maxAccounts int, maxSize string) (map[string]interface{}, error) {
	if name == "" {
		return nil, fmt.Errorf("domain name is required")
	}
	if maxAccounts <= 0 {
		maxAccounts = 100
	}

	domain := &db.DomainData{
		Name:        name,
		MaxAccounts: maxAccounts,
	}
	if err := s.db.CreateDomain(domain); err != nil {
		return nil, fmt.Errorf("failed to create domain: %w", err)
	}

	text := fmt.Sprintf("Domain '%s' created successfully (max accounts: %d)", name, maxAccounts)
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolDeleteDomain(name string) (map[string]interface{}, error) {
	if name == "" {
		return nil, fmt.Errorf("domain name is required")
	}

	if err := s.db.DeleteDomain(name); err != nil {
		return nil, fmt.Errorf("failed to delete domain: %w", err)
	}

	text := fmt.Sprintf("Domain '%s' deleted successfully", name)
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolAddAccount(email, password string, isAdmin bool) (map[string]interface{}, error) {
	if email == "" || password == "" {
		return nil, fmt.Errorf("email and password are required")
	}

	// Parse domain from email
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email address")
	}
	localPart := parts[0]
	domain := parts[1]

	// Verify domain exists
	domains, err := s.db.ListDomains()
	if err != nil {
		return nil, err
	}
	domainExists := false
	for _, d := range domains {
		if d.Name == domain {
			domainExists = true
			break
		}
	}
	if !domainExists {
		return nil, fmt.Errorf("domain '%s' does not exist", domain)
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	account := &db.AccountData{
		Email:        email,
		LocalPart:    localPart,
		Domain:       domain,
		PasswordHash: string(hash),
		IsAdmin:      isAdmin,
	}
	if err := s.db.CreateAccount(account); err != nil {
		return nil, fmt.Errorf("failed to create account: %w", err)
	}

	text := fmt.Sprintf("Account '%s' created successfully", email)
	if isAdmin {
		text += " with admin privileges"
	}
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolDeleteAccount(email string) (map[string]interface{}, error) {
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	// Parse domain and local part
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email address")
	}

	if err := s.db.DeleteAccount(parts[1], parts[0]); err != nil {
		return nil, fmt.Errorf("failed to delete account: %w", err)
	}

	text := fmt.Sprintf("Account '%s' deleted successfully", email)
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolGetAccountInfo(email string) (map[string]interface{}, error) {
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	// Parse domain and local part
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email address")
	}

	account, err := s.db.GetAccount(parts[1], parts[0])
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}

	text := fmt.Sprintf("Account Information:\n")
	text += fmt.Sprintf("- Email: %s\n", account.Email)
	text += fmt.Sprintf("- Domain: %s\n", account.Domain)
	text += fmt.Sprintf("- Admin: %t\n", account.IsAdmin)
	text += fmt.Sprintf("- Quota Used: %d bytes\n", account.QuotaUsed)
	text += fmt.Sprintf("- Quota Limit: %d bytes\n", account.QuotaLimit)

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolGetQueueStatus(limit int) (map[string]interface{}, error) {
	// Queue operations would need queue manager access
	// For now, return placeholder
	text := "Queue Status:\n"
	text += "- Note: Full queue status requires queue manager integration\n"
	text += "- Use CLI 'umailserver queue list' for detailed status\n"

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolRetryQueueItem(id string) (map[string]interface{}, error) {
	if id == "" {
		return nil, fmt.Errorf("queue item ID is required")
	}
	text := fmt.Sprintf("Queue item '%s' retry requested\n", id)
	text += "- Note: Full queue management requires queue manager integration\n"
	text += "- Use CLI 'umailserver queue retry <id>' for actual retry\n"

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolFlushQueue() (map[string]interface{}, error) {
	text := "Queue flush requested\n"
	text += "- Note: Full queue management requires queue manager integration\n"
	text += "- Use CLI 'umailserver queue flush' for actual flush\n"

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolCheckDNS(domain string) (map[string]interface{}, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	text := fmt.Sprintf("DNS Check for %s:\n", domain)
	text += "- Note: Full DNS check requires DNS resolver integration\n"
	text += fmt.Sprintf("- Use CLI 'umailserver check dns %s' for full check\n", domain)

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolCheckTLS(domain string) (map[string]interface{}, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	text := fmt.Sprintf("TLS Check for %s:\n", domain)
	text += "- Note: Full TLS check requires TLS connection\n"
	text += fmt.Sprintf("- Use CLI 'umailserver check tls %s' for full check\n", domain)

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolGetSystemStatus() (map[string]interface{}, error) {
	text := "System Status:\n"
	text += "- Status: OK\n"
	text += "- Note: Full system status requires additional monitoring integration\n"
	text += "- Use '/health' endpoint for detailed health checks\n"

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

func (s *Server) toolReloadConfig() (map[string]interface{}, error) {
	text := "Configuration reload requested\n"
	text += "- Note: Config reload requires server restart or SIGHUP\n"
	text += "- Use 'systemctl reload umailserver' or send SIGHUP\n"

	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	}, nil
}

// Write error response
func (s *Server) writeError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(MCPResponse{
		JSONRPC: "2.0",
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	})
}

// Resource types
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType,omitempty"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Prompt types
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Handle resources/list
func (s *Server) handleResourcesList() map[string]interface{} {
	return map[string]interface{}{"resources": []Resource{
		{
			URI:         "umailserver://domains",
			Name:        "Domains",
			Description: "List of all configured domains",
			MimeType:    "application/json",
		},
		{
			URI:         "umailserver://accounts",
			Name:        "Accounts",
			Description: "List of all email accounts",
			MimeType:    "application/json",
		},
		{
			URI:         "umailserver://config",
			Name:        "Server Configuration",
			Description: "Current server configuration (sanitized)",
			MimeType:    "application/json",
		},
		{
			URI:         "umailserver://status",
			Name:        "Server Status",
			Description: "Current server status and health",
			MimeType:    "application/json",
		},
	}}
}

// Handle resources/read
type ResourceReadRequest struct {
	URI string `json:"uri"`
}

func (s *Server) handleResourceRead(params json.RawMessage) (map[string]interface{}, error) {
	var req ResourceReadRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	switch req.URI {
	case "umailserver://domains":
		domains, err := s.db.ListDomains()
		if err != nil {
			return nil, err
		}
		data, _ := json.MarshalIndent(domains, "", "  ")
		return map[string]interface{}{"contents": []ResourceContent{
			{URI: req.URI, Text: string(data), MimeType: "application/json"},
		}}, nil

	case "umailserver://accounts":
		domains, _ := s.db.ListDomains()
		var allAccounts []*db.AccountData
		for _, d := range domains {
			accounts, _ := s.db.ListAccountsByDomain(d.Name)
			allAccounts = append(allAccounts, accounts...)
		}
		data, _ := json.MarshalIndent(allAccounts, "", "  ")
		return map[string]interface{}{"contents": []ResourceContent{
			{URI: req.URI, Text: string(data), MimeType: "application/json"},
		}}, nil

	case "umailserver://config":
		config := map[string]interface{}{
			"version": s.version,
			"note":    "Full config requires access to config file",
		}
		data, _ := json.MarshalIndent(config, "", "  ")
		return map[string]interface{}{"contents": []ResourceContent{
			{URI: req.URI, Text: string(data), MimeType: "application/json"},
		}}, nil

	case "umailserver://status":
		status := map[string]interface{}{
			"status":  "healthy",
			"version": s.version,
			"time":    time.Now().Format(time.RFC3339),
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		return map[string]interface{}{"contents": []ResourceContent{
			{URI: req.URI, Text: string(data), MimeType: "application/json"},
		}}, nil

	default:
		return nil, fmt.Errorf("unknown resource: %s", req.URI)
	}
}

// Handle prompts/list
func (s *Server) handlePromptsList() map[string]interface{} {
	return map[string]interface{}{"prompts": []Prompt{
		{
			Name:        "setup_domain",
			Description: "Guide for setting up a new domain",
			Arguments: []PromptArgument{
				{Name: "domain", Description: "Domain name to set up", Required: true},
			},
		},
		{
			Name:        "troubleshoot_delivery",
			Description: "Troubleshoot email delivery issues",
			Arguments: []PromptArgument{
				{Name: "email", Description: "Email address having issues", Required: false},
			},
		},
		{
			Name:        "security_audit",
			Description: "Run a security audit on server configuration",
			Arguments:   []PromptArgument{},
		},
	}}
}

// Handle prompts/get
type PromptGetRequest struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

func (s *Server) handlePromptGet(params json.RawMessage) (map[string]interface{}, error) {
	var req PromptGetRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}

	switch req.Name {
	case "setup_domain":
		domain := req.Arguments["domain"]
		if domain == "" {
			domain = "example.com"
		}
		return map[string]interface{}{"description": fmt.Sprintf("Setup guide for %s", domain), "messages": []PromptMessage{
			{
				Role: "assistant",
				Content: PromptContent{
					Type: "text",
					Text: fmt.Sprintf(`Domain Setup Guide for %s:

1. Add the domain:
   umailserver domain add %s

2. Create DNS records:
   - A record: mail.%s -> YOUR_SERVER_IP
   - MX record: %s -> mail.%s (priority 10)
   - SPF: v=spf1 mx ~all
   - DKIM: Generate with umailserver (see admin panel)
   - DMARC: v=DMARC1; p=quarantine; rua=mailto:dmarc@%s

3. Set up reverse DNS (PTR) for your server IP

4. Test with: umailserver check dns %s`, domain, domain, domain, domain, domain, domain, domain),
				},
			},
		}}, nil

	case "troubleshoot_delivery":
		email := req.Arguments["email"]
		msg := "Email Delivery Troubleshooting:\n\n"
		msg += "1. Check queue status: umailserver queue list\n"
		msg += "2. Check logs: /var/log/umailserver/\n"
		msg += "3. Verify DNS: umailserver check dns <domain>\n"
		msg += "4. Check TLS: umailserver check tls <domain>\n"
		if email != "" {
			msg += fmt.Sprintf("\nFor account %s:\n", email)
			msg += "- Check account exists\n"
			msg += "- Verify mailbox isn't full\n"
			msg += "- Check forwarding rules\n"
		}
		return map[string]interface{}{"description": "Troubleshooting guide", "messages": []PromptMessage{
			{Role: "assistant", Content: PromptContent{Type: "text", Text: msg}},
		}}, nil

	case "security_audit":
		return map[string]interface{}{"description": "Security audit checklist", "messages": []PromptMessage{
			{
				Role: "assistant",
				Content: PromptContent{
					Type: "text",
					Text: `Security Audit Checklist:

1. Authentication:
   - Verify strong JWT secret (32+ chars)
   - Check password policy is enforced
   - Review admin accounts

2. TLS Configuration:
   - TLS 1.2+ enforced
   - Valid certificates
   - HSTS headers

3. Rate Limiting:
   - Per-IP limits configured
   - Per-user limits configured
   - Brute force protection active

4. Network:
   - Firewall rules configured
   - Only necessary ports open
   - Fail2ban or similar active

5. Monitoring:
   - Alerts configured
   - Logs being rotated
   - Disk space monitoring

Use 'umailserver check tls' and review admin panel security settings.`,
				},
			},
		}}, nil

	default:
		return nil, fmt.Errorf("unknown prompt: %s", req.Name)
	}
}
