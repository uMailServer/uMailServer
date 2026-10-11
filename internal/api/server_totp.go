package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/db"
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

	// F5028: while 2FA is enabled the stored secret is the verified one that
	// login checks; overwriting it with a new, unverified secret would lock
	// the enrolled authenticator out. Re-enrollment requires disabling first.
	if account.TOTPEnabled {
		s.sendError(w, http.StatusConflict, "TOTP already enabled — disable it before setting up again")
		return
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to generate TOTP secret")
		return
	}

	// Encrypt secret before storage
	encryptedSecret, err := auth.EncryptTOTPSecret(secret, s.getTOTPKey())
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to encrypt TOTP secret")
		return
	}

	// Store secret but don't enable yet — user must verify first
	if beforeCredentialWrite != nil {
		beforeCredentialWrite()
	}
	// F6706: write only the secret, and re-check enrolment inside the
	// transaction; the snapshot above is stale by now and a whole-row
	// UpdateAccount reverted concurrent password/role/disable changes (or
	// replaced a secret a concurrent verify had just enabled).
	errEnabled := errors.New("totp already enabled")
	if _, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		if a.TOTPEnabled {
			return errEnabled
		}
		a.TOTPSecret = encryptedSecret
		return nil
	}); err != nil {
		if errors.Is(err, errEnabled) {
			s.sendError(w, http.StatusConflict, "TOTP already enabled — disable it before setting up again")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "failed to save TOTP secret")
		return
	}

	uri := auth.GenerateTOTPUri(secret, email, "uMailServer", auth.TOTPAlgorithmSHA1)

	// Audit TOTP setup initiated
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"uri": uri,
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

	// F6381: wrong codes are budgeted like login attempts, otherwise a
	// stolen session can brute-force the 6-digit code here.
	if s.isTOTPLockedOut(email) {
		s.sendError(w, http.StatusTooManyRequests, "too many failed TOTP attempts")
		return
	}

	totpSecret, err := auth.DecryptTOTPSecret(account.TOTPSecret, s.getTOTPKey())
	if err != nil {
		s.logger.Error("failed to decrypt TOTP secret", "error", err, "email", email)
		s.sendError(w, http.StatusInternalServerError, "authentication error")
		return
	}

	valid, step := auth.ValidateTOTPAtWithStep(totpSecret, req.Code, time.Now(), auth.TOTPAlgorithmSHA1)
	if !valid {
		s.recordTOTPFailure(email)
		s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
		return
	}

	// RFC 6238 §5.2: an OTP accepted once must never be accepted again.
	// The step check and the enablement happen in one transaction so two
	// concurrent verifies cannot both consume the same code, and the same
	// code cannot be replayed through login. F6382: enabling the second
	// factor revokes other sessions (the caller gets a replacement).
	selfService := authenticatedUser == email
	errReplay := errors.New("totp replay")
	updated, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		if a.TOTPSecret != account.TOTPSecret {
			return errReplay // enrolment changed underneath us
		}
		if step <= a.TOTPLastUsedStep {
			return errReplay
		}
		a.TOTPLastUsedStep = step
		a.TOTPEnabled = true
		revokeSessions(a)
		return nil
	})
	if errors.Is(err, errReplay) {
		s.recordTOTPFailure(email)
		s.sendError(w, http.StatusUnauthorized, "TOTP code already used")
		return
	}
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to enable TOTP")
		return
	}
	s.clearTOTPFailures(email)

	// Audit TOTP verification complete (2FA now enabled)
	s.auditLogger.LogTOTPEnable(authenticatedUser, email, audit.ExtractIP(r))

	resp := map[string]interface{}{"enabled": true}
	if selfService {
		s.attachReplacementSession(w, r, updated, resp)
	}
	s.sendJSON(w, http.StatusOK, resp)
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

	// F5991: a self-service disable of an ENABLED second factor must prove
	// possession of the factor; otherwise a stolen session token strips 2FA
	// (admin recovery of another user's account stays code-free).
	if authenticatedUser == email && account.TOTPEnabled {
		var req struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &req); err != nil || req.Code == "" {
			s.sendError(w, http.StatusBadRequest, "current TOTP code required to disable 2FA")
			return
		}
		if s.isTOTPLockedOut(email) {
			s.sendError(w, http.StatusTooManyRequests, "too many failed TOTP attempts")
			return
		}
		secret, err := auth.DecryptTOTPSecret(account.TOTPSecret, s.getTOTPKey())
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "authentication error")
			return
		}
		valid, step := auth.ValidateTOTPAtWithStep(secret, req.Code, time.Now(), auth.TOTPAlgorithmSHA1)
		if !valid {
			s.recordTOTPFailure(email)
			s.sendError(w, http.StatusUnauthorized, "invalid TOTP code")
			return
		}
		if step <= account.TOTPLastUsedStep {
			s.recordTOTPFailure(email)
			s.sendError(w, http.StatusUnauthorized, "TOTP code already used")
			return
		}
	}

	// F6256: only the 2FA fields are written, so concurrent changes to the
	// row are not reverted. F6250: when an admin removes another user's
	// second factor (account recovery) that user's sessions are revoked, which
	// also cuts off whoever may hold them; a self-service disable already
	// proved possession of the factor above. F6380: a self-service disable
	// of an enabled factor also revokes other sessions (a stolen session
	// must not outlive the factor change); the caller gets a replacement.
	selfService := authenticatedUser == email
	wasEnabled := account.TOTPEnabled
	updated, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		a.TOTPSecret = ""
		a.TOTPEnabled = false
		if !selfService || wasEnabled {
			revokeSessions(a)
		}
		return nil
	})
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to disable TOTP")
		return
	}

	// Audit TOTP disable
	s.auditLogger.LogTOTPDisable(authenticatedUser, email, audit.ExtractIP(r))

	resp := map[string]interface{}{"enabled": false}
	if selfService && wasEnabled {
		s.attachReplacementSession(w, r, updated, resp)
	}
	s.sendJSON(w, http.StatusOK, resp)
}

// handleSelfTOTP adapts the admin TOTP handlers for self-service use: the
// account is resolved from the auth context instead of the URL, so a portal
// user can only ever address their own TOTP (the handlers' own
// owner-or-admin check then always sees email == authenticated user).
func (s *Server) handleSelfTOTP(handler func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email, _ := r.Context().Value("user").(string)
		if email == "" {
			s.sendError(w, http.StatusUnauthorized, "missing authentication")
			return
		}
		handler(w, r, email)
	}
}

// handleTOTPStatus reports the caller's current TOTP state so the portal can
// render the right management view (enabled, setup-in-progress, or off).
func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request, email string) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil || account == nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       account.TOTPEnabled,
		"pending_setup": account.TOTPSecret != "" && !account.TOTPEnabled,
	})
}
