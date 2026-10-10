package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
	// F6138: create/reset cap passwords at 128 characters; self-service had
	// no ceiling with argon2id, so it could store what create rejects.
	if len(req.NewPassword) > maxPasswordLength {
		s.sendError(w, http.StatusBadRequest, fmt.Sprintf("new password exceeds maximum length of %d characters", maxPasswordLength))
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
	emailKey := accountLoginKey(getClientIP(r, s.config.TrustedProxies), authUser) // F6134
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
	// F6256: change only the credential fields of the stored row; writing
	// back the snapshot read before the slow hashes re-enabled an account an
	// admin disabled meanwhile and reverted concurrent role/TOTP changes.
	// F6250: every existing session of the account is revoked; the caller
	// receives a replacement token below.
	if beforeCredentialWrite != nil {
		beforeCredentialWrite()
	}
	updated, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		a.PasswordHash = hashedPassword
		a.APOPHash = fmt.Sprintf("%x", sha256.Sum256([]byte(req.NewPassword)))
		revokeSessions(a)
		return nil
	})
	if err != nil {
		status, msg := dbErrStatus(err, "failed to update password")
		s.sendError(w, status, msg)
		return
	}

	s.auditLogger.LogAccountUpdate(authUser, authUser, audit.ExtractIP(r), []string{"password_changed"})
	resp := map[string]interface{}{"message": "password changed"}
	s.attachReplacementSession(w, r, updated, resp)
	s.sendJSON(w, http.StatusOK, resp)
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
		setTotalCount(w, 1)
		s.sendJSON(w, http.StatusOK, []map[string]interface{}{accountToJSON(account)})
		return
	}

	limit, offset, ok := s.parsePage(w, r)
	if !ok {
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

	// F6254: stable order (by address), then the requested window.
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Email < accounts[j].Email })
	setTotalCount(w, len(accounts))
	lo, hi := pageBounds(len(accounts), limit, offset)
	accounts = accounts[lo:hi]

	result := make([]map[string]interface{}, 0, len(accounts)) // F6137: [] not null
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
	// F6136: domains are stored lower-case and SMTP/IMAP lower-case the
	// login name, so a mixed-case address either failed the domain lookup
	// or created a mailbox nobody could authenticate to.
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

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

	// F5510/F5511: the checks above give early answers, but only
	// CreateAccountInDomain re-checks the domain and its limit in the same
	// transaction as the insert, so parallel creates and a racing domain
	// delete cannot slip past them.
	if err := s.db.CreateAccountInDomain(account); err != nil {
		if errors.Is(err, db.ErrDomainNotFound) { // create: unknown domain is a bad request
			s.sendError(w, http.StatusBadRequest, "domain not found")
			return
		}
		status, msg := dbErrStatus(err, "failed to create account")
		s.sendError(w, status, msg)
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

	// Authorization check: prevent privilege escalation
	authUser, ok := r.Context().Value("user").(string)
	isAdmin, _ := r.Context().Value("isAdmin").(bool)

	// F5871: authorize before the lookup so a non-admin cannot use 404 vs 403
	// to probe which accounts exist.
	if ok && authUser != "" && !isAdmin && authUser != user+"@"+domain {
		s.sendError(w, http.StatusForbidden, "access denied")
		return
	}

	account, err := s.db.GetAccount(domain, user)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}
	if !ok || authUser == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
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

	// F5870: quota is an administrative limit; a non-admin must not raise
	// (or clear) their own.
	if !isAdmin && req.QuotaLimit != nil {
		s.sendError(w, http.StatusForbidden, "only admins can change quota")
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

	var newHash, newAPOP string
	if req.Password != "" {
		// F6139: PUT must not be a way around the current-password check
		// and throttle of /api/v1/account/password for one's own account.
		if authUser == user+"@"+domain {
			s.sendError(w, http.StatusForbidden, "use /api/v1/account/password to change your own password")
			return
		}
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
		newHash = hashedPassword
		newAPOP = fmt.Sprintf("%x", sha256.Sum256([]byte(req.Password)))
	}

	// F6256: apply only the requested fields to the stored row inside one
	// transaction. The snapshot above was read before the slow password
	// hash; writing it back (UpdateAccount) erased a concurrent disable,
	// role change or password change. F6250/F6252: a password reset, a
	// disable or an admin demotion also revokes the account's sessions, so
	// re-enabling or re-promoting cannot revive old tokens.
	updated, err := s.db.MutateAccount(domain, user, func(a *db.AccountData) error {
		revoke := false
		if newHash != "" {
			a.PasswordHash = newHash
			a.APOPHash = newAPOP
			revoke = true
		}
		if req.IsAdmin != nil {
			if a.IsAdmin && !*req.IsAdmin {
				revoke = true
			}
			a.IsAdmin = *req.IsAdmin
		}
		if req.IsActive != nil {
			if a.IsActive && !*req.IsActive {
				revoke = true
			}
			a.IsActive = *req.IsActive
		}
		if req.ForwardTo != nil {
			a.ForwardTo = *req.ForwardTo
		}
		if req.ForwardKeepCopy != nil {
			a.ForwardKeepCopy = *req.ForwardKeepCopy
		}
		if req.QuotaLimit != nil {
			a.QuotaLimit = *req.QuotaLimit
		}
		if req.VacationSettings != nil {
			a.VacationSettings = *req.VacationSettings
		}
		if revoke {
			revokeSessions(a)
		}
		return nil
	})
	if err != nil {
		status, msg := dbErrStatus(err, "failed to update account")
		s.sendError(w, status, msg)
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(updated))
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

	// F6130: deleting a missing account is a 404, not a 204 with an audit
	// entry for a deletion that never happened.
	if _, err := s.db.GetAccount(domain, user); err != nil {
		if isKeyNotFound(err) {
			s.sendError(w, http.StatusNotFound, "account not found")
		} else {
			s.sendError(w, http.StatusInternalServerError, "failed to delete account")
		}
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
