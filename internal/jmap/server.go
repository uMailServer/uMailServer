// Package jmap provides JMAP (RFC 8620/8621) protocol support
package jmap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/umailserver/umailserver/internal/tracing"
)

// idCounter is used to ensure unique IDs even when called rapidly
var idCounter uint64

// maxJMAPAPIRequestBodySize bounds the JSON method-call request body,
// mirroring handleUpload's upload budget.
const maxJMAPAPIRequestBodySize = 32 << 20 // 32 MiB

// maxCallsInRequest and maxObjectsInGet are the limits advertised in the
// session's urn:ietf:params:jmap:core capability; RFC 8620 §3.6.1 and §5.1
// require the server to reject requests that exceed them (F4995, F4996).
const (
	maxCallsInRequest = 16
	maxObjectsInGet   = 256
)

// Server represents a JMAP server
type Server struct {
	logger          *slog.Logger
	config          Config
	db              *storage.Database
	msgStore        *storage.MessageStore
	sessions        map[string]*Session
	sessionMu       sync.RWMutex
	tracingProvider *tracing.Provider
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each
// JMAP method call emits a child span. Nil disables tracing without overhead.
func (s *Server) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
}

// Config holds JMAP server configuration
type Config struct {
	JWTSecret   string
	TokenExpiry time.Duration
	CorsOrigins []string // Allowed CORS origins; if empty, allows all
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

// safeError logs the actual error server-side and returns a generic
// description suitable for exposure to JMAP clients.
func (s *Server) safeError(context string, err error) string {
	s.logger.Error("jmap error", "context", context, "error", err)
	return "internal server error"
}

// ServeHTTP implements the http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.logger.Debug("JMAP request",
		"method", r.Method,
		"path", r.URL.Path,
	)

	// CORS headers - secure by default (no wildcard)
	corsOrigin := ""
	origin := r.Header.Get("Origin")
	for _, allowed := range s.config.CorsOrigins {
		if allowed == origin {
			corsOrigin = allowed
			break
		}
	}
	if corsOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
	}
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
	case path == "/jmap/upload" || strings.HasPrefix(path, "/jmap/upload/"):
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

	// Parse request body. The API endpoint must enforce the same class of
	// budget as handleUpload: without a cap, any authenticated user can make
	// the server buffer an unbounded request body (memory exhaustion).
	limitedBody := http.MaxBytesReader(w, r.Body, maxJMAPAPIRequestBodySize)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.sendError(w, http.StatusRequestEntityTooLarge, "requestTooLarge", nil)
			return
		}
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}
	defer r.Body.Close()

	var request Request
	if err := json.Unmarshal(body, &request); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}

	// RFC 8620 §3.6.1: a request exceeding maxCallsInRequest is rejected
	// as a whole with a limit error (F4995).
	if len(request.MethodCalls) > maxCallsInRequest {
		s.sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"type":   "urn:ietf:params:jmap:error:limit",
			"status": http.StatusBadRequest,
			"limit":  "maxCallsInRequest",
		})
		return
	}

	// Process method calls, resolving RFC 8620 §3.7 result references
	// against the responses already produced in this request (F4997).
	var responses []Response
	for _, call := range request.MethodCalls {
		var response Response
		if resolved, errResp, ok := resolveResultReferences(call, responses); ok {
			response = s.processMethodCall(user, resolved)
		} else {
			response = errResp
		}
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

	// F5149: the session advertises uploadUrl /jmap/upload/{accountId}.
	// When the account segment is present it must be the caller's own
	// account, matching the download endpoint's ownership check.
	if rest := strings.TrimPrefix(r.URL.Path, "/jmap/upload"); rest != "" && rest != "/" {
		if strings.Trim(rest, "/") != user {
			s.sendError(w, http.StatusForbidden, "forbidden", nil)
			return
		}
	}

	// Read upload data (limit to 50MB to prevent DoS)
	limitedReader := http.MaxBytesReader(w, r.Body, 50<<20)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalidArguments", nil)
		return
	}
	defer r.Body.Close()

	// F5149: persist the blob so its blobId can be used afterwards (RFC 8620
	// §6.1), e.g. by Email/import, which resolves blobIds through the
	// message store. The store is content-addressed and its id is the
	// blobId. Without a store (test wiring) the id is only computed.
	blobID := generateBlobID(data)
	if s.msgStore != nil {
		id, err := s.msgStore.StoreMessage(user, data)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "serverFail", nil)
			return
		}
		blobID = id
	}

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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// #nosec G705 -- Content-Type is explicitly set to non-HTML via DetectContentType and nosniff
	_, _ = w.Write(data)
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
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		_, span := s.tracingProvider.StartSpanWithKind(context.Background(), "jmap."+call.Name, tracing.SpanKindServer)
		defer span.End()
		tracing.SetStringAttribute(span, "jmap.method", call.Name)
		tracing.SetStringAttribute(span, "jmap.user", user)
		if call.ID != "" {
			tracing.SetStringAttribute(span, "jmap.call_id", call.ID)
		}
		resp := s.dispatchMethodCall(user, call)
		if errType, ok := resp.Args["type"].(string); ok && errType != "" {
			tracing.SetStringAttribute(span, "jmap.error", errType)
			tracing.SetStatus(span, tracing.StatusError, errType)
		} else {
			tracing.SetStatus(span, tracing.StatusOk, "")
		}
		return resp
	}
	return s.dispatchMethodCall(user, call)
}

// dispatchMethodCall is the core method dispatch shared by traced and untraced
// processing.
func (s *Server) dispatchMethodCall(user string, call MethodCall) Response {
	// RFC 8620 §5.1 / RFC 8621 §5.1: more ids than maxObjectsInGet MUST be
	// answered with requestTooLarge (F4996).
	idsKey := ""
	switch call.Name {
	case "Mailbox/get", "Email/get", "Thread/get", "Identity/get":
		idsKey = "ids"
	case "SearchSnippet/get":
		idsKey = "emailIds"
	}
	if idsKey != "" {
		if ids, ok := call.Args[idsKey].([]interface{}); ok && len(ids) > maxObjectsInGet {
			return Response{
				Name: "error",
				Args: map[string]interface{}{"type": "requestTooLarge"},
				ID:   call.ID,
			}
		}
	}

	switch call.Name {
	// Mailbox methods
	case "Mailbox/get":
		return s.handleMailboxGet(user, call)
	case "Mailbox/query":
		return s.handleMailboxQuery(user, call)
	case "Mailbox/set":
		return s.handleMailboxSet(user, call)
	case "Mailbox/changes":
		return s.handleMailboxChanges(user, call)
	case "Mailbox/queryChanges":
		return s.handleMailboxQueryChanges(user, call)

	// Email methods
	case "Email/get":
		return s.handleEmailGet(user, call)
	case "Email/query":
		return s.handleEmailQuery(user, call)
	case "Email/set":
		return s.handleEmailSet(user, call)
	case "Email/import":
		return s.handleEmailImport(user, call)
	case "Email/changes":
		return s.handleEmailChanges(user, call)
	case "Email/queryChanges":
		return s.handleEmailQueryChanges(user, call)

	// Thread methods
	case "Thread/get":
		return s.handleThreadGet(user, call)
	case "Thread/query":
		return s.handleThreadQuery(user, call)
	case "Thread/changes":
		return s.handleThreadChanges(user, call)
	case "Thread/queryChanges":
		return s.handleThreadQueryChanges(user, call)

	// Search methods
	case "SearchSnippet/get":
		return s.handleSearchSnippetGet(user, call)

	// Identity methods
	case "Identity/get":
		return s.handleIdentityGet(user, call)
	case "Identity/set":
		return s.handleIdentitySet(user, call)
	case "Identity/changes":
		return s.handleIdentityChanges(user, call)
	case "Identity/query":
		return s.handleIdentityQuery(user, call)
	case "Identity/queryChanges":
		return s.handleIdentityQueryChanges(user, call)

	default:
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "unknownMethod"},
			ID:   call.ID,
		}
	}
}

// resolveResultReferences replaces every "#name" argument (RFC 8620 §3.7
// ResultReference) with the value selected from an earlier response in the
// same request. An unresolvable reference — unknown call id, name mismatch
// (including a call that failed with an error response), or a bad path —
// yields an invalidResultReference error instead of running the method with
// the argument silently missing (F4997).
func resolveResultReferences(call MethodCall, prior []Response) (MethodCall, Response, bool) {
	fail := func(errType, desc string) (MethodCall, Response, bool) {
		return call, Response{
			Name: "error",
			Args: map[string]interface{}{"type": errType, "description": desc},
			ID:   call.ID,
		}, false
	}
	var args map[string]interface{}
	for key, val := range call.Args {
		if !strings.HasPrefix(key, "#") {
			continue
		}
		name := key[1:]
		if _, dup := call.Args[name]; dup {
			return fail("invalidArguments", "argument "+name+" given both directly and as a result reference")
		}
		ref, _ := val.(map[string]interface{})
		resultOf, _ := ref["resultOf"].(string)
		refName, _ := ref["name"].(string)
		path, pathOK := ref["path"].(string)
		if resultOf == "" || refName == "" || !pathOK {
			return fail("invalidResultReference", "malformed result reference for "+name)
		}
		var target *Response
		for i := range prior {
			if prior[i].ID == resultOf {
				target = &prior[i]
				break
			}
		}
		if target == nil || target.Name != refName {
			return fail("invalidResultReference", "no "+refName+" response with call id "+resultOf)
		}
		raw, err := json.Marshal(target.Args)
		if err != nil {
			return fail("invalidResultReference", "unencodable referenced result")
		}
		var doc interface{}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fail("invalidResultReference", "unencodable referenced result")
		}
		value, ok := evalResultPointer(doc, path)
		if !ok {
			return fail("invalidResultReference", "path "+path+" does not resolve")
		}
		if args == nil {
			args = make(map[string]interface{}, len(call.Args))
			for k, v := range call.Args {
				args[k] = v
			}
		}
		delete(args, key)
		args[name] = value
	}
	if args != nil {
		call.Args = args
	}
	return call, Response{}, true
}

// evalResultPointer evaluates an RFC 6901 JSON Pointer extended with the
// RFC 8620 §3.7 "*" array wildcard (results are flattened one level).
func evalResultPointer(doc interface{}, path string) (interface{}, bool) {
	if path == "" {
		return doc, true
	}
	if !strings.HasPrefix(path, "/") {
		return nil, false
	}
	tokens := strings.Split(path[1:], "/")
	for i, t := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return evalResultTokens(doc, tokens)
}

func evalResultTokens(doc interface{}, tokens []string) (interface{}, bool) {
	if len(tokens) == 0 {
		return doc, true
	}
	tok, rest := tokens[0], tokens[1:]
	switch v := doc.(type) {
	case map[string]interface{}:
		child, ok := v[tok]
		if !ok {
			return nil, false
		}
		return evalResultTokens(child, rest)
	case []interface{}:
		if tok == "*" {
			out := []interface{}{}
			for _, item := range v {
				r, ok := evalResultTokens(item, rest)
				if !ok {
					return nil, false
				}
				if arr, isArr := r.([]interface{}); isArr {
					out = append(out, arr...)
				} else {
					out = append(out, r)
				}
			}
			return out, true
		}
		idx := 0
		if tok == "" || (len(tok) > 1 && tok[0] == '0') {
			return nil, false
		}
		for _, c := range tok {
			if c < '0' || c > '9' {
				return nil, false
			}
			idx = idx*10 + int(c-'0')
			if idx >= len(v) {
				return nil, false
			}
		}
		return evalResultTokens(v[idx], rest)
	}
	return nil, false
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
	_ = json.NewEncoder(w).Encode(data)
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

// generateSessionID generates a unique session ID using crypto/rand
func generateSessionID() string {
	counter := atomic.AddUint64(&idCounter, 1)

	// Generate 16 random bytes for uniqueness
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp+counter if crypto/rand fails (should not happen)
		return fmt.Sprintf("session-%d-%d", time.Now().UnixNano(), counter)
	}
	return fmt.Sprintf("session-%s-%d", hex.EncodeToString(b), counter)
}
