package server

import (
	"fmt"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/pop3"
)

// startPOP3 creates and starts the POP3 server (if enabled).
func (s *Server) startPOP3(mailstore *imap.BboltMailstore) error {
	if !s.config.POP3.Enabled {
		s.logger.Info("POP3 disabled; skipping listener")
		return nil
	}

	pop3Addr := fmt.Sprintf("%s:%d", s.config.POP3.Bind, s.config.POP3.Port)
	pop3Adapter := &pop3MailstoreAdapter{
		mailstore: mailstore,
		msgStore:  s.msgStore,
	}
	pop3Server := pop3.NewServer(pop3Addr, pop3Adapter, s.logger)
	pop3Server.SetAuthFunc(s.authenticate)
	pop3Server.SetQuotaReleaseFunc(func(user string, bytes int64) { s.quotaAdjust(user, -bytes) })
	pop3Server.SetRequireTLS(true)
	pop3Server.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	pop3Server.SetReadTimeout(10 * time.Minute)
	pop3Server.SetWriteTimeout(10 * time.Minute)
	pop3Server.SetMaxConnections(s.config.POP3.MaxConnections)
	pop3Server.SetLoginResultHandler(s.protoLoginHandler("pop3"))
	pop3Server.SetTracingProvider(s.tracingProvider)

	// The POP3 port is implicit TLS (RFC 8314, default 995). The TLS config
	// used to be gated on tlsManager.IsEnabled(), which New never sets, and
	// the listener was always plaintext: with requireTLS on, USER/PASS were
	// refused and STLS was unavailable, so no client could log in (F5420).
	// pop3 loads certificates from files, so a manual cert/key is required.
	// Without a loadable one the listener stays plaintext: auth is refused
	// there and STLS is offered once the certificate becomes loadable.
	implicitTLS := false
	if s.config.TLS.CertFile != "" && s.config.TLS.KeyFile != "" {
		pop3Server.SetTLSConfig(&pop3.TLSConfig{
			CertFile: s.config.TLS.CertFile,
			KeyFile:  s.config.TLS.KeyFile,
		})
		if err := pop3Server.StartTLS(); err != nil {
			s.logger.Error("POP3 implicit TLS unavailable; clients cannot authenticate", "addr", pop3Addr, "error", err)
		} else {
			implicitTLS = true
		}
	} else {
		s.logger.Warn("POP3 has no tls.cert_file/tls.key_file; clients cannot authenticate", "addr", pop3Addr)
	}
	if !implicitTLS {
		if err := pop3Server.Start(); err != nil {
			return fmt.Errorf("failed to start POP3 server: %w", err)
		}
	}
	s.pop3Server = pop3Server
	s.logger.Info("POP3 server started", "addr", pop3Addr)
	return nil
}
