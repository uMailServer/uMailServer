package server

import "strings"

// quotaLimit returns the configured quota in bytes for a full email address,
// or 0 (unlimited) when the account is unknown or has no limit.
func (s *Server) quotaLimit(user string) int64 {
	local, domain, ok := strings.Cut(strings.ToLower(user), "@")
	if !ok || s.database == nil {
		return 0
	}
	account, err := s.database.GetAccount(domain, local)
	if err != nil || account == nil {
		return 0
	}
	return account.QuotaLimit
}

// quotaAdjust adds delta (which may be negative) to the account's tracked
// usage. A positive delta fails when it would exceed the account's limit.
func (s *Server) quotaAdjust(user string, delta int64) error {
	local, domain, ok := strings.Cut(strings.ToLower(user), "@")
	if !ok || s.database == nil {
		return nil
	}
	return s.database.IncrementQuota(domain, local, delta)
}
