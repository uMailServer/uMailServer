package api

import (
	"crypto/sha256"
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

// totpAttempt tracks failed TOTP attempts per account
type totpAttempt struct {
	count    int
	lastSeen time.Time
}

const maxTOTPFailures = 5
const totpLockoutDuration = 5 * time.Minute

// isTOTPLockedOut returns true if the account has exceeded TOTP failure limits.
func (s *Server) isTOTPLockedOut(email string) bool {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	if s.totpAttempts == nil {
		s.totpAttempts = make(map[string]*totpAttempt)
	}

	attempt := s.totpAttempts[email]
	if attempt.count >= maxTOTPFailures && time.Since(attempt.lastSeen) < totpLockoutDuration {
		return true
	}
	return false
}

// recordTOTPFailure increments the failed TOTP attempt count for an account.
func (s *Server) recordTOTPFailure(email string) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	if s.totpAttempts == nil {
		s.totpAttempts = make(map[string]*totpAttempt)
	}

	attempt := s.totpAttempts[email]
	if attempt == nil {
		attempt = &totpAttempt{}
	}
	attempt.count++
	attempt.lastSeen = time.Now()
	s.totpAttempts[email] = attempt
}

// clearTOTPFailures resets the TOTP failure count for an account.
func (s *Server) clearTOTPFailures(email string) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	delete(s.totpAttempts, email)
}

// clearAccountLoginFailures resets the account login failure count.
func (s *Server) clearAccountLoginFailures(email string) {
	s.accountLoginMu.Lock()
	defer s.accountLoginMu.Unlock()

	delete(s.accountLoginAttempts, email)
}

// RevokeToken adds a token to the blacklist (for logout).
// If a database is configured, the revocation is persisted; otherwise it falls back to memory.
func (s *Server) RevokeToken(tokenHash string) {
	expiry := time.Now().Add(time.Hour)
	if s.db != nil {
		if err := s.db.StoreRevokedToken(tokenHash, expiry); err != nil {
			// Fall back to in-memory on DB error
			s.tokenBlacklistMu.Lock()
			defer s.tokenBlacklistMu.Unlock()
			if s.tokenBlacklist == nil {
				s.tokenBlacklist = make(map[string]time.Time)
			}
			s.tokenBlacklist[tokenHash] = expiry
		}
		return
	}
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	if s.tokenBlacklist == nil {
		s.tokenBlacklist = make(map[string]time.Time)
	}
	s.tokenBlacklist[tokenHash] = expiry
}

// IsTokenRevoked checks if a token is in the blacklist.
// When a database is available the check is persistent; otherwise it uses the in-memory map.
// DB errors are treated as revoked (fail-secure).
func (s *Server) IsTokenRevoked(tokenHash string) bool {
	if s.db != nil {
		revoked, err := s.db.IsTokenRevoked(tokenHash)
		if err != nil {
			return true
		}
		return revoked
	}
	s.tokenBlacklistMu.Lock()
	defer s.tokenBlacklistMu.Unlock()
	if expiry, ok := s.tokenBlacklist[tokenHash]; ok {
		if time.Now().After(expiry) {
			delete(s.tokenBlacklist, tokenHash)
			return false
		}
		return true
	}
	return false
}

// CleanupExpiredTokens removes expired entries from the blacklist.
func (s *Server) CleanupExpiredTokens() {
	if s.db != nil {
		_ = s.db.CleanupRevokedTokens()
		return
	}
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

// checkAccountLoginRateLimit returns true if the account is allowed to attempt login.
// Allows 5 attempts per 5-minute window per account; blocks after that.
func (s *Server) checkAccountLoginRateLimit(email string) bool {
	s.accountLoginMu.Lock()
	defer s.accountLoginMu.Unlock()

	if s.accountLoginAttempts == nil {
		s.accountLoginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.accountLoginAttempts[email]
	if !exists || now.Sub(attempt.lastSeen) > 5*time.Minute {
		s.accountLoginAttempts[email] = &loginAttempt{count: 1, lastSeen: now}
		return true
	}

	if attempt.count >= 5 {
		return false
	}
	attempt.count++
	attempt.lastSeen = now
	return true
}

// recordAccountLoginFailure increments the failed login counter for an account.
func (s *Server) recordAccountLoginFailure(email string) {
	s.accountLoginMu.Lock()
	defer s.accountLoginMu.Unlock()

	if s.accountLoginAttempts == nil {
		s.accountLoginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.accountLoginAttempts[email]
	if !exists {
		s.accountLoginAttempts[email] = &loginAttempt{count: 1, lastSeen: now}
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

	// Normalize email for rate limiting
	emailKey := strings.ToLower(req.Email)

	// Rate limit login attempts by account
	if !s.checkAccountLoginRateLimit(emailKey) {
		s.sendError(w, http.StatusTooManyRequests, "too many login attempts for this account")
		return
	}

	// Parse email
	user, domain := parseEmail(req.Email)

	// Get account
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.recordLoginFailure(ip)
		s.recordAccountLoginFailure(emailKey)
		s.auditLogger.LogLoginFailure(req.Email, ip, "account_not_found")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Check password using configured hasher
	matches, needsRehash := s.verifyPassword(req.Password, account.PasswordHash)
	if !matches {
		s.recordLoginFailure(ip)
		s.recordAccountLoginFailure(emailKey)
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
		if s.isTOTPLockedOut(req.Email) {
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_locked_out")
			s.sendError(w, http.StatusTooManyRequests, "too many failed TOTP attempts")
			return
		}
		totpSecret, err := auth.DecryptTOTPSecret(account.TOTPSecret, s.config.JWTSecret)
		if err != nil {
			s.logger.Error("failed to decrypt TOTP secret", "error", err, "email", req.Email)
			s.sendError(w, http.StatusInternalServerError, "authentication error")
			return
		}
		valid, step := auth.ValidateTOTPAtWithStep(totpSecret, req.TOTPCode, time.Now(), auth.TOTPAlgorithmSHA1)
		if !valid {
			s.recordTOTPFailure(req.Email)
			s.recordAccountLoginFailure(emailKey)
			s.auditLogger.LogLoginFailure(req.Email, ip, "invalid_totp")
			s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
			return
		}
		// Replay protection: reject reuse of the same time step
		if step <= account.TOTPLastUsedStep {
			s.recordTOTPFailure(req.Email)
			s.recordAccountLoginFailure(emailKey)
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_replay")
			s.sendError(w, http.StatusUnauthorized, "TOTP code already used")
			return
		}
		account.TOTPLastUsedStep = step
		if err := s.db.UpdateAccount(account); err != nil {
			s.logger.Error("failed to update TOTP last used step", "error", err, "email", req.Email)
		}
		s.clearTOTPFailures(req.Email)
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

	// Clear login failures on successful login
	s.clearAccountLoginFailures(emailKey)

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
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenStr)))
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

	// Revoke the old token by adding it to the blacklist
	authHeader := r.Header.Get("Authorization")
	if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		oldTokenStr := parts[1]
		oldTokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(oldTokenStr)))
		s.RevokeToken(oldTokenHash)
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
