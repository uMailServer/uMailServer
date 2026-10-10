package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/audit"
	"github.com/umailserver/umailserver/internal/db"
)

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAccounts(w, r)
	case http.MethodPost:
		s.createAccount(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAccounts lists and creates accounts
//
//	@Summary List accounts
//	@Description Returns a list of all accounts for a domain
//	@Tags Accounts
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {array} map[string]interface{} "List of accounts"
//	@Router /api/v1/accounts [get]
//	@Summary Create account
//	@Description Creates a new email account
//	@Tags Accounts
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Success 201 {object} map[string]interface{} "Account created"
//	@Router /api/v1/accounts [post]
func (s *Server) handleAccountDetail(w http.ResponseWriter, r *http.Request) {
	suffix := r.URL.Path[len("/api/v1/accounts/"):]

	// Handle TOTP 2FA sub-paths
	if len(suffix) > 11 && suffix[len(suffix)-11:] == "/totp/setup" {
		email := suffix[:len(suffix)-11]
		s.handleTOTPSetup(w, r, email)
		return
	}
	if len(suffix) > 13 && suffix[len(suffix)-13:] == "/totp/disable" {
		email := suffix[:len(suffix)-13]
		s.handleTOTPDisable(w, r, email)
		return
	}
	if len(suffix) > 12 && suffix[len(suffix)-12:] == "/totp/verify" {
		email := suffix[:len(suffix)-12]
		s.handleTOTPVerify(w, r, email)
		return
	}

	// Regular account detail
	switch r.Method {
	case http.MethodGet:
		s.getAccount(w, r, suffix)
	case http.MethodPut:
		s.updateAccount(w, r, suffix)
	case http.MethodDelete:
		s.deleteAccount(w, r, suffix)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAccountPassword allows the authenticated account to change its own
// password (self-service). The current password must be provided for
// re-authentication.
//
//	@Summary Change own password
//	@Description Changes the password of the cookie/bearer-authenticated account
//	@Tags Account
//	@Produce json
//	@Accept json
//	@Param body body object true "Current and new password"
//	@Success 200 {object} map[string]string
//	@Failure 400 {object} map[string]string
//	@Failure 401 {object} map[string]string
//	@Failure 403 {object} map[string]string
//	@Router /api/v1/account/password [post]
func (s *Server) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	authUser, _ := r.Context().Value("user").(string)
	if authUser == "" {
		s.sendError(w, http.StatusUnauthorized, "missing authentication")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		s.sendError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	if len(req.NewPassword) < 8 {
		s.sendError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	if err := s.checkHasherPasswordLength(req.NewPassword); err != nil { // F5282
		s.sendError(w, http.StatusBadRequest, "new "+err.Error())
		return
	}

	user, domain := parseEmail(authUser)
	account, err := s.db.GetAccount(domain, user)
	if err != nil || account == nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// F5030: this route is outside the API rate limiter, so without a budget
	// it is an unlimited oracle for the current password. Guesses share the
	// per-account login budget (5 per 5 minutes), cleared on success.
	emailKey := strings.ToLower(authUser)
	if !s.checkAccountLoginRateLimit(emailKey) {
		s.sendError(w, http.StatusTooManyRequests, "too many password attempts for this account")
		return
	}

	// Re-authentication: the current password must match.
	if matches, _ := s.verifyPassword(req.CurrentPassword, account.PasswordHash); !matches {
		s.recordAccountLoginFailure(emailKey) // F5135: the check no longer counts attempts
		s.auditLogger.LogLoginFailure(authUser, audit.ExtractIP(r), "password_change_wrong_current")
		s.sendError(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	s.clearAccountLoginFailures(emailKey)

	hashedPassword, err := s.hashPassword(req.NewPassword)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	account.PasswordHash = hashedPassword
	account.APOPHash = fmt.Sprintf("%x", sha256.Sum256([]byte(req.NewPassword)))
	account.UpdatedAt = time.Now()

	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update password")
		return
	}

	s.auditLogger.LogAccountUpdate(authUser, authUser, audit.ExtractIP(r), []string{"password_changed"})
	s.sendJSON(w, http.StatusOK, map[string]string{"message": "password changed"})
}

// Account handlers

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	authUser, _ := r.Context().Value("user").(string)
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	// Non-admins may only view their own account
	if !isAdmin && authUser != "" {
		user, domain := parseEmail(authUser)
		account, err := s.db.GetAccount(domain, user)
		if err != nil || account == nil {
			s.sendError(w, http.StatusNotFound, "account not found")
			return
		}
		s.sendJSON(w, http.StatusOK, []map[string]interface{}{accountToJSON(account)})
		return
	}

	domain := r.URL.Query().Get("domain")

	var accounts []*db.AccountData
	var err error

	if domain != "" {
		accounts, err = s.db.ListAccountsByDomain(domain)
	} else {
		// Get all accounts from all domains
		domains, listErr := s.db.ListDomains()
		if listErr != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to list accounts")
			return
		}
		for _, d := range domains {
			domainAccounts, accErr := s.db.ListAccountsByDomain(d.Name)
			if accErr != nil {
				s.sendError(w, http.StatusInternalServerError, "failed to list accounts for domain")
				return
			}
			accounts = append(accounts, domainAccounts...)
		}
	}

	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}

	var result []map[string]interface{}
	for _, a := range accounts {
		result = append(result, accountToJSON(a))
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		s.sendError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	// Non-admin cannot create admin accounts
	if !isAdmin && req.IsAdmin {
		s.sendError(w, http.StatusForbidden, "only admins can create admin accounts")
		return
	}

	// Validate email format
	if err := validateEmailFormat(req.Email); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid email format")
		return
	}

	// Validate password strength
	if err := s.validateNewPassword(req.Password); err != nil { // F5282
		s.sendError(w, http.StatusBadRequest, "password does not meet complexity requirements")
		return
	}

	user, domain := parseEmail(req.Email)

	// F5372: the domain must be hosted here; an account on an unknown domain
	// can log in but never appears in the admin account list. ListDomains
	// separates "not hosted" (400) from a storage failure (500).
	domains, err := s.db.ListDomains()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to check domain")
		return
	}
	var domainData *db.DomainData
	for _, d := range domains {
		if d.Name == domain {
			domainData = d
			break
		}
	}
	if domainData == nil {
		s.sendError(w, http.StatusBadRequest, "domain not found")
		return
	}
	// F5373: enforce the domain's account limit (0 = unlimited).
	if domainData.MaxAccounts > 0 {
		existing, err := s.db.ListAccountsByDomain(domain)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to check domain accounts")
			return
		}
		if len(existing) >= domainData.MaxAccounts {
			s.sendError(w, http.StatusConflict, "domain account limit reached")
			return
		}
	}

	// Hash password with configured hasher
	hashedPassword, err := s.hashPassword(req.Password)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	account := &db.AccountData{
		Email:        req.Email,
		LocalPart:    user,
		Domain:       domain,
		PasswordHash: hashedPassword,
		APOPHash:     fmt.Sprintf("%x", sha256.Sum256([]byte(req.Password))),
		IsAdmin:      req.IsAdmin && isAdmin,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.CreateAccount(account); err != nil {
		if errors.Is(err, db.ErrAccountExists) {
			s.sendError(w, http.StatusConflict, "account already exists")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	// Audit account creation
	actor := "system"
	if authUser := r.Context().Value("user"); authUser != nil {
		if userStr, ok := authUser.(string); ok {
			actor = userStr
		}
	}
	s.auditLogger.LogAccountCreate(actor, req.Email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusCreated, accountToJSON(account))
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	// Authorization check: ensure user owns this account or is admin
	authUser, _ := r.Context().Value("user").(string)
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	// If auth context exists, enforce ownership
	if authUser != "" && !isAdmin && authUser != user+"@"+domain {
		s.sendError(w, http.StatusForbidden, "access denied")
		return
	}

	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(account))
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	// Authorization check: prevent privilege escalation
	authUser, ok := r.Context().Value("user").(string)
	if !ok || authUser == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	if !isAdmin && authUser != user+"@"+domain {
		s.sendError(w, http.StatusForbidden, "access denied")
		return
	}

	// Parse request body first to check IsAdmin modification. F5280: optional
	// fields are pointers so an omitted field keeps its stored value; the
	// admin panel's edit dialog sends only is_admin/is_active/password, and
	// zero-filling the rest wiped forwarding, the vacation reply and the
	// quota (0 = unlimited) on every edit.
	var req struct {
		Password             string  `json:"password"`
		IsAdmin              *bool   `json:"is_admin"`
		IsActive             *bool   `json:"is_active"`
		ForwardTo            *string `json:"forward_to"`
		ForwardKeepCopy      *bool   `json:"forward_keep_copy"`
		QuotaLimit           *int64  `json:"quota_limit"`
		VacationSettings     *string `json:"vacation_settings"`
		CurrentAdminPassword string  `json:"current_admin_password"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.QuotaLimit != nil && *req.QuotaLimit < 0 {
		s.sendError(w, http.StatusBadRequest, "quota_limit must be non-negative")
		return
	}

	// F5280: an omitted is_admin keeps the stored role.
	wantAdmin := account.IsAdmin
	if req.IsAdmin != nil {
		wantAdmin = *req.IsAdmin
	}

	// Non-admin cannot grant admin privileges
	if !isAdmin && wantAdmin {
		s.sendError(w, http.StatusForbidden, "only admins can grant admin privileges")
		return
	}

	// Admins can only promote other users (not themselves) to admin
	if isAdmin && wantAdmin && authUser == email && account.IsAdmin != wantAdmin {
		s.sendError(w, http.StatusForbidden, "cannot modify your own admin status")
		return
	}

	// Admin status changes require re-authentication (current admin password)
	if isAdmin && account.IsAdmin != wantAdmin {
		if req.CurrentAdminPassword == "" {
			s.sendError(w, http.StatusForbidden, "current_admin_password required for admin privilege changes")
			return
		}
		// Verify the acting admin's password
		adminUser, adminDomain := parseEmail(authUser)
		adminAccount, err := s.db.GetAccount(adminDomain, adminUser)
		if err != nil {
			s.sendError(w, http.StatusForbidden, "unable to verify admin credentials")
			return
		}
		matches, _ := s.verifyPassword(req.CurrentAdminPassword, adminAccount.PasswordHash)
		if !matches {
			s.sendError(w, http.StatusForbidden, "invalid current_admin_password")
			return
		}
		// Audit log the privilege change
		ip := audit.ExtractIP(r)
		action := "demoted"
		if wantAdmin {
			action = "promoted"
		}
		s.auditLogger.LogAccountUpdate(authUser, email, ip, []string{"admin_status_" + action})
	}

	if req.Password != "" {
		if err := s.validateNewPassword(req.Password); err != nil { // F5281
			s.sendError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Hash new password with configured hasher
		hashedPassword, err := s.hashPassword(req.Password)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		account.PasswordHash = hashedPassword
		account.APOPHash = fmt.Sprintf("%x", sha256.Sum256([]byte(req.Password)))
	}
	account.IsAdmin = wantAdmin
	if req.IsActive != nil {
		account.IsActive = *req.IsActive
	}
	if req.ForwardTo != nil {
		account.ForwardTo = *req.ForwardTo
	}
	if req.ForwardKeepCopy != nil {
		account.ForwardKeepCopy = *req.ForwardKeepCopy
	}
	if req.QuotaLimit != nil {
		account.QuotaLimit = *req.QuotaLimit
	}
	if req.VacationSettings != nil {
		account.VacationSettings = *req.VacationSettings
	}
	account.UpdatedAt = time.Now()

	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update account")
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(account))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	// Authorization check: ensure user owns this account or is admin
	authUser, _ := r.Context().Value("user").(string)
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	// If auth context exists, enforce ownership/non-admin restrictions
	if authUser != "" && !isAdmin && authUser != user+"@"+domain {
		s.sendError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := s.db.DeleteAccount(domain, user); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete account")
		return
	}

	// Audit account deletion
	actor := "system"
	if authUser != "" {
		actor = authUser
	}
	s.auditLogger.LogAccountDelete(actor, email, audit.ExtractIP(r))

	w.WriteHeader(http.StatusNoContent)
}
