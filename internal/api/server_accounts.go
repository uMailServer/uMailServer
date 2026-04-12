package api

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
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

// Account handlers

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" {
		s.sendError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	// Validate email format
	if err := validateEmailFormat(req.Email); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate password strength
	if err := validatePassword(req.Password); err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, domain := parseEmail(req.Email)

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
		PasswordHash: string(hashedPassword),
		APOPHash:     fmt.Sprintf("%x", md5.Sum([]byte(req.Password))),
		IsAdmin:      req.IsAdmin,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.CreateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	// Audit account creation
	actor := "system"
	if authUser := r.Context().Value("user"); authUser != nil {
		actor = authUser.(string)
	}
	s.auditLogger.LogAccountCreate(actor, req.Email, audit.ExtractIP(r))

	s.sendJSON(w, http.StatusCreated, accountToJSON(account))
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

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
	// Only enforce when context is properly set (i.e., through HTTP middleware)
	authUser := r.Context().Value("user")
	authIsAdmin := r.Context().Value("isAdmin")

	// Parse request body first to check IsAdmin modification
	var req struct {
		Password         string `json:"password"`
		IsAdmin          bool   `json:"is_admin"`
		IsActive         bool   `json:"is_active"`
		ForwardTo        string `json:"forward_to"`
		ForwardKeepCopy  bool   `json:"forward_keep_copy"`
		QuotaLimit       int64  `json:"quota_limit"`
		VacationSettings string `json:"vacation_settings"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only enforce authorization when context is properly set (authenticated request)
	if authUser != nil && authIsAdmin != nil {
		isAdmin, _ := authIsAdmin.(bool)
		authenticatedUser, _ := authUser.(string)

		// Prevent self-modification of IsAdmin flag
		if authenticatedUser == email && req.IsAdmin != account.IsAdmin {
			s.sendError(w, http.StatusForbidden, "cannot modify your own admin status")
			return
		}

		// Prevent non-admin from setting IsAdmin to true
		if !isAdmin && req.IsAdmin {
			s.sendError(w, http.StatusForbidden, "only admins can grant admin privileges")
			return
		}
	}

	if req.Password != "" {
		// Hash new password with configured hasher
		hashedPassword, err := s.hashPassword(req.Password)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to hash password")
			return
		}
		account.PasswordHash = string(hashedPassword)
		account.APOPHash = fmt.Sprintf("%x", md5.Sum([]byte(req.Password)))
	}
	account.IsAdmin = req.IsAdmin
	account.IsActive = req.IsActive
	account.ForwardTo = req.ForwardTo
	account.ForwardKeepCopy = req.ForwardKeepCopy
	account.QuotaLimit = req.QuotaLimit
	account.VacationSettings = req.VacationSettings
	account.UpdatedAt = time.Now()

	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update account")
		return
	}

	s.sendJSON(w, http.StatusOK, accountToJSON(account))
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, email string) {
	user, domain := parseEmail(email)

	if err := s.db.DeleteAccount(domain, user); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete account")
		return
	}

	// Audit account deletion
	actor := "system"
	if authUser := r.Context().Value("user"); authUser != nil {
		actor = authUser.(string)
	}
	s.auditLogger.LogAccountDelete(actor, email, audit.ExtractIP(r))

	w.WriteHeader(http.StatusNoContent)
}
