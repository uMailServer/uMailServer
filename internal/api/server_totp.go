package api

import (
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/auth"
)

// TOTP 2FA handlers

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authorization: user can setup their own TOTP; admin can setup for non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is setting up their own TOTP, OR admin is setting up for non-admin users
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot setup TOTP for this user")
		return
	}
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate TOTP secret")
		return
	}

	// Encrypt secret before storage
	encryptedSecret, err := auth.EncryptTOTPSecret(secret, s.config.JWTSecret)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to encrypt TOTP secret")
		return
	}

	// Store secret but don't enable yet — user must verify first
	account.TOTPSecret = encryptedSecret
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to save TOTP secret")
		return
	}

	uri := auth.GenerateTOTPUri(secret, email, "uMailServer", auth.TOTPAlgorithmSHA1)

	// Audit TOTP setup initiated
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"secret": secret,
		"uri":    uri,
	})
}

func (s *Server) handleTOTPVerify(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authorization: user can verify their own TOTP; admin can verify for non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is verifying their own TOTP, OR admin is verifying for non-admin users
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot verify TOTP for this user")
		return
	}

	if account.TOTPSecret == "" {
		s.sendError(w, http.StatusBadRequest, "TOTP not set up — call /totp/setup first")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	totpSecret, err := auth.DecryptTOTPSecret(account.TOTPSecret, s.config.JWTSecret)
	if err != nil {
		s.logger.Error("failed to decrypt TOTP secret", "error", err, "email", email)
		s.sendError(w, http.StatusInternalServerError, "authentication error")
		return
	}

	if !auth.ValidateTOTP(totpSecret, req.Code) {
		s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}

	// Code verified — enable TOTP
	account.TOTPEnabled = true
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to enable TOTP")
		return
	}

	// Audit TOTP verification complete (2FA now enabled)
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
	})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Authorization: user can disable their own TOTP; admin can disable non-admin users
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")
	isAdmin, _ := authIsAdmin.(bool)
	authenticatedUser, _ := authUser.(string)

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Allow if: user is disabling their own TOTP, OR admin is disabling a non-admin user's TOTP
	if authenticatedUser != email && (!isAdmin || account.IsAdmin) {
		s.sendError(w, http.StatusForbidden, "forbidden: cannot disable TOTP for this user")
		return
	}

	account.TOTPSecret = ""
	account.TOTPEnabled = false
	account.UpdatedAt = time.Now()
	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to disable TOTP")
		return
	}

	// Audit TOTP disable
	s.auditLogger.LogTOTPDisable(authenticatedUser, email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": false,
	})
}
