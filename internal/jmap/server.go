// Package jmap provides JMAP (RFC 8620/8621) protocol support
package jmap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/storage"
)

// idCounter is used to ensure unique IDs even when called rapidly
var idCounter uint64

// Server represents a JMAP server
type Server struct {
	logger    *slog.Logger
	config    Config
	db        *storage.Database
	msgStore  *storage.MessageStore
	sessions  map[string]*Session
	sessionMu sync.RWMutex
}

// Config holds JMAP server configuration
type Config struct {
	JWTSecret   string
	TokenExpiry time.Duration
}

// Session represents a JMAP session
type Session struct {
	ID         string
	User       string
	CreatedAt  time.Time
	LastActive time.Time
}

// NewServer creates a new JMAP server
func NewServer(db *storage.Database, msgStore *storage.MessageStore, logger *slog.Logger, config Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if config.TokenExpiry == 0 {
		config.TokenExpiry = 24 * time.Hour
	}

	return &Server{
		logger:   logger,
		config:   config,
		db:       db,
		msgStore: msgStore,
		sessions: make(map[string]*Session),
	}
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("JMAP request",
		"method", r.Method,
		"path", r.URL.Path,
	)

	// CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route based on path
	path := r.URL.Path
	switch {
	case path == "/.well-known/jmap":
		s.handleWellKnown(w, r)
	case path == "/jmap/session":
		s.handleSession(w, r)
	case path == "/jmap/api":
		s.handleAPI(w, r)
	case path == "/jmap/upload":
		s.handleUpload(w, r)
	case strings.HasPrefix(path, "/jmap/download"):
		s.handleDownload(w, r)
	case path == "/jmap/events":
		s.handleEvents(w, r)
	default:
		s.sendError(w, http.StatusNotFound, "notFound", nil)
	}
}

// handleWellKnown handles /.well-known/jmap requests
func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	response := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"urn:ietf:params:jmap:core": map[string]interface{}{
				"maxSizeUpload":         50 * 1024 * 1024, // 50MB
				"maxConcurrentUpload":   4,
				"maxSizeRequest":        10 * 1024 * 1024, // 10MB
				"maxConcurrentRequests": 4,
				"maxCallsInRequest":     16,
				"maxObjectsInGet":       256,
				"maxObjectsInSet":       128,
				"collationAlgorithms":   []string{"i;unicode-casemap"},
			},
			"urn:ietf:params:jmap:mail": map[string]interface{}{
				"maxMailboxesPerEmail":       100,
				"maxMailboxDepth":            10,
				"maxSizeMailboxName":         256,
				"maxSizeAttachmentsPerEmail": 100 * 1024 * 1024, // 100MB
				"emailQuerySortOptions":      []string{"receivedAt", "sentAt", "from", "to", "subject", "size"},
			},
		},
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handleSession handles /jmap/session requests
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	// Authenticate
	user, ok := s.authenticate(r)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalidCredentials", nil)
		return
	}

	session := s.getOrCreateSession(user)

	response := SessionResponse{
		Capabilities: map[string]interface{}{
			"urn:ietf:params:jmap:core": CoreCapabilities{
				MaxSizeUpload:         50 * 1024 * 1024,
				MaxConcurrentUpload:   4,
				MaxSizeRequest:        10 * 1024 * 1024,
				MaxConcurrentRequests: 4,
				MaxCallsInRequest:     16,
				MaxObjectsInGet:       256,
				MaxObjectsInSet:       128,
				CollationAlgorithms:   []string{"i;unicode-casemap"},
			},
			"urn:ietf:params:jmap:mail": MailCapabilities{
				MaxMailboxesPerEmail:       100,
				MaxMailboxDepth:            10,
				MaxSizeMailboxName:         256,
				MaxSizeAttachmentsPerEmail: 100 * 1024 * 1024,
				EmailQuerySortOptions:      []string{"receivedAt", "sentAt", "from", "to", "subject", "size"},
			},
		},
		Accounts: map[string]Account{
			user: {
				Name:      user,
				IsPrimary: true,
				AccountCapabilities: map[string]interface{}{
					"urn:ietf:params:jmap:mail": struct{}{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			"urn:ietf:params:jmap:mail": user,
		},
		Username:       user,
		APIURL:         "/jmap/api",
		DownloadURL:    "/jmap/download/{accountId}/{blobId}/{name}?accept={type}",
		UploadURL:      "/jmap/upload/{accountId}",
		EventSourceURL: "/jmap/events/{types}?ping={interval}&closeafter={closeafter}",
		State:          session.ID,
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handleAPI handles /jmap/api requests (method calls)
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	// Authenticate
	user, ok := s.authenticate(r)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalidCredentials", nil)
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}
	defer r.Body.Close()

	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}

	// Process method calls
	var responses []Response
	for _, call := range request.MethodCalls {
		response := s.processMethodCall(user, call)
		responses = append(responses, response)
	}

	result := ResponseObject{
		SessionState:    s.getOrCreateSession(user).ID,
		MethodResponses: responses,
	}

	s.sendJSON(w, http.StatusOK, result)
}

// handleUpload handles /jmap/upload requests
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	// Authenticate
	user, ok := s.authenticate(r)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalidCredentials", nil)
		return
	}

	// Read upload data
	data, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}
	defer r.Body.Close()

	// Generate blob ID
	blobID := generateBlobID(data)

	// Store blob (in production, store to blob storage)
	s.logger.Debug("Upload received",
		"user", user,
		"blobID", blobID,
		"size", len(data),
		"type", r.Header.Get("Content-Type"),
	)

	response := UploadResponse{
		AccountID: user,
		BlobID:    blobID,
		Type:      r.Header.Get("Content-Type"),
		Size:      len(data),
	}

	s.sendJSON(w, http.StatusCreated, response)
}

// handleDownload handles /jmap/download requests
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	// Authenticate
	user, ok := s.authenticate(r)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalidCredentials", nil)
		return
	}

	// Parse URL parameters
	// Format: /jmap/download/{accountId}/{blobId}/{name}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}

	accountID := parts[3]
	blobID := parts[4]

	// Verify account ownership
	if accountID != user {
		s.sendError(w, http.StatusForbidden, "forbidden", nil)
		return
	}

	// Retrieve message from storage using blobID as messageID
	data, err := s.msgStore.ReadMessage(user, blobID)
	if err != nil {
		s.logger.Debug("Blob not found",
			"user", user,
			"blobID", blobID,
			"error", err,
		)
		s.sendError(w, http.StatusNotFound, "blobNotFound", nil)
		return
	}

	// Set content type based on data
	contentType := http.DetectContentType(data)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleEvents handles /jmap/events (EventSource for push)
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "invalidArguments", nil)
		return
	}

	// Authenticate
	user, ok := s.authenticate(r)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalidCredentials", nil)
		return
	}

	// Set up EventSource
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial state
	fmt.Fprintf(w, "event: state\n")
	fmt.Fprintf(w, "data: %s\n\n", s.getOrCreateSession(user).ID)
	w.(http.Flusher).Flush()

	// Keep connection open for push notifications
	// In production, use a proper event bus
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: ping\n")
			fmt.Fprintf(w, "data: {}\n\n")
			w.(http.Flusher).Flush()
		}
	}
}

// processMethodCall processes a single JMAP method call
func (s *Server) processMethodCall(user string, call MethodCall) Response {
	switch call.Name {
	case "Mailbox/get":
		return s.handleMailboxGet(user, call)
	case "Mailbox/query":
		return s.handleMailboxQuery(user, call)
	case "Mailbox/set":
		return s.handleMailboxSet(user, call)
	case "Email/get":
		return s.handleEmailGet(user, call)
	case "Email/query":
		return s.handleEmailQuery(user, call)
	case "Email/set":
		return s.handleEmailSet(user, call)
	case "Email/import":
		return s.handleEmailImport(user, call)
	case "Thread/get":
		return s.handleThreadGet(user, call)
	case "SearchSnippet/get":
		return s.handleSearchSnippetGet(user, call)
	case "Identity/get":
		return s.handleIdentityGet(user, call)
	case "Identity/set":
		return s.handleIdentitySet(user, call)
	default:
		return Response{
			Name: "error",
			Args: map[string]interface{}{
				"type": "unknownMethod",
			},
		}
	}
}

// authenticate authenticates a request
func (s *Server) authenticate(r *http.Request) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", false
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", false
	}

	token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return "", false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", false
	}

	user, ok := claims["sub"].(string)
	if !ok || user == "" {
		return "", false
	}

	return user, true
}

// getOrCreateSession gets or creates a session for a user
func (s *Server) getOrCreateSession(user string) *Session {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	for _, session := range s.sessions {
		if session.User == user {
			session.LastActive = time.Now()
			return session
		}
	}

	session := &Session{
		ID:         generateSessionID(),
		User:       user,
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}
	s.sessions[session.ID] = session

	return session
}

// sendJSON sends a JSON response
func (s *Server) sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// sendError sends a JMAP error response
func (s *Server) sendError(w http.ResponseWriter, status int, errType string, details interface{}) {
	response := map[string]interface{}{
		"type": errType,
	}
	if details != nil {
		response["details"] = details
	}
	s.sendJSON(w, status, response)
}

// generateBlobID generates a unique blob ID based on content hash
func generateBlobID(data []byte) string {
	hash := sha256.Sum256(data)
	return "blob-" + hex.EncodeToString(hash[:16])
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	counter := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("session-%d-%d", time.Now().UnixNano(), counter)
}
