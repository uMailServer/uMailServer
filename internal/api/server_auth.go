package api

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/auth"
)

// loginAttempt tracks failed login attempts per IP
type loginAttempt struct {
	count    int
	lastSeen time.Time
}

// apiRateAttempt tracks API requests per IP for rate limiting
type apiRateAttempt struct {
	count       int
	windowStart time.Time
}

// RevokeToken adds a token to the blacklist (for logout)
func (s *Server) RevokeToken(tokenHash string) {
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	// Blacklist expires when the token would have expired (1 hour from now for typical tokens)
	s.tokenBlacklist[tokenHash] = time.Now().Add(time.Hour)
}

// IsTokenRevoked checks if a token is in the blacklist
func (s *Server) IsTokenRevoked(tokenHash string) bool {
	s.tokenBlacklistMu.RLock()
	defer s.tokenBlacklistMu.RUnlock()
	if expiry, ok := s.tokenBlacklist[tokenHash]; ok {
		if time.Now().After(expiry) {
			return false // Expired, remove from blacklist
		}
		return true
	}
	return false
}

// CleanupExpiredTokens removes expired entries from the blacklist
func (s *Server) CleanupExpiredTokens() {
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	now := time.Now()
	for token, expiry := range s.tokenBlacklist {
		if now.After(expiry) {
			delete(s.tokenBlacklist, token)
		}
	}
}

// checkLoginRateLimit returns true if the IP is allowed to attempt login.
// Allows 5 attempts per 5-minute window per IP; blocks after that.
func (s *Server) checkLoginRateLimit(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists || now.Sub(attempt.lastSeen) > 5*time.Minute {
		s.loginAttempts[ip] = &loginAttempt{count: 1, lastSeen: now}
		return true
	}

	if attempt.count >= 5 {
		return false
	}
	attempt.count++
	attempt.lastSeen = now
	return true
}

// recordLoginFailure increments the failed login counter for an IP.
func (s *Server) recordLoginFailure(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists {
		s.loginAttempts[ip] = &loginAttempt{count: 1, lastSeen: now}
		return
	}
	attempt.count++
	attempt.lastSeen = now
}

// handleLogin authenticates a user and returns a JWT token
//
//	@Summary Login
//	@Description Authenticates a user with email and password, returns JWT token
//	@Tags Auth
//	@Accept json
//	@Produce json
//	@Param request body object{email=string,password=string,totp_code=string} true "Credentials"
//	@Success 200 {object} map[string]interface{} "Login successful"
//	@Failure 400 {object} map[string]interface{} "Invalid request"
//	@Failure 401 {object} map[string]interface{} "Invalid credentials"
//	@Failure 429 {object} map[string]interface{} "Too many login attempts"
//	@Router /api/v1/login [post]
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Rate limit login attempts by IP
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if !s.checkLoginRateLimit(ip) {
		s.sendError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Parse email
	user, domain := parseEmail(req.Email)

	// Get account
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.recordLoginFailure(ip)
		s.auditLogger.LogLoginFailure(req.Email, ip, "account_not_found")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check password using configured hasher
	matches, needsRehash := s.verifyPassword(req.Password, account.PasswordHash)
	if !matches {
		s.recordLoginFailure(ip)
		s.auditLogger.LogLoginFailure(req.Email, ip, "invalid_password")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Rehash password if using older algorithm and argon2id is preferred
	if needsRehash {
		newHash, err := s.hashPassword(req.Password)
		if err == nil {
			account.PasswordHash = newHash
			s.db.UpdateAccount(account)
		}
	}

	// Check TOTP if enabled
	if account.TOTPEnabled {
		if req.TOTPCode == "" {
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_required")
			s.sendError(w, http.StatusUnauthorized, "TOTP code required")
			return
		}
		if !auth.ValidateTOTP(account.TOTPSecret, req.TOTPCode) {
			s.auditLogger.LogLoginFailure(req.Email, ip, "invalid_totp")
			s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
			return
		}
	}

	// Generate JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   account.Email,
		"admin": account.IsAdmin,
		"exp":   time.Now().Add(s.config.TokenExpiry).Unix(),
		"iat":   time.Now().Unix(),
	})
	// Set key ID header for secret rotation support
	token.Header["kid"] = s.currentKid

	tokenString, err := token.SignedString([]byte(s.jwtSecrets[s.currentKid]))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})

	// Initialize demo emails for the user on first login
	InitDemoEmails(account.Email)

	// Audit successful login
	s.auditLogger.LogLoginSuccess(account.Email, ip)
}

// handleLogout revokes the current token (adds it to blacklist)
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		s.sendError(w, http.StatusUnauthorized, "invalid authorization header")
		return
	}

	tokenStr := parts[1]

	// Revoke the token by adding it to blacklist
	tokenHash := fmt.Sprintf("%x", md5.Sum([]byte(tokenStr)))
	s.RevokeToken(tokenHash)

	// Audit logout
	user := r.Context().Value("user")
	if user != nil {
		s.auditLogger.LogLogout(user.(string), audit.ExtractIP(r))
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"message": "logged out successfully",
	})
}

// handleRefresh refreshes the JWT token
//
//	@Summary Refresh token
//	@Description Returns a new JWT token with extended expiry
//	@Tags Auth
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {object} map[string]interface{} "Token refreshed"
//	@Failure 401 {object} map[string]interface{} "Unauthorized"
//	@Router /api/v1/refresh [post]
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// The auth middleware already validated the token
	user := r.Context().Value("user")
	isAdmin := r.Context().Value("isAdmin")

	// Generate new token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user,
		"admin": isAdmin,
		"exp":   time.Now().Add(s.config.TokenExpiry).Unix(),
		"iat":   time.Now().Unix(),
	})
	token.Header["kid"] = s.currentKid

	tokenString, err := token.SignedString([]byte(s.jwtSecrets[s.currentKid]))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})
}
