package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ClusterConfig holds cluster configuration for the API server
type ClusterConfig struct {
	RedisURL   string `json:"redis_url"`
	InstanceID string `json:"instance_id"`
	Enabled    bool   `json:"enabled"`
}

// handleClusterStatus handles GET /api/v1/cluster/status
func (s *Server) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if cluster is enabled
	if s.clusterMgr == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":   false,
			"status":    "disabled",
			"instances": []interface{}{},
		})
		return
	}

	health, err := s.clusterMgr.HealthMonitor().GetInstanceHealth(context.Background())
	if err != nil {
		http.Error(w, "Failed to get cluster health", http.StatusInternalServerError)
		return
	}

	// Determine if this instance is leader
	isLeader := false
	for _, h := range health {
		if h.InstanceID == s.clusterConfig.InstanceID {
			isLeader = h.IsLeader
			break
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":     true,
		"is_leader":   isLeader,
		"instance_id": s.clusterConfig.InstanceID,
		"instances":   health,
	})
}

// handleClusterInstances handles GET /api/v1/cluster/instances
func (s *Server) handleClusterInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.clusterMgr == nil {
		http.Error(w, "Cluster not enabled", http.StatusServiceUnavailable)
		return
	}

	health, err := s.clusterMgr.HealthMonitor().GetInstanceHealth(context.Background())
	if err != nil {
		http.Error(w, "Failed to get instances", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"instances": health,
	})
}

// handleClusterFailover handles POST /api/v1/cluster/failover
func (s *Server) handleClusterFailover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.clusterMgr == nil {
		http.Error(w, "Cluster not enabled", http.StatusServiceUnavailable)
		return
	}

	// Check if this instance is the leader
	leader, err := s.clusterMgr.LeaderElection().IsLeader(context.Background(), "server")
	if err != nil {
		http.Error(w, "Failed to check leadership", http.StatusInternalServerError)
		return
	}

	if !leader {
		http.Error(w, "Not the leader, cannot trigger failover", http.StatusForbidden)
		return
	}

	// Get list of other healthy instances
	health, err := s.clusterMgr.HealthMonitor().GetInstanceHealth(context.Background())
	if err != nil {
		http.Error(w, "Failed to get cluster health", http.StatusInternalServerError)
		return
	}

	// Find another instance to become leader
	var nextLeader string
	for _, h := range health {
		if h.InstanceID != s.clusterConfig.InstanceID && h.Status == "healthy" {
			nextLeader = h.InstanceID
			break
		}
	}

	if nextLeader == "" {
		http.Error(w, "No suitable failover target found", http.StatusServiceUnavailable)
		return
	}

	// Release leadership to trigger failover
	err = s.clusterMgr.LeaderElection().Release(context.Background(), "server")
	if err != nil {
		http.Error(w, "Failed to release leadership", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"new_leader": nextLeader,
		"message":    "Failover triggered successfully",
	})
}

// handleClusterHeartbeat handles POST /api/v1/cluster/heartbeat
func (s *Server) handleClusterHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.clusterMgr == nil {
		http.Error(w, "Cluster not enabled", http.StatusServiceUnavailable)
		return
	}

	err := s.clusterMgr.HealthMonitor().RecordHeartbeat(context.Background())
	if err != nil {
		http.Error(w, "Failed to record heartbeat", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"instance_id": s.clusterConfig.InstanceID,
		"timestamp":   time.Now().Unix(),
	})
}
