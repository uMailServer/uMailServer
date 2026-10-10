package server

import (
	"fmt"
	"strings"
)

// maxAliasHops bounds alias chains; a longer chain or a cycle is refused.
const maxAliasHops = 10

// normalizeRcpt splits a recipient address into a lowercase local part and a
// lowercase domain without a trailing root dot (RFC 5321 §2.4: domains are
// case-insensitive; mailboxes are stored lowercase, F6050).
func normalizeRcpt(addr string) (user, domain string) {
	user, domain = parseEmail(strings.TrimSpace(addr))
	return strings.ToLower(user), strings.TrimRight(strings.ToLower(domain), ".")
}

// routeRecipient decides where mail for rcpt goes. It returns either the
// local mailbox (user, domain) or, when the final address is not in an
// active local domain, the external address to relay to. Alias chains are
// followed (cycles and chains over maxAliasHops fail), an alias to an
// external address relays, and a "user+tag" address falls back to "user"
// when only the base mailbox exists (F6050-F6053).
func (s *Server) routeRecipient(rcpt string) (user, domain, external string, err error) {
	user, domain = normalizeRcpt(rcpt)
	seen := map[string]bool{}
	for hop := 0; ; hop++ {
		addr := user + "@" + domain
		domainData, derr := s.database.GetDomain(domain)
		if derr != nil || domainData == nil || !domainData.IsActive {
			return "", "", addr, nil
		}
		if seen[addr] || hop > maxAliasHops {
			return "", "", "", fmt.Errorf("alias loop or chain too long at %s", addr)
		}
		seen[addr] = true

		target, aliasErr := s.database.ResolveAlias(domain, user)
		if aliasErr != nil {
			s.logger.Debug("Alias resolution failed, trying direct delivery", "domain", domain, "user", user, "error", aliasErr)
		}
		if target == "" {
			if base, _, ok := strings.Cut(user, "+"); ok && base != "" {
				if a, aerr := s.database.GetAccount(domain, user); aerr != nil || a == nil {
					if t, _ := s.database.ResolveAlias(domain, base); t != "" {
						target = t
					} else {
						user = base
					}
				}
			}
		}
		if target != "" {
			if tUser, tDomain := normalizeRcpt(target); tUser != "" && tDomain != "" {
				user, domain = tUser, tDomain
				continue
			}
		}
		return user, domain, "", nil
	}
}

// senderAllowed reports whether the authenticated user may submit mail with
// MAIL FROM from: their own address, or an address (alias, plus address) that
// routes to their mailbox (F6041). The null sender is accepted.
func (s *Server) senderAllowed(username, from string) bool {
	if strings.TrimSpace(from) == "" {
		return true
	}
	uUser, uDomain := normalizeRcpt(username)
	fUser, fDomain := normalizeRcpt(from)
	if fUser == uUser && fDomain == uDomain {
		return true
	}
	rUser, rDomain, external, err := s.routeRecipient(from)
	if err != nil || external != "" {
		return false
	}
	return rUser == uUser && rDomain == uDomain
}
