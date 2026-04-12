package api

import (
	"encoding/json"
	"net/http"
)

// handleHealth is the Kubernetes liveness probe endpoint
//
//	@Summary Get server health status
//	@Description Returns the health status of the server including database, queue, and storage checks
//	@Tags Health
//	@Produce json
//	@Success 200 {object} map[string]interface{} "Server is healthy"
//	@Success 503 {object} map[string]interface{} "Server is unhealthy"
//	@Router /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	result := map[string]interface{}{
		"status": "healthy",
	}

	// Check if server is draining (graceful shutdown in progress)
	if s.draining.Load() {
		result["draining"] = true
		result["status"] = "draining"
	}

	// Check database
	if s.db == nil {
		status = http.StatusServiceUnavailable
		result["status"] = "unhealthy"
		result["database"] = "not initialized"
	} else {
		if _, err := s.db.ListDomains(); err != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["database"] = "unavailable"
		} else {
			result["database"] = "ok"
		}
	}

	// Check queue manager
	if s.queueMgr != nil {
		// Check for mock error injection (used in tests)
		if s.queueMgrStatsError != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["queue"] = "unavailable"
		} else if _, err := s.queueMgr.GetStats(); err != nil {
			status = http.StatusServiceUnavailable
			result["status"] = "unhealthy"
			result["queue"] = "unavailable"
		} else {
			result["queue"] = "ok"
		}
	}

	// Check message store
	if s.msgStore != nil {
		result["storage"] = "ok"
	}

	s.sendJSON(w, status, result)
}

// handleReady is the Kubernetes readiness probe endpoint
// Returns 200 if the server is ready to accept traffic, 503 if draining
//
//	@Summary Get server readiness for zero-downtime deployment
//	@Description Returns whether the server is ready to accept traffic. Used for kubernetes readiness probes.
//	@Tags Health
//	@Produce json
//	@Success 200 {object} map[string]interface{} "Server is ready"
//	@Success 503 {object} map[string]interface{} "Server is not ready"
//	@Router /health/ready [get]
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// If draining, report not ready
	if s.draining.Load() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "not ready",
			"reason": "server is draining for graceful shutdown",
		})
		return
	}

	// Check database connectivity
	if s.db != nil {
		if _, err := s.db.ListDomains(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "not ready",
				"reason": "database unavailable",
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
	})
}
