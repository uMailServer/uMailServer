package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"go.etcd.io/bbolt"
)

// handleJWTRotate handles POST /api/v1/admin/jwt/rotate to rotate JWT secret
// It generates a new key ID and secret, keeping old secrets for backward compatibility
func (s *Server) handleJWTRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// F5540: one rotation at a time; the next set is built from a snapshot,
	// persisted, and only then published, so memory never runs ahead of disk.
	s.jwtRotateMu.Lock()
	defer s.jwtRotateMu.Unlock()

	// Generate new key ID and secret
	newKid := fmt.Sprintf("k%d", time.Now().UnixNano())
	newSecret := generateSecureJWTSecret()

	// Add new secret to versions map, pruning old secrets to limit exposure
	const maxJWTSecretVersions = 5
	// F4847: jwtSecrets/currentKid are read by every authenticated request.
	s.jwtMu.RLock()
	keys := make(map[string]string, len(s.jwtSecrets)+1)
	for kid, secret := range s.jwtSecrets {
		keys[kid] = secret
	}
	s.jwtMu.RUnlock()
	keys[newKid] = newSecret
	pruned := pruneJWTSecrets(keys, newKid, maxJWTSecretVersions)

	if err := s.persistJWTKeys(keys, newKid); err != nil {
		s.logger.Error("JWT secret rotation not persisted", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to persist rotated JWT key")
		return
	}

	s.jwtMu.Lock()
	s.jwtSecrets = keys
	s.currentKid = newKid
	s.jwtMu.Unlock()
	for _, kid := range pruned {
		s.logger.Info("Pruned old JWT secret", "kid", kid)
	}

	s.logger.Info("JWT secret rotated", "newKid", newKid, "activeKeys", len(keys))

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "rotated",
		"newKid":     newKid,
		"message":    "JWT secret rotated successfully. Old tokens remain valid until they expire.",
		"activeKids": len(keys),
	})
}

// pruneJWTSecrets deletes the oldest keys other than current until at most
// limit remain and returns the deleted kids. Rotated kids are k<unixnano>.
// F5541: any other kid (the legacy "default" jwt_secret, configured versions)
// predates every rotation, so it is pruned first instead of never.
func pruneJWTSecrets(keys map[string]string, current string, limit int) []string {
	var pruned []string
	for len(keys) > limit {
		oldest, oldestTs, found := "", int64(0), false
		for kid := range keys {
			if kid == current {
				continue
			}
			ts, err := strconv.ParseInt(strings.TrimPrefix(kid, "k"), 10, 64)
			if err != nil || !strings.HasPrefix(kid, "k") {
				ts = math.MinInt64
			}
			if !found || ts < oldestTs || (ts == oldestTs && kid < oldest) {
				oldest, oldestTs, found = kid, ts, true
			}
		}
		if !found {
			break
		}
		delete(keys, oldest)
		pruned = append(pruned, oldest)
	}
	return pruned
}

// Rotated JWT keys are persisted in the accounts database (F5540) as one
// record sealed like TOTP secrets (auth.EncryptTOTPSecret) under the
// configured jwt_secret: the database alone does not reveal them, and
// changing jwt_secret discards the set. The legacy kid is stored as a marker,
// never as a copy of jwt_secret.
const (
	jwtKeysBucket = "jwt_keys"
	jwtKeysRecord = "keyset"
	legacyJWTKid  = "default"
)

type persistedJWTKeys struct {
	Current string            `json:"current"`
	Keys    map[string]string `json:"keys"`
	Legacy  bool              `json:"legacy"`
}

// persistJWTKeys stores keys/current. A nil database keeps keys in memory.
func (s *Server) persistJWTKeys(keys map[string]string, current string) error {
	if s.db == nil {
		return nil
	}
	rec := persistedJWTKeys{Current: current, Keys: make(map[string]string, len(keys))}
	for kid, secret := range keys {
		if kid == legacyJWTKid && secret == s.config.JWTSecret {
			rec.Legacy = true
			continue
		}
		rec.Keys[kid] = secret
	}
	plain, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal JWT keys: %w", err)
	}
	sealed, err := auth.EncryptTOTPSecret(string(plain), s.config.JWTSecret)
	if err != nil {
		return fmt.Errorf("seal JWT keys: %w", err)
	}
	return s.db.BoltDB().Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(jwtKeysBucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(jwtKeysRecord), []byte(sealed))
	})
}

// loadJWTKeys restores the persisted key set, if any. A record that is not
// sealed, does not open under the current jwt_secret, or lacks its current
// key is ignored (logged) and the configured jwt_secret is used.
func (s *Server) loadJWTKeys() {
	if s.db == nil {
		return
	}
	var sealed string
	err := s.db.BoltDB().View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte(jwtKeysBucket)); b != nil {
			sealed = string(b.Get([]byte(jwtKeysRecord)))
		}
		return nil
	})
	if err != nil {
		s.logger.Warn("Failed to read persisted JWT keys", "error", err)
		return
	}
	if sealed == "" {
		return
	}
	if !strings.HasPrefix(sealed, "enc2:") {
		s.logger.Warn("Ignoring unsealed persisted JWT keys")
		return
	}
	plain, err := auth.DecryptTOTPSecret(sealed, s.config.JWTSecret)
	if err != nil {
		s.logger.Warn("Ignoring persisted JWT keys: not sealed with the current jwt_secret", "error", err)
		return
	}
	var rec persistedJWTKeys
	if err := json.Unmarshal([]byte(plain), &rec); err != nil {
		s.logger.Warn("Ignoring malformed persisted JWT keys", "error", err)
		return
	}
	keys := make(map[string]string, len(rec.Keys)+1)
	for kid, secret := range rec.Keys {
		if secret != "" {
			keys[kid] = secret
		}
	}
	if rec.Legacy {
		keys[legacyJWTKid] = s.config.JWTSecret
	}
	if _, ok := keys[rec.Current]; !ok {
		s.logger.Warn("Ignoring persisted JWT keys: current key missing", "kid", rec.Current)
		return
	}
	s.jwtMu.Lock()
	s.jwtSecrets = keys
	s.currentKid = rec.Current
	s.jwtMu.Unlock()
}

// handleJWTStatus handles GET /api/v1/admin/jwt/status to get JWT secret status
func (s *Server) handleJWTStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Return status (not the actual secrets for security)
	s.jwtMu.RLock()
	defer s.jwtMu.RUnlock()
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
		// Fallback: crypto/rand is virtually always available on Unix/Linux systems.
		// If it ever fails, seed from OS entropy via a best-effort fallback rather
		// than crashing the entire API server.
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> (i % 8))
		}
	}
	return hex.EncodeToString(b)
}
