package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/umailserver/umailserver/internal/db"
)

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field      string `json:"field"`    // from, to, subject, body, header
	Operator   string `json:"operator"` // contains, equals, startsWith, endsWith, matches
	Value      string `json:"value"`
	HeaderName string `json:"headerName,omitempty"`
}

// FilterAction represents a single filter action
type FilterAction struct {
	Type      string `json:"type"`                // move, copy, delete, markRead, markSpam, forward, flag
	Target    string `json:"target,omitempty"`    // for move/copy
	ForwardTo string `json:"forwardTo,omitempty"` // for forward
}

// EmailFilter represents a user's email filter
type EmailFilter struct {
	ID         string            `json:"id"`
	UserID     string            `json:"user_id"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	MatchAll   bool              `json:"matchAll"`
	Conditions []FilterCondition `json:"conditions"`
	Actions    []FilterAction    `json:"actions"`
	Priority   int               `json:"priority"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// handleFilters handles GET/POST /api/v1/filters
func (s *Server) handleFilters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetFilters(w, r)
	case http.MethodPost:
		s.handleCreateFilter(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFilter handles GET/PUT/DELETE /api/v1/filters/:id
func (s *Server) handleFilter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetFilter(w, r)
	case http.MethodPut:
		s.handleUpdateFilter(w, r)
	case http.MethodDelete:
		s.handleDeleteFilter(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetFilters gets all filters for the current user
func (s *Server) handleGetFilters(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get filters for user
	filters, err := s.getUserFilters(user)
	if err != nil {
		s.logger.Error("Failed to get filters", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to get filters")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"filters": filters,
	})
}

// handleGetFilter gets a single filter
func (s *Server) handleGetFilter(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get filter ID from path
	filterID := path.Base(r.URL.Path)

	// Get filter
	filter, err := s.getFilter(user, filterID)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "filter not found")
		return
	}

	s.sendJSON(w, http.StatusOK, filter)
}

// handleCreateFilter creates a new filter
func (s *Server) handleCreateFilter(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse request body
	var req struct {
		Name       string            `json:"name"`
		MatchAll   bool              `json:"matchAll"`
		Conditions []FilterCondition `json:"conditions"`
		Actions    []FilterAction    `json:"actions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if req.Name == "" {
		s.sendError(w, http.StatusBadRequest, "filter name is required")
		return
	}
	if len(req.Name) > 255 {
		s.sendError(w, http.StatusBadRequest, "filter name exceeds maximum length of 255")
		return
	}
	if len(req.Conditions) == 0 {
		s.sendError(w, http.StatusBadRequest, "at least one condition is required")
		return
	}
	if len(req.Conditions) > 50 {
		s.sendError(w, http.StatusBadRequest, "too many conditions (max 50)")
		return
	}
	if len(req.Actions) == 0 {
		s.sendError(w, http.StatusBadRequest, "at least one action is required")
		return
	}
	if len(req.Actions) > 20 {
		s.sendError(w, http.StatusBadRequest, "too many actions (max 20)")
		return
	}

	// Validate condition values
	for i, cond := range req.Conditions {
		if cond.Value == "" {
			s.sendError(w, http.StatusBadRequest, fmt.Sprintf("condition %d has empty value", i+1))
			return
		}
		if len(cond.Value) > 1000 {
			s.sendError(w, http.StatusBadRequest, fmt.Sprintf("condition %d value exceeds maximum length", i+1))
			return
		}
		if cond.Field == "header" && cond.HeaderName == "" {
			s.sendError(w, http.StatusBadRequest, fmt.Sprintf("condition %d requires headerName", i+1))
			return
		}
	}

	// Create filter
	filter := &EmailFilter{
		ID:         uuid.New().String(),
		UserID:     user,
		Name:       req.Name,
		Enabled:    true,
		MatchAll:   req.MatchAll,
		Conditions: req.Conditions,
		Actions:    req.Actions,
		Priority:   0, // Will be set based on order
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Save filter
	if err := s.saveFilter(filter); err != nil {
		s.logger.Error("Failed to save filter", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to save filter")
		return
	}

	s.sendJSON(w, http.StatusCreated, filter)
}

// handleUpdateFilter updates an existing filter
func (s *Server) handleUpdateFilter(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get filter ID from path
	filterID := path.Base(r.URL.Path)

	// Get existing filter
	existing, err := s.getFilter(user, filterID)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "filter not found")
		return
	}

	// Parse request body
	var req struct {
		Name       string            `json:"name"`
		Enabled    *bool             `json:"enabled,omitempty"`
		MatchAll   bool              `json:"matchAll"`
		Conditions []FilterCondition `json:"conditions"`
		Actions    []FilterAction    `json:"actions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Update fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.MatchAll = req.MatchAll
	if len(req.Conditions) > 0 {
		existing.Conditions = req.Conditions
	}
	if len(req.Actions) > 0 {
		existing.Actions = req.Actions
	}
	existing.UpdatedAt = time.Now()

	// Save filter
	if err := s.saveFilter(existing); err != nil {
		s.logger.Error("Failed to update filter", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to update filter")
		return
	}

	s.sendJSON(w, http.StatusOK, existing)
}

// handleDeleteFilter deletes a filter
func (s *Server) handleDeleteFilter(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get filter ID from path
	filterID := path.Base(r.URL.Path)

	// Delete filter
	if err := s.deleteFilter(user, filterID); err != nil {
		s.sendError(w, http.StatusNotFound, "filter not found")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
	})
}

// handleFilterToggle handles POST /api/v1/filters/:id/toggle
func (s *Server) handleFilterToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get filter ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid filter id")
		return
	}
	filterID := pathParts[len(pathParts)-2]

	// Get existing filter
	existing, err := s.getFilter(user, filterID)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "filter not found")
		return
	}

	// Toggle enabled state
	existing.Enabled = !existing.Enabled
	existing.UpdatedAt = time.Now()

	// Save filter
	if err := s.saveFilter(existing); err != nil {
		s.logger.Error("Failed to toggle filter", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to toggle filter")
		return
	}

	s.sendJSON(w, http.StatusOK, existing)
}

// handleFilterPath routes filter requests including toggle paths
func (s *Server) handleFilterPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// Check if this is a toggle request: /api/v1/filters/{id}/toggle
	if len(parts) >= 4 && parts[len(parts)-1] == "toggle" {
		s.handleFilterToggle(w, r)
		return
	}

	// Otherwise fall through to handleFilter
	s.handleFilter(w, r)
}

// handleFilterReorder handles POST /api/v1/filters/reorder
func (s *Server) handleFilterReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse request body
	var req struct {
		FilterIDs []string `json:"filterIds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Reorder filters
	if err := s.reorderFilters(user, req.FilterIDs); err != nil {
		s.logger.Error("Failed to reorder filters", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to reorder filters")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "reordered",
	})
}

// Database/storage functions

// filterKey returns the database key for a filter
func filterKey(userID, filterID string) string {
	return fmt.Sprintf("%s:%s", userID, filterID)
}

// getUserFilters gets all filters for a user
func (s *Server) getUserFilters(userID string) ([]*EmailFilter, error) {
	// Use interface if set
	if s.filterMgr != nil {
		return s.filterMgr.GetUserFilters(userID)
	}
	if s.db == nil {
		return []*EmailFilter{}, nil
	}

	var filters []*EmailFilter
	prefix := userID + ":"

	err := s.db.ForEachPrefix(db.BucketFilters, prefix, func(key string, value []byte) error {
		var filter EmailFilter
		if err := json.Unmarshal(value, &filter); err != nil {
			return nil // Skip invalid entries
		}
		filters = append(filters, &filter)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by priority
	for i := range filters {
		filters[i].Priority = i
	}

	return filters, nil
}

// getFilter gets a single filter by ID
func (s *Server) getFilter(userID, filterID string) (*EmailFilter, error) {
	// Check for mock error injection (used in tests)
	if s.filterGetError != nil {
		return nil, s.filterGetError
	}
	// Use interface if set
	if s.filterMgr != nil {
		return s.filterMgr.GetFilter(userID, filterID)
	}
	if s.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	key := filterKey(userID, filterID)
	var filter EmailFilter
	if err := s.db.Get(db.BucketFilters, key, &filter); err != nil {
		return nil, err
	}

	return &filter, nil
}

// saveFilter saves a filter
func (s *Server) saveFilter(filter *EmailFilter) error {
	// Check for mock error injection (used in tests)
	if s.filterSaveError != nil {
		return s.filterSaveError
	}
	// Use interface if set
	if s.filterMgr != nil {
		return s.filterMgr.SaveFilter(filter)
	}
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	key := filterKey(filter.UserID, filter.ID)
	return s.db.Put(db.BucketFilters, key, filter)
}

// deleteFilter deletes a filter
func (s *Server) deleteFilter(userID, filterID string) error {
	// Use interface if set
	if s.filterMgr != nil {
		return s.filterMgr.DeleteFilter(userID, filterID)
	}
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	key := filterKey(userID, filterID)
	return s.db.Delete(db.BucketFilters, key)
}

// reorderFilters updates the priority of filters
func (s *Server) reorderFilters(userID string, filterIDs []string) error {
	// Use interface if set
	if s.filterMgr != nil {
		return s.filterMgr.ReorderFilters(userID, filterIDs)
	}
	if s.db == nil {
		return fmt.Errorf("database not available")
	}

	for priority, filterID := range filterIDs {
		filter, err := s.getFilter(userID, filterID)
		if err != nil {
			continue
		}
		filter.Priority = priority
		filter.UpdatedAt = time.Now()
		s.saveFilter(filter)
	}

	return nil
}
