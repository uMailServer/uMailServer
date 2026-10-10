package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// dummyPasswordHash is generated dynamically at startup to prevent timing attacks
// when an account does not exist. Using a static hash in source code would allow
// attackers with source access to confirm the dummy value and refine timing attacks.
var dummyPasswordHash string

func init() {
	// Generate a random password and hash it with bcrypt to produce a valid
	// dummy hash. This is called at package init time so the hash is ready
	// before any authentication requests arrive.
	dummyPwd := make([]byte, 32)
	if _, err := rand.Read(dummyPwd); err != nil {
		panic("failed to generate dummy password: " + err.Error())
	}
	// bcrypt cost 4 is intentionally low here since this hash is never user data;
	// we just need a valid bcrypt structure for timing-safe comparison.
	hash, err := bcrypt.GenerateFromPassword(dummyPwd, 4)
	if err != nil {
		panic("failed to hash dummy password: " + err.Error())
	}
	dummyPasswordHash = string(hash)
}

// loginAttempt tracks failed login attempts per IP with exponential backoff
type loginAttempt struct {
	count        int       // consecutive failures
	lastSeen     time.Time // last attempt timestamp
	lockoutUntil time.Time // lockout expiration (0 = not locked out)
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

// accountLoginKey is the per-account login budget key. F6134: the budget is
// per (client IP, account), not per account alone, otherwise anyone able to
// send five wrong passwords for a victim's address, from any IP, locks the
// victim out of the account for the window. Guessing from one IP stays capped
// at 5 per window per account, and the per-IP limiter still bounds an
// attacker's total guesses across accounts.
func accountLoginKey(ip, email string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "|" + ip
}

// totpKey normalises the account identity for the TOTP budget (F6135).
func totpKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// isTOTPLockedOut returns true if the account has exceeded TOTP failure limits.
func (s *Server) isTOTPLockedOut(email string) bool {
	email = totpKey(email)
	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	if s.totpAttempts == nil {
		s.totpAttempts = make(map[string]*totpAttempt)
	}

	attempt := s.totpAttempts[email]
	if attempt != nil && attempt.count >= maxTOTPFailures && time.Since(attempt.lastSeen) < totpLockoutDuration {
		return true
	}
	return false
}

// recordTOTPFailure increments the failed TOTP attempt count for an account.
func (s *Server) recordTOTPFailure(email string) {
	email = totpKey(email)
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
	email = totpKey(email)
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

// newJTI returns a random 128-bit token ID for the JWT "jti" claim (F4939,
// F5025), so every issued token is unique even within one second.
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// beforeCredentialWrite is a test seam invoked between the slow password
// hash and the credential write (F6255/F6256) so tests can interleave a
// concurrent change deterministically. Nil in production.
var beforeCredentialWrite func()

// ceilMicros returns t as unix microseconds rounded up. JWT claims are
// decoded as float64, which holds microsecond timestamps exactly but not
// nanosecond ones, so the session cut-off works in microseconds; rounding up
// keeps a token issued right after a cut-off at or after it.
func ceilMicros(t time.Time) int64 { return (t.UnixNano() + 999) / 1000 }

// sessionState applies the account's current state to a validated token
// (F5029, F6250-F6252, F6258). JWT claims are a snapshot from issue time, so
// without this a deactivated account kept working, a demoted admin kept admin
// access and a deleted account (or one whose password was just changed) kept
// every session until the token expired. The stored account is authoritative:
//   - missing/unreadable account, or active=false: rejected (fail closed);
//   - TokensValidAfter (set on password change/reset, disable, demotion,
//     TOTP disable) and the account's creation time reject tokens issued
//     before them, so a deleted-and-recreated address does not inherit
//     old sessions;
//   - isAdmin is the stored role.
//
// With no database or no subject the claim value is kept (nothing to
// consult).
func (s *Server) sessionState(claims jwt.MapClaims) (isAdmin, active bool) {
	sub, _ := claims["sub"].(string)
	claimAdmin, _ := claims["admin"].(bool)
	if s.db == nil || sub == "" {
		return claimAdmin, true
	}
	localPart, domain := parseEmail(sub)
	account, err := s.db.GetAccount(domain, localPart)
	if err != nil || account == nil {
		return false, false
	}
	if !account.IsActive {
		return account.IsAdmin, false
	}
	if !tokenIssuedAfterCutoff(claims, account) {
		return account.IsAdmin, false
	}
	return account.IsAdmin, true
}

// tokenIssuedAfterCutoff reports whether the token was issued at or after the
// account's revocation cut-off and creation time.
func tokenIssuedAfterCutoff(claims jwt.MapClaims, account *db.AccountData) bool {
	cutoff := account.TokensValidAfter
	var created int64
	if !account.CreatedAt.IsZero() {
		created = account.CreatedAt.UnixNano()
	}
	if cutoff == 0 && created == 0 {
		return true
	}
	if iatu, ok := claims["iatu"].(float64); ok {
		n := int64(iatu) * 1000
		return n >= cutoff && n >= created
	}
	iat, ok := claims["iat"].(float64)
	if !ok {
		return cutoff == 0
	}
	// Legacy token (second precision): it was issued somewhere in
	// [iat, iat+1). Reject whenever that interval can precede the cut-off;
	// the creation time is only compared whole-second so a token minted in
	// the creation second is accepted.
	start := int64(iat) * int64(time.Second)
	if cutoff != 0 && start <= cutoff {
		return false
	}
	return created == 0 || start+int64(time.Second) > created
}

// revokeSessions sets the account's session cut-off to now: every token
// issued before this instant stops working (F6250).
func revokeSessions(a *db.AccountData) {
	a.TokensValidAfter = ceilMicros(time.Now()) * 1000
}

// isWebBrowserUA reports whether the User-Agent looks like a browser; those
// clients get their token only via the HttpOnly cookie.
func isWebBrowserUA(r *http.Request) bool {
	ua := r.Header.Get("User-Agent")
	return strings.Contains(ua, "Mozilla/") || strings.Contains(ua, "Chrome/") ||
		strings.Contains(ua, "Firefox/") || strings.Contains(ua, "Safari/") ||
		strings.Contains(ua, "Edge/")
}

// attachReplacementSession is used after the caller revoked all sessions of
// their own account (password change, 2FA removal): it issues a fresh token so
// the acting session survives. A cookie session gets the replacement cookie;
// non-browser clients also receive it in resp["token"].
func (s *Server) attachReplacementSession(w http.ResponseWriter, r *http.Request, account *db.AccountData, resp map[string]interface{}) {
	tokenString, err := s.newSessionToken(account.Email, account.IsAdmin)
	if err != nil {
		// The old tokens are already dead; the client must log in again.
		return
	}
	if cookie, cerr := r.Cookie("jwt"); cerr == nil && cookie.Value != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "jwt",
			Value:    tokenString,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int(s.config.TokenExpiry.Seconds()),
		})
	}
	if !isWebBrowserUA(r) || r.Header.Get("Authorization") != "" {
		resp["token"] = tokenString
		resp["expiresIn"] = int(s.config.TokenExpiry.Seconds())
	}
}

// newSessionToken signs a session JWT for sub. The iatu claim carries
// microsecond issue time for the revocation cut-off (see sessionState).
func (s *Server) newSessionToken(sub string, admin bool) (string, error) {
	// F5025/F4939: a random jti keeps two tokens issued in the same second
	// from being byte-identical.
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   sub,
		"admin": admin,
		"exp":   now.Add(s.config.TokenExpiry).Unix(),
		"iat":   now.Unix(),
		"iatu":  ceilMicros(now),
		"jti":   jti,
	})
	// Set key ID header for secret rotation support
	kid, secret := s.signingKey()
	token.Header["kid"] = kid
	return token.SignedString(secret)
}

// getTOTPKey returns the encryption key for TOTP secrets.
// Returns TOTPKey if set, otherwise falls back to JWTSecret.
func (s *Server) getTOTPKey() string {
	if s.config.TOTPKey != "" {
		return s.config.TOTPKey
	}
	return s.config.JWTSecret
}

// RevokeToken adds a token to the blacklist (for logout).
// The expiry parameter should match the token's actual expiration so the
// blacklist entry lives exactly as long as the token remains valid.
// If a database is configured, the revocation is persisted; otherwise it falls back to memory.
func (s *Server) RevokeToken(tokenHash string, expiry time.Time) {
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

// tokenExpiryFromString extracts the exp claim from a JWT without verifying the signature.
// It decodes the payload segment directly via base64 to avoid the security risk of ParseUnverified.
func (s *Server) tokenExpiryFromString(tokenStr string) time.Time {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return time.Now().Add(s.config.TokenExpiry)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(s.config.TokenExpiry)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Now().Add(s.config.TokenExpiry)
	}
	expClaim, ok := claims["exp"].(float64)
	if !ok {
		return time.Now().Add(s.config.TokenExpiry)
	}
	return time.Unix(int64(expClaim), 0)
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
// It only reads the failure count kept by recordLoginFailure (F5027: counting
// every attempt here as well made one wrong password count twice and made
// successful logins from a shared IP count toward the lockout). Once
// maxAttempts failures are recorded the IP is locked out for 5 minutes,
// doubled per failure recorded beyond the limit (e.g. by concurrent
// requests), capped at 60 minutes.
func (s *Server) checkLoginRateLimit(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists {
		return true
	}

	// Reset if previous lockout expired (sliding window from last failure)
	if now.Sub(attempt.lastSeen) > 5*time.Minute {
		attempt.count = 0
		attempt.lastSeen = now
		attempt.lockoutUntil = time.Time{}
		return true
	}

	// Check if currently locked out
	if !attempt.lockoutUntil.IsZero() && now.Before(attempt.lockoutUntil) {
		return false
	}

	// Clear lockout if expired
	if !attempt.lockoutUntil.IsZero() && now.After(attempt.lockoutUntil) {
		attempt.lockoutUntil = time.Time{}
		attempt.count = 0
		return true
	}

	maxAttempts := 5 // Default; configurable via security.max_login_attempts
	if s.config.MaxLoginAttempts > 0 {
		maxAttempts = s.config.MaxLoginAttempts
	}
	if attempt.count >= maxAttempts {
		// Exponential backoff: 5min * 2^(failures-maxAttempts), capped at 60min.
		// F5026: the shift is relative to maxAttempts and clamped, so a
		// limit below 5 cannot produce a negative shift (runtime panic) and
		// a large count cannot overflow it.
		shift := attempt.count - maxAttempts
		if shift > 4 {
			shift = 4
		}
		backoffMinutes := 5 * (1 << shift)
		if backoffMinutes > 60 {
			backoffMinutes = 60 // cap at 60 minutes
		}
		attempt.lockoutUntil = now.Add(time.Duration(backoffMinutes) * time.Minute)
		return false
	}
	return true
}

// recordLoginFailure increments the failed login counter for an IP.
// Lockout timing is calculated in checkLoginRateLimit.
func (s *Server) recordLoginFailure(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if s.loginAttempts == nil {
		s.loginAttempts = make(map[string]*loginAttempt)
	}

	now := time.Now()
	attempt, exists := s.loginAttempts[ip]
	if !exists {
		if len(s.loginAttempts) >= maxAuthAttemptEntries {
			for k, a := range s.loginAttempts {
				if now.Sub(a.lastSeen) > 5*time.Minute {
					delete(s.loginAttempts, k)
				}
			}
			if len(s.loginAttempts) >= maxAuthAttemptEntries {
				return
			}
		}
		s.loginAttempts[ip] = &loginAttempt{count: 1, lastSeen: now}
		return
	}

	// Reset if window expired
	if now.Sub(attempt.lastSeen) > 5*time.Minute {
		attempt.count = 1
		attempt.lastSeen = now
		attempt.lockoutUntil = time.Time{}
		return
	}

	// Clear any existing lockout when recording new failure
	attempt.lockoutUntil = time.Time{}
	attempt.count++
	attempt.lastSeen = now
}

// checkAccountLoginRateLimit returns true if the account is allowed to attempt login.
// Allows 5 failed attempts per 5-minute window per account; blocks after that.
// F5135: the check only reads the failure count recorded by
// recordAccountLoginFailure. It used to increment on every attempt as well,
// so each failure counted twice and 3 wrong passwords locked the account.
func (s *Server) checkAccountLoginRateLimit(email string) bool {
	s.accountLoginMu.Lock()
	defer s.accountLoginMu.Unlock()

	attempt, exists := s.accountLoginAttempts[email]
	if !exists {
		return true
	}
	if time.Since(attempt.lastSeen) > 5*time.Minute {
		delete(s.accountLoginAttempts, email)
		return true
	}
	return attempt.count < 5
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
		if len(s.accountLoginAttempts) >= maxAuthAttemptEntries {
			pruneLoginMap(s.accountLoginAttempts, now)
			if len(s.accountLoginAttempts) >= maxAuthAttemptEntries {
				return // still full of live entries: do not grow further
			}
		}
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

	// Rate limit login attempts by IP (respects X-Forwarded-For from trusted proxies)
	ip := getClientIP(r, s.config.TrustedProxies)
	if !s.checkLoginRateLimit(ip) {
		s.sendError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Normalize email for rate limiting
	emailKey := accountLoginKey(ip, req.Email)

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
		// Timing-safe: perform a dummy password check to avoid revealing
		// whether the account exists through response timing.
		_, _ = s.verifyPassword(req.Password, dummyPasswordHash)
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

	// F4845: a deactivated account must not obtain a session (SMTP/IMAP auth
	// already refuses it). Same generic response as a bad password so the
	// account state is not disclosed.
	if !account.IsActive {
		s.recordLoginFailure(ip)
		s.recordAccountLoginFailure(emailKey)
		s.auditLogger.LogLoginFailure(req.Email, ip, "account_inactive")
		s.sendError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Rehash password if using older algorithm and argon2id is preferred
	if needsRehash {
		newHash, err := s.hashPassword(req.Password)
		if err == nil {
			// F6255: compare-and-swap on the hash that was verified. The
			// hash above is slow; writing back the whole snapshot let a
			// login racing a password change put the OLD password's rehash
			// over the new password (and revert concurrent disables).
			oldHash := account.PasswordHash
			if beforeCredentialWrite != nil {
				beforeCredentialWrite()
			}
			errStale := errors.New("password changed concurrently")
			_, _ = s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
				if a.PasswordHash != oldHash {
					return errStale
				}
				a.PasswordHash = newHash
				return nil
			})
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
		totpSecret, err := auth.DecryptTOTPSecret(account.TOTPSecret, s.getTOTPKey())
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
		// Replay protection: reject reuse of the same or older time step.
		// RFC 6238 §5.2: an OTP must be accepted only once.
		if step <= account.TOTPLastUsedStep {
			s.recordTOTPFailure(req.Email)
			s.recordAccountLoginFailure(emailKey)
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_replay")
			s.sendError(w, http.StatusUnauthorized, "TOTP code already used")
			return
		}
		// Atomically consume the step (RFC 6238 §5.2): the replay check
		// above runs against the account snapshot fetched before the slow
		// password hash, so two concurrent logins with one code can both
		// pass it. The compare-and-swap inside one bbolt transaction lets
		// exactly one login consume; the loser is a replay.
		consumed, err := s.db.ConsumeTOTPStep(domain, user, account.TOTPLastUsedStep, step)
		if err != nil {
			s.logger.Error("failed to update TOTP last used step", "error", err, "email", req.Email)
		} else if !consumed {
			s.recordTOTPFailure(req.Email)
			s.recordAccountLoginFailure(emailKey)
			s.auditLogger.LogLoginFailure(req.Email, ip, "totp_replay")
			s.sendError(w, http.StatusUnauthorized, "TOTP code already used")
			return
		}
		account.TOTPLastUsedStep = step
		s.clearTOTPFailures(req.Email)
	}

	tokenString, err := s.newSessionToken(account.Email, account.IsAdmin)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Set JWT as HttpOnly cookie for web clients
	isSecure := r.TLS != nil
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Value:    tokenString,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(s.config.TokenExpiry.Seconds()),
	})

	// Check User-Agent to distinguish web browsers from API clients
	// Web browsers receive token only via HttpOnly cookie (XSS protection)
	// API clients (mobile, desktop) receive token in JSON response
	userAgent := r.Header.Get("User-Agent")
	isWebBrowser := strings.Contains(userAgent, "Mozilla/") || strings.Contains(userAgent, "Chrome/") ||
		strings.Contains(userAgent, "Firefox/") || strings.Contains(userAgent, "Safari/") ||
		strings.Contains(userAgent, "Edge/")

	if !isWebBrowser {
		// Return token in JSON only for non-browser API clients
		s.sendJSON(w, http.StatusOK, map[string]interface{}{
			"token":     tokenString,
			"expiresIn": int(s.config.TokenExpiry.Seconds()),
		})
	} else {
		// For web browsers, don't expose token in JSON - rely on HttpOnly cookie
		s.sendJSON(w, http.StatusOK, map[string]interface{}{
			"expiresIn": int(s.config.TokenExpiry.Seconds()),
		})
	}

	// Clear login failures on successful login
	s.clearAccountLoginFailures(emailKey)

	// Initialize demo emails for the user on first login
	InitDemoEmails(account.Email)

	// Audit successful login
	s.auditLogger.LogLoginSuccess(account.Email, ip)
}

// handleLogout revokes the current token (adds it to blacklist) and clears the cookie
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get token from cookie first, then Authorization header
	var tokenStr string
	if cookie, err := r.Cookie("jwt"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	} else {
		// Fall back to Authorization header
		authHeader := r.Header.Get("Authorization")
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			tokenStr = parts[1]
		}
	}

	// Revoke the token by adding it to blacklist (if we have a token)
	if tokenStr != "" {
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenStr)))
		expiry := s.tokenExpiryFromString(tokenStr)
		s.RevokeToken(tokenHash, expiry)
	}

	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

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

	// Revoke the old token(s) by adding them to the blacklist. F4937:
	// authMiddleware authenticates the "jwt" cookie in preference to the
	// Bearer header, so the cookie token must be rotated out too.
	var oldTokens []string
	fromCookie := false
	if cookie, err := r.Cookie("jwt"); err == nil && cookie.Value != "" {
		oldTokens = append(oldTokens, cookie.Value)
		fromCookie = true
	}
	authHeader := r.Header.Get("Authorization")
	if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		if len(oldTokens) == 0 || oldTokens[0] != parts[1] {
			oldTokens = append(oldTokens, parts[1])
		}
	}
	for _, oldTokenStr := range oldTokens {
		oldTokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(oldTokenStr)))

		// Defense-in-depth: verify the old token has not already been revoked
		if s.IsTokenRevoked(oldTokenHash) {
			s.sendError(w, http.StatusUnauthorized, "token has been revoked")
			return
		}
	}
	for _, oldTokenStr := range oldTokens {
		oldTokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(oldTokenStr)))
		expiry := s.tokenExpiryFromString(oldTokenStr)
		s.RevokeToken(oldTokenHash, expiry)
	}

	// The auth middleware already validated the token
	user := r.Context().Value("user")
	isAdmin := r.Context().Value("isAdmin")

	// F4939: newSessionToken's random jti keeps it distinct from the token
	// revoked above even when both are issued in the same second.
	adminClaim, _ := isAdmin.(bool)
	subject, _ := user.(string)
	tokenString, err := s.newSessionToken(subject, adminClaim)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// F4937: the old cookie token is now revoked, so a cookie (browser)
	// session receives the new token as its replacement HttpOnly cookie.
	if fromCookie {
		http.SetCookie(w, &http.Cookie{
			Name:     "jwt",
			Value:    tokenString,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int(s.config.TokenExpiry.Seconds()),
		})
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"token":     tokenString,
		"expiresIn": int(s.config.TokenExpiry.Seconds()),
	})
}

// maxAuthAttemptEntries bounds each failed-attempt map. The account map is
// keyed by an attacker-chosen email string, so without a bound unauthenticated
// requests grew it forever (F6065).
const maxAuthAttemptEntries = 10000

// pruneAuthAttempts drops stale entries from the login/TOTP/API rate maps.
func (s *Server) pruneAuthAttempts() {
	now := time.Now()
	s.loginMu.Lock()
	for k, a := range s.loginAttempts {
		if now.Sub(a.lastSeen) > 5*time.Minute && now.After(a.lockoutUntil) {
			delete(s.loginAttempts, k)
		}
	}
	s.loginMu.Unlock()
	s.accountLoginMu.Lock()
	pruneLoginMap(s.accountLoginAttempts, now)
	s.accountLoginMu.Unlock()
	s.totpMu.Lock()
	for k, a := range s.totpAttempts {
		if now.Sub(a.lastSeen) > totpLockoutDuration {
			delete(s.totpAttempts, k)
		}
	}
	s.totpMu.Unlock()
	s.apiRateMu.Lock()
	for k, a := range s.apiRateAttempts {
		if now.Sub(a.windowStart) > time.Minute {
			delete(s.apiRateAttempts, k)
		}
	}
	s.apiRateMu.Unlock()
}

func pruneLoginMap(m map[string]*loginAttempt, now time.Time) {
	for k, a := range m {
		if now.Sub(a.lastSeen) > 5*time.Minute {
			delete(m, k)
		}
	}
}

// TokenIssuedAfterCutoff reports whether the token claims were issued at or
// after the account's session cut-off and creation time. It is exported so
// other listeners (JMAP) apply the same session revocation as the API.
func TokenIssuedAfterCutoff(claims map[string]interface{}, account *db.AccountData) bool {
	return tokenIssuedAfterCutoff(jwt.MapClaims(claims), account)
}
