package api

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
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

// deliveryLocker is optionally implemented by the AccountPurger: it blocks
// local deliveries to a mailbox while its data is being purged (F6880).
type deliveryLocker interface {
	LockDelivery(email string) func()
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
	// A delivery that passed its account check before the row was deleted
	// would otherwise write after the purge and recreate the mailbox.
	if dl, ok := s.accountPurger.(deliveryLocker); ok {
		defer dl.LockDelivery(email)()
	}
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
