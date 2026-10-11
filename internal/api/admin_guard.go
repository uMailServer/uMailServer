package api

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
	"sync"

	"github.com/umailserver/umailserver/internal/db"
)

// adminGuardMu serialises the check-then-write sequences that must keep at
// least one active administrator (F6650).
var adminGuardMu sync.Mutex

// retryAfterSeconds is advertised on every 429 (F6654).
const retryAfterSeconds = "60"

// methodNotAllowed answers 405 with the mandatory Allow header (F6654).
func (s *Server) methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// otherActiveAdmins reports whether an active admin exists outside the
// excluded accounts. skip is consulted with (domain, localPart).
func (s *Server) otherActiveAdmins(skip func(domain, local string) bool) (bool, error) {
	// Scan the accounts bucket directly: accounts of a domain that was never
	// registered are still able to log in.
	found := false
	err := s.db.ForEach(db.BucketAccounts, func(_ string, value []byte) error {
		var a db.AccountData
		if json.Unmarshal(value, &a) != nil {
			return nil
		}
		if a.IsAdmin && a.IsActive && !skip(a.Domain, a.LocalPart) {
			found = true
		}
		return nil
	})
	return found, err
}

// normalizeForwardTo validates an admin-supplied forwarding target the same
// way the self-service endpoint does and returns the bare addr-spec.
func normalizeForwardTo(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", true
	}
	addr, err := mail.ParseAddress(v)
	if err != nil || strings.ContainsAny(v, "\r\n") || addr.Address != v {
		return "", false
	}
	return addr.Address, true
}
