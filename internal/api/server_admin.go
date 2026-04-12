package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
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

	// Add new secret to versions map (keeping old ones for backward compatibility)
	s.jwtSecrets[newKid] = newSecret
	s.currentKid = newKid

	s.logger.Info("JWT secret rotated", "newKid", newKid)

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
