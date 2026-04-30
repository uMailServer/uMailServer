package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/umailserver/umailserver/internal/push"
)

// handlePushVAPID handles GET /api/v1/push/vapid-public-key
func (s *Server) handlePushVAPID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get VAPID public key
	publicKey := s.getVAPIDPublicKey()
	if publicKey == "" {
		s.sendError(w, http.StatusServiceUnavailable, "push notifications not configured")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"publicKey": publicKey,
	})
}

// handlePushSubscribe handles POST /api/v1/push/subscribe
func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
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
		Endpoint   string `json:"endpoint"`
		P256dh     string `json:"p256dh"`
		Auth       string `json:"auth"`
		DeviceType string `json:"deviceType,omitempty"`
		OS         string `json:"os,omitempty"`
		Browser    string `json:"browser,omitempty"`
		Name       string `json:"name,omitempty"`
	}

	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate required fields
	if req.Endpoint == "" || req.P256dh == "" || req.Auth == "" {
		s.sendError(w, http.StatusBadRequest, "endpoint, p256dh, and auth are required")
		return
	}

	// Validate endpoint URL to prevent SSRF / exfiltration
	endpointURL, err := url.Parse(req.Endpoint)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid endpoint URL")
		return
	}
	if endpointURL.Scheme != "https" {
		s.sendError(w, http.StatusBadRequest, "endpoint must use HTTPS")
		return
	}
	if endpointURL.Host == "" {
		s.sendError(w, http.StatusBadRequest, "invalid endpoint host")
		return
	}

	// Create subscription
	sub := &push.Subscription{
		Endpoint: req.Endpoint,
		P256dh:   req.P256dh,
		Auth:     req.Auth,
		DeviceInfo: push.DeviceInfo{
			DeviceType: req.DeviceType,
			OS:         req.OS,
			Browser:    req.Browser,
			Name:       req.Name,
		},
	}

	// Save subscription
	if err := s.subscribePush(user, sub); err != nil {
		s.logger.Error("Failed to subscribe to push", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}

	s.sendJSON(w, http.StatusCreated, map[string]string{
		"status":         "subscribed",
		"subscriptionId": sub.ID,
	})
}

// handlePushUnsubscribe handles DELETE /api/v1/push/unsubscribe
func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get subscription ID from query or body
	subscriptionID := r.URL.Query().Get("id")
	if subscriptionID == "" {
		// Try to get from body
		var req struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Endpoint != "" {
			// Find subscription by endpoint
			subscriptionID = s.findSubscriptionByEndpoint(user, req.Endpoint)
		}
	}

	if subscriptionID == "" {
		s.sendError(w, http.StatusBadRequest, "subscription id or endpoint required")
		return
	}

	// Unsubscribe
	if err := s.unsubscribePush(user, subscriptionID); err != nil {
		s.logger.Error("Failed to unsubscribe from push", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to unsubscribe")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "unsubscribed",
	})
}

// handlePushSubscriptions handles GET /api/v1/push/subscriptions
func (s *Server) handlePushSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get subscriptions
	subs := s.getPushSubscriptions(user)

	// Sanitize for output (don't expose sensitive keys)
	var result []map[string]interface{}
	for _, sub := range subs {
		result = append(result, map[string]interface{}{
			"id":         sub.ID,
			"createdAt":  sub.CreatedAt,
			"updatedAt":  sub.UpdatedAt,
			"deviceInfo": sub.DeviceInfo,
		})
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"subscriptions": result,
	})
}

// handlePushTest handles POST /api/v1/push/test
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
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

	// Send test notification
	if err := s.sendTestPushNotification(user); err != nil {
		s.logger.Error("Failed to send test notification", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to send test notification")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "sent",
	})
}

// handleAdminPushStats handles GET /api/v1/admin/push/stats (admin only)
func (s *Server) handleAdminPushStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Check admin
	isAdmin, _ := r.Context().Value("isAdmin").(bool)
	if !isAdmin {
		s.sendError(w, http.StatusForbidden, "admin access required")
		return
	}

	stats := s.getPushStats()
	s.sendJSON(w, http.StatusOK, stats)
}

// Placeholder functions - would be implemented with actual push service

func (s *Server) getVAPIDPublicKey() string {
	// Use interface if set
	if s.pushSvc != nil {
		return s.pushSvc.GetVAPIDPublicKey()
	}
	// In real implementation, get from push service
	return ""
}

func (s *Server) subscribePush(userID string, sub *push.Subscription) error {
	// Check for mock error injection (used in tests)
	if s.pushSubscribeError != nil {
		return s.pushSubscribeError
	}
	// Use interface if set
	if s.pushSvc != nil {
		return s.pushSvc.Subscribe(userID, sub)
	}
	// In real implementation, call push service
	return nil
}

func (s *Server) unsubscribePush(userID, subscriptionID string) error {
	// Check for mock error injection (used in tests)
	if s.pushUnsubscribeError != nil {
		return s.pushUnsubscribeError
	}
	// Use interface if set
	if s.pushSvc != nil {
		return s.pushSvc.Unsubscribe(userID, subscriptionID)
	}
	// In real implementation, call push service
	return nil
}

func (s *Server) getPushSubscriptions(userID string) []*push.Subscription {
	// In real implementation, call push service
	return []*push.Subscription{}
}

func (s *Server) findSubscriptionByEndpoint(userID, endpoint string) string {
	// In real implementation, search in push service
	return ""
}

func (s *Server) sendTestPushNotification(userID string) error {
	// Check for mock error injection (used in tests)
	if s.pushSendError != nil {
		return s.pushSendError
	}
	// Use interface if set
	if s.pushSvc != nil {
		return s.pushSvc.SendNotification(userID, &push.Notification{
			Title: "Test Notification",
			Body:  "This is a test push notification",
		})
	}
	// In real implementation, send via push service
	return nil
}

func (s *Server) getPushStats() map[string]interface{} {
	// In real implementation, get from push service
	return map[string]interface{}{
		"totalSubscriptions": 0,
		"totalUsers":         0,
		"deviceTypes":        map[string]int{},
		"osTypes":            map[string]int{},
	}
}
