package api

import (
	"net/http"
	"path"

	"github.com/umailserver/umailserver/internal/ratelimit"
)

// RateLimitConfigRequest represents the rate limit configuration for API requests
type RateLimitConfigRequest struct {
	// Per-IP limits (inbound)
	IPPerMinute   int `json:"ip_per_minute"`
	IPPerHour     int `json:"ip_per_hour"`
	IPPerDay      int `json:"ip_per_day"`
	IPConnections int `json:"ip_connections"`

	// Per-user limits (outbound authenticated)
	UserPerMinute     int `json:"user_per_minute"`
	UserPerHour       int `json:"user_per_hour"`
	UserPerDay        int `json:"user_per_day"`
	UserMaxRecipients int `json:"user_max_recipients"`

	// Global limits
	GlobalPerMinute int `json:"global_per_minute"`
	GlobalPerHour   int `json:"global_per_hour"`
}

// handleRateLimitConfig handles GET/PUT /api/v1/admin/ratelimits/config
func (s *Server) handleRateLimitConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetRateLimitConfig(w, r)
	case http.MethodPut:
		s.handlePutRateLimitConfig(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleGetRateLimitConfig gets the current rate limit configuration
func (s *Server) handleGetRateLimitConfig(w http.ResponseWriter, r *http.Request) {
	if s.rateLimitMgr == nil {
		s.sendError(w, http.StatusServiceUnavailable, "rate limiting not available")
		return
	}

	cfg := s.rateLimitMgr.GetConfig()
	response := RateLimitConfigRequest{
		IPPerMinute:       cfg.IPPerMinute,
		IPPerHour:         cfg.IPPerHour,
		IPPerDay:          cfg.IPPerDay,
		IPConnections:     cfg.IPConnections,
		UserPerMinute:     cfg.UserPerMinute,
		UserPerHour:       cfg.UserPerHour,
		UserPerDay:        cfg.UserPerDay,
		UserMaxRecipients: cfg.UserMaxRecipients,
		GlobalPerMinute:   cfg.GlobalPerMinute,
		GlobalPerHour:     cfg.GlobalPerHour,
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handlePutRateLimitConfig updates the rate limit configuration
func (s *Server) handlePutRateLimitConfig(w http.ResponseWriter, r *http.Request) {
	if s.rateLimitMgr == nil {
		s.sendError(w, http.StatusServiceUnavailable, "rate limiting not available")
		return
	}

	var req RateLimitConfigRequest
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate bounds for all fields
	const maxRate = 10_000_000
	fields := map[string]int{
		"ip_per_minute":       req.IPPerMinute,
		"ip_per_hour":         req.IPPerHour,
		"ip_per_day":          req.IPPerDay,
		"ip_connections":      req.IPConnections,
		"user_per_minute":     req.UserPerMinute,
		"user_per_hour":       req.UserPerHour,
		"user_per_day":        req.UserPerDay,
		"user_max_recipients": req.UserMaxRecipients,
		"global_per_minute":   req.GlobalPerMinute,
		"global_per_hour":     req.GlobalPerHour,
	}
	for name, val := range fields {
		if val < 0 {
			s.sendError(w, http.StatusBadRequest, name+" must be non-negative")
			return
		}
		if val > maxRate {
			s.sendError(w, http.StatusBadRequest, name+" exceeds maximum allowed value")
			return
		}
	}

	// Build new config from request
	newCfg := &ratelimit.Config{
		IPPerMinute:       req.IPPerMinute,
		IPPerHour:         req.IPPerHour,
		IPPerDay:          req.IPPerDay,
		IPConnections:     req.IPConnections,
		UserPerMinute:     req.UserPerMinute,
		UserPerHour:       req.UserPerHour,
		UserPerDay:        req.UserPerDay,
		UserMaxRecipients: req.UserMaxRecipients,
		GlobalPerMinute:   req.GlobalPerMinute,
		GlobalPerHour:     req.GlobalPerHour,
	}

	// Get current config to preserve CleanupInterval
	currentCfg := s.rateLimitMgr.GetConfig()
	if currentCfg != nil {
		newCfg.CleanupInterval = currentCfg.CleanupInterval
	}

	// Apply config update at runtime
	s.rateLimitMgr.SetConfig(newCfg)

	s.logger.Info("Rate limit config updated",
		"ip_per_minute", req.IPPerMinute,
		"ip_per_hour", req.IPPerHour,
		"user_per_day", req.UserPerDay,
	)

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status":  "config_updated",
		"message": "Configuration updated successfully.",
	})
}

// handleRateLimitIPStats handles GET /api/v1/admin/ratelimits/ip/:ip
func (s *Server) handleRateLimitIPStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.rateLimitMgr == nil {
		s.sendError(w, http.StatusServiceUnavailable, "rate limiting not available")
		return
	}

	ip := path.Base(r.URL.Path)
	if ip == "" || ip == "ip" {
		s.sendError(w, http.StatusBadRequest, "IP address is required")
		return
	}

	stats := s.rateLimitMgr.GetIPStats(ip)
	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"ip":    ip,
		"stats": stats,
	})
}

// handleRateLimitUserStats handles GET /api/v1/admin/ratelimits/user/:user
func (s *Server) handleRateLimitUserStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.rateLimitMgr == nil {
		s.sendError(w, http.StatusServiceUnavailable, "rate limiting not available")
		return
	}

	user := path.Base(r.URL.Path)
	if user == "" || user == "user" {
		s.sendError(w, http.StatusBadRequest, "username is required")
		return
	}

	stats := s.rateLimitMgr.GetUserStats(user)
	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"user":  user,
		"stats": stats,
	})
}
