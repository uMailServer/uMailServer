package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"

	"github.com/umailserver/umailserver/internal/db"
)

// adminGuardMu serialises the check-then-write sequences that must keep at
// least one active administrator (F6650).
var adminGuardMu sync.Mutex

// AccountPurger removes the data of a deleted mailbox that lives outside the
// packages the api server holds directly (sieve scripts, push subscriptions,
// ...). It is wired by the server so this package does not import it.
type AccountPurger interface {
	PurgeAccount(email string) error
}

// SetAccountPurger installs the cascade hook run on account/domain deletion.
func (s *Server) SetAccountPurger(p AccountPurger) { s.accountPurger = p }

// SetQuotaAdjustFunc installs the quota counter hook for REST mail (F6680).
func (s *Server) SetQuotaAdjustFunc(f func(user string, delta int64) error) {
	s.quotaAdjust = f
	s.initMailHandler()
}

// purgeAccountData deletes everything stored for the mailbox so a later
// account created under the same address starts empty (F6649c): blob files,
// IMAP/JMAP metadata, search index, vacation config, plus whatever the
// installed AccountPurger owns.
func (s *Server) purgeAccountData(email string) error {
	var errs []error
	if s.mailDB != nil {
		if err := s.mailDB.PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	}
	if s.msgStore != nil {
		if err := s.msgStore.PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	}
	if s.searchSvc != nil {
		s.searchSvc.ClearIndex(email)
	}
	if s.vacationMgr != nil {
		_ = s.vacationMgr.DeleteConfig(email)
	}
	if s.accountPurger != nil {
		if err := s.accountPurger.PurgeAccount(email); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

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
