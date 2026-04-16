package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleJWTRotate handles POST /api/v1/admin/jwt/rotate to rotate JWT secret
// It generates a new key ID and secret, keeping old secrets for backward compatibility
func (s *Server) handleJWTRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Generate new key ID and secret
	newKid := fmt.Sprintf("k%d", time.Now().UnixNano())
	newSecret := generateSecureJWTSecret()

	// Add new secret to versions map, pruning old secrets to limit exposure
	const maxJWTSecretVersions = 5
	s.jwtSecrets[newKid] = newSecret
	s.currentKid = newKid
	if len(s.jwtSecrets) > maxJWTSecretVersions {
		// Prune oldest secrets (lowest timestamp in kid = k<timestamp>)
		for len(s.jwtSecrets) > maxJWTSecretVersions {
			var oldest string
			var oldestTs int64 = -1
			for kid := range s.jwtSecrets {
				if kid == s.currentKid {
					continue
				}
				// Parse timestamp from kid format: k<timestamp>
				tsStr := strings.TrimPrefix(kid, "k")
				ts, err := strconv.ParseInt(tsStr, 10, 64)
				if err != nil {
					continue
				}
				if oldestTs == -1 || ts < oldestTs {
					oldest = kid
					oldestTs = ts
				}
			}
			if oldest != "" {
				delete(s.jwtSecrets, oldest)
				s.logger.Info("Pruned old JWT secret", "kid", oldest)
			} else {
				break
			}
		}
	}

	s.logger.Info("JWT secret rotated", "newKid", newKid, "activeKeys", len(s.jwtSecrets))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "rotated",
		"newKid":     newKid,
		"message":    "JWT secret rotated successfully. Old tokens remain valid until they expire.",
		"activeKids": len(s.jwtSecrets),
	})
}

// handleJWTStatus handles GET /api/v1/admin/jwt/status to get JWT secret status
func (s *Server) handleJWTStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return status (not the actual secrets for security)
	activeKids := make([]string, 0, len(s.jwtSecrets))
	for kid := range s.jwtSecrets {
		activeKids = append(activeKids, kid)
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"currentKid": s.currentKid,
		"activeKeys": len(s.jwtSecrets),
		"activeKids": activeKids,
	})
}

// generateSecureJWTSecret generates a cryptographically secure 32-byte hex token for JWT signing
func generateSecureJWTSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
