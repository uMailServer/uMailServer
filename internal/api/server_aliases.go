package api

import (
	"net/http"
	"strings"

	"github.com/umailserver/umailserver/internal/db"
)

// handleAliases lists and creates aliases
//
//	@Summary List aliases
//	@Description Returns a list of all email aliases
//	@Tags Aliases
//	@Produce json
//	@Security BearerAuth
//	@Success 200 {array} map[string]interface{} "List of aliases"
//	@Router /api/v1/aliases [get]
//	@Summary Create alias
//	@Description Creates a new email alias
//	@Tags Aliases
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Success 201 {object} map[string]interface{} "Alias created"
//	@Router /api/v1/aliases [post]
func (s *Server) handleAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAliases(w, r)
	case http.MethodPost:
		s.createAlias(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAliasDetail(w http.ResponseWriter, r *http.Request) {
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/aliases/")

	switch r.Method {
	case http.MethodGet:
		s.getAlias(w, r, suffix)
	case http.MethodPut:
		s.updateAlias(w, r, suffix)
	case http.MethodDelete:
		s.deleteAlias(w, r, suffix)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Alias handlers

func (s *Server) listAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.db.ListAliases()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list aliases")
		return
	}

	var result []map[string]interface{}
	for _, a := range aliases {
		result = append(result, aliasToJSON(a))
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) createAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias    string `json:"alias"`  // alias@domain
		Target   string `json:"target"` // user@domain
		IsActive bool   `json:"is_active"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate alias format (must be alias@domain)
	if req.Alias == "" {
		s.sendError(w, http.StatusBadRequest, "alias address required")
		return
	}
	aliasUser, aliasDomain := parseEmail(req.Alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address format")
		return
	}

	// Validate target format (must be user@domain)
	if req.Target == "" {
		s.sendError(w, http.StatusBadRequest, "target address required")
		return
	}
	targetUser, targetDomain := parseEmail(req.Target)
	if targetUser == "" || targetDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid target address format")
		return
	}

	// Verify domain exists
	if _, err := s.db.GetDomain(aliasDomain); err != nil {
		s.sendError(w, http.StatusBadRequest, "domain not found")
		return
	}

	// Verify target account exists
	if _, err := s.db.GetAccount(targetDomain, targetUser); err != nil {
		s.sendError(w, http.StatusBadRequest, "target account not found")
		return
	}

	alias := &db.AliasData{
		Alias:    aliasUser,
		Domain:   aliasDomain,
		Target:   req.Target,
		IsActive: req.IsActive,
	}

	if err := s.db.CreateAlias(alias); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to create alias")
		return
	}

	s.sendJSON(w, http.StatusCreated, aliasToJSON(alias))
}

func (s *Server) getAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	data, err := s.db.GetAlias(aliasDomain, aliasUser)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "alias not found")
		return
	}

	s.sendJSON(w, http.StatusOK, aliasToJSON(data))
}

func (s *Server) updateAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	data, err := s.db.GetAlias(aliasDomain, aliasUser)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "alias not found")
		return
	}

	var req struct {
		Target   string `json:"target"`
		IsActive *bool  `json:"is_active"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Target != "" {
		targetUser, targetDomain := parseEmail(req.Target)
		if targetUser == "" || targetDomain == "" {
			s.sendError(w, http.StatusBadRequest, "invalid target address format")
			return
		}
		data.Target = req.Target
	}

	if req.IsActive != nil {
		data.IsActive = *req.IsActive
	}

	if err := s.db.UpdateAlias(data); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to update alias")
		return
	}

	s.sendJSON(w, http.StatusOK, aliasToJSON(data))
}

func (s *Server) deleteAlias(w http.ResponseWriter, r *http.Request, alias string) {
	aliasUser, aliasDomain := parseEmail(alias)
	if aliasUser == "" || aliasDomain == "" {
		s.sendError(w, http.StatusBadRequest, "invalid alias address")
		return
	}

	if err := s.db.DeleteAlias(aliasDomain, aliasUser); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to delete alias")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
