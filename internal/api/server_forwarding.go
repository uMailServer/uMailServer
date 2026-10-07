package api

import (
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/audit"
)

// Self-service mail forwarding: the authenticated account manages the
// ForwardTo/ForwardKeepCopy fields the account model already carries (the
// admin PUT /accounts/{email} path edits the same fields with admin
// privileges).

// handleForwarding dispatches GET (read) and PUT (update) for the caller's
// forwarding configuration.
func (s *Server) handleForwarding(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetForwarding(w, r)
	case http.MethodPut:
		s.handleSetForwarding(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetForwarding reports the caller's current forwarding configuration.
func (s *Server) handleGetForwarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	email, _ := r.Context().Value("user").(string)
	if email == "" {
		s.sendError(w, http.StatusUnauthorized, "missing authentication")
		return
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil || account == nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"forward_to":    account.ForwardTo,
		"keep_copy":     account.ForwardKeepCopy,
		"forwarding_on": account.ForwardTo != "",
	})
}

// handleSetForwarding updates the caller's forwarding configuration. An
// empty forward_to disables forwarding entirely.
func (s *Server) handleSetForwarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	email, _ := r.Context().Value("user").(string)
	if email == "" {
		s.sendError(w, http.StatusUnauthorized, "missing authentication")
		return
	}

	var req struct {
		ForwardTo   string `json:"forward_to"`
		KeepCopy    bool   `json:"keep_copy"`
		ForwardCopy bool   `json:"forward_copy"` // legacy alias
	}
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	forwardTo := strings.TrimSpace(req.ForwardTo)
	if forwardTo != "" {
		if _, err := mail.ParseAddress(forwardTo); err != nil {
			s.sendError(w, http.StatusBadRequest, "forward_to must be a valid email address")
			return
		}
	}

	user, domain := parseEmail(email)
	account, err := s.db.GetAccount(domain, user)
	if err != nil || account == nil {
		s.sendError(w, http.StatusNotFound, "account not found")
		return
	}

	account.ForwardTo = forwardTo
	// A forwarding target without keep_copy silently drops the original
	// message; only honor keep_copy when forwarding is actually active.
	account.ForwardKeepCopy = forwardTo != "" && (req.KeepCopy || req.ForwardCopy)
	account.UpdatedAt = time.Now()

	if err := s.db.UpdateAccount(account); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update forwarding")
		return
	}

	s.auditLogger.LogAccountUpdate(email, email, audit.ExtractIP(r), []string{"forwarding_changed"})

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"forward_to":    account.ForwardTo,
		"keep_copy":     account.ForwardKeepCopy,
		"forwarding_on": account.ForwardTo != "",
	})
}
