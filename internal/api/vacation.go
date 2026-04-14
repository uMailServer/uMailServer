package api

import (
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/vacation"
)

// VacationConfig represents vacation auto-reply configuration in API
type VacationConfig struct {
	Enabled          bool     `json:"enabled"`
	StartDate        *string  `json:"start_date,omitempty"`
	EndDate          *string  `json:"end_date,omitempty"`
	Subject          string   `json:"subject"`
	Message          string   `json:"message"`
	HTMLMessage      string   `json:"html_message,omitempty"`
	SendInterval     int      `json:"send_interval,omitempty"` // in hours
	ExcludeAddresses []string `json:"exclude_addresses,omitempty"`
	IgnoreLists      bool     `json:"ignore_lists,omitempty"`
	IgnoreBulk       bool     `json:"ignore_bulk,omitempty"`
}

// handleVacation handles GET/PUT /api/v1/vacation
func (s *Server) handleVacation(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetVacation(w, r)
	case http.MethodPut:
		s.handleSetVacation(w, r)
	case http.MethodDelete:
		s.handleDeleteVacation(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetVacation gets vacation configuration
func (s *Server) handleGetVacation(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get vacation manager from server (we need to add this field)
	config, err := s.getVacationConfig(user)
	if err != nil {
		s.logger.Error("Failed to get vacation config", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to get vacation config")
		return
	}

	// Convert to API response
	response := VacationConfig{
		Enabled:          config.Enabled,
		Subject:          config.Subject,
		Message:          config.Message,
		HTMLMessage:      config.HTMLMessage,
		SendInterval:     int(config.SendInterval.Hours()),
		ExcludeAddresses: config.ExcludeAddresses,
		IgnoreLists:      config.IgnoreLists,
		IgnoreBulk:       config.IgnoreBulk,
	}

	if !config.StartDate.IsZero() {
		startStr := config.StartDate.Format(time.RFC3339)
		response.StartDate = &startStr
	}
	if !config.EndDate.IsZero() {
		endStr := config.EndDate.Format(time.RFC3339)
		response.EndDate = &endStr
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handleSetVacation sets vacation configuration
func (s *Server) handleSetVacation(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse request body
	var req VacationConfig
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Convert to internal config
	config := &vacation.Config{
		Enabled:          req.Enabled,
		Subject:          req.Subject,
		Message:          req.Message,
		HTMLMessage:      req.HTMLMessage,
		SendInterval:     time.Duration(req.SendInterval) * time.Hour,
		ExcludeAddresses: req.ExcludeAddresses,
		IgnoreLists:      req.IgnoreLists,
		IgnoreBulk:       req.IgnoreBulk,
	}

	if req.StartDate != nil {
		if startDate, err := time.Parse(time.RFC3339, *req.StartDate); err == nil {
			config.StartDate = startDate
		}
	}
	if req.EndDate != nil {
		if endDate, err := time.Parse(time.RFC3339, *req.EndDate); err == nil {
			config.EndDate = endDate
		}
	}

	// Validate
	if config.Enabled && config.Subject == "" {
		s.sendError(w, http.StatusBadRequest, "subject is required when vacation is enabled")
		return
	}
	if config.Enabled && config.Message == "" {
		s.sendError(w, http.StatusBadRequest, "message is required when vacation is enabled")
		return
	}

	// Save config
	if err := s.setVacationConfig(user, config); err != nil {
		s.logger.Error("Failed to set vacation config", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to set vacation config")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "success",
	})
}

// handleDeleteVacation deletes vacation configuration
func (s *Server) handleDeleteVacation(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Delete config
	if err := s.deleteVacationConfig(user); err != nil {
		s.logger.Error("Failed to delete vacation config", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to delete vacation config")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
	})
}

// handleAdminVacations handles GET /api/v1/admin/vacations (admin only)
func (s *Server) handleAdminVacations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Check admin (should be done by middleware, but double-check)
	isAdmin, _ := r.Context().Value("isAdmin").(bool)
	if !isAdmin {
		s.sendError(w, http.StatusForbidden, "admin access required")
		return
	}

	// Get active vacations
	activeVacations := s.listActiveVacations()

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"active_vacations": activeVacations,
		"count":            len(activeVacations),
	})
}

// getVacationConfig gets vacation config for a user
func (s *Server) getVacationConfig(user string) (*vacation.Config, error) {
	// Check for mock error injection (used in tests)
	if s.vacationGetError != nil {
		return nil, s.vacationGetError
	}
	// Use interface if set
	if s.vacationMgr != nil {
		return s.vacationMgr.GetConfig(user)
	}
	// Placeholder - in real implementation, get from vacation manager
	return &vacation.Config{
		Enabled:      false,
		Subject:      "Out of Office",
		Message:      "I am currently out of office. I will respond to your email when I return.",
		SendInterval: 7 * 24 * time.Hour,
		IgnoreLists:  true,
		IgnoreBulk:   true,
	}, nil
}

// setVacationConfig sets vacation config for a user
func (s *Server) setVacationConfig(user string, config *vacation.Config) error {
	// Check for mock error injection (used in tests)
	if s.vacationSetError != nil {
		return s.vacationSetError
	}
	// Use interface if set
	if s.vacationMgr != nil {
		return s.vacationMgr.SetConfig(user, config)
	}
	// Placeholder - in real implementation, save to vacation manager
	return nil
}

// deleteVacationConfig deletes vacation config for a user
func (s *Server) deleteVacationConfig(user string) error {
	// Check for mock error injection (used in tests)
	if s.vacationDeleteError != nil {
		return s.vacationDeleteError
	}
	// Use interface if set
	if s.vacationMgr != nil {
		return s.vacationMgr.DeleteConfig(user)
	}
	// Placeholder - in real implementation, delete from vacation manager
	return nil
}

// listActiveVacations lists all active vacations
func (s *Server) listActiveVacations() []string {
	// Use interface if set
	if s.vacationMgr != nil {
		list, err := s.vacationMgr.ListActive()
		if err != nil {
			return []string{}
		}
		return list
	}
	// Placeholder - in real implementation, get from vacation manager
	return []string{}
}
