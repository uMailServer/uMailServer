package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/sieve"
)

// startManageSieve creates and starts the ManageSieve server on port 4190
func (s *Server) startManageSieve() error {
	s.attachSieveStore()
	if !s.config.ManageSieve.Enabled {
		return nil
	}

	addr, tlsAddr := manageSieveAddrs(s.config.ManageSieve.Bind, s.config.ManageSieve.Port)
	tlsCfg := s.tlsManager.GetTLSConfig()

	sieveServer := sieve.NewManageSieveServer(s.sieveManager, tlsCfg)
	sieveServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	// F5116: the configured bind/port were only logged; the listener always
	// bound the package default 0.0.0.0:4190 (and 0.0.0.0:4191 for TLS).
	sieveServer.SetListenAddrs(addr, tlsAddr)
	// Set auth handler for ManageSieve (uses same auth as submission SMTP)
	sieveServer.SetAuthHandler(func(user, pass string) bool {
		ok, _ := s.authenticate(user, pass)
		return ok
	})
	// F5631: scripts are keyed by the mailbox address delivery looks up, not
	// by whatever string the user typed (an LDAP uid has no domain).
	sieveServer.SetUserResolver(s.sieveUserKey)
	sieveServer.SetTracingProvider(s.tracingProvider)
	if err := sieveServer.Listen(); err != nil {
		// Listen may fail after the plain listener is already serving
		// (TLS bind error); close it so it is not left running unowned.
		_ = sieveServer.Close()
		return fmt.Errorf("failed to start ManageSieve server: %w", err)
	}

	s.manageSieveServer = sieveServer
	s.logger.Info("ManageSieve server started", "addr", addr, "tls_addr", tlsAddr)
	return nil
}

// manageSieveAddrs returns the plain and implicit-TLS listen addresses for
// the configured bind host and port. The TLS listener takes the next port
// (4191 for the default 4190), or an ephemeral one when port is 0.
func manageSieveAddrs(bind string, port int) (addr, tlsAddr string) {
	tlsPort := 0
	if port > 0 && port < 65535 {
		tlsPort = port + 1
	}
	return net.JoinHostPort(bind, strconv.Itoa(port)), net.JoinHostPort(bind, strconv.Itoa(tlsPort))
}

// attachSieveStore loads the persisted Sieve scripts from the accounts
// database and makes every later change durable (F5630). It is idempotent and
// is called both before ManageSieve starts and from delivery, whichever runs
// first, so a restart never delivers with an empty script set.
func (s *Server) attachSieveStore() {
	if s.sieveManager == nil || s.database == nil || s.sieveManager.StoreAttached() {
		return
	}
	if err := s.sieveManager.AttachStore(s.database.BoltDB(), s.logger.Warn); err != nil {
		s.logger.Error("Sieve scripts are not persisted", "error", err)
	}
}

// sieveUserKey returns the script owner key for an authenticated login.
func (s *Server) sieveUserKey(login string) string {
	return canonicalSieveUser(login, s.ldapMailAddress)
}

// ldapMailAddress returns the LDAP mail attribute of login, or "".
func (s *Server) ldapMailAddress(login string) string {
	if s.ldapClient == nil {
		return ""
	}
	user, err := s.ldapClient.GetUser(login)
	if err != nil || user == nil {
		s.logger.Warn("Sieve: cannot resolve LDAP login to a mailbox address", "login", login, "error", err)
		return ""
	}
	return user.Email
}

// canonicalSieveUser maps a login to the lowercase mailbox address under which
// its scripts are stored and delivery looks them up (F5631). A login that is
// an address is lowercased; a bare directory uid uses lookupEmail; when
// neither yields an address the login is kept.
func canonicalSieveUser(login string, lookupEmail func(login string) string) string {
	l := strings.TrimSpace(login)
	if strings.Contains(l, "@") {
		return strings.ToLower(l)
	}
	if lookupEmail != nil {
		if email := strings.TrimSpace(lookupEmail(l)); strings.Contains(email, "@") {
			return strings.ToLower(email)
		}
	}
	return login
}
