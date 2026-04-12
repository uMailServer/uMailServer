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
		return nil
	}

	pop3Addr := fmt.Sprintf("%s:%d", s.config.POP3.Bind, s.config.POP3.Port)
	pop3Adapter := &pop3MailstoreAdapter{
		mailstore: mailstore,
		msgStore:  s.msgStore,
	}
	pop3Server := pop3.NewServer(pop3Addr, pop3Adapter, s.logger)
	pop3Server.SetAuthFunc(s.authenticate)
	pop3Server.SetAPOPSecretHandler(s.getAPOPSecret)
	pop3Server.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	pop3Server.SetReadTimeout(10 * time.Minute)
	pop3Server.SetWriteTimeout(10 * time.Minute)
	pop3Server.SetMaxConnections(s.config.POP3.MaxConnections)

	if s.tlsManager.IsEnabled() {
		pop3Server.SetTLSConfig(&pop3.TLSConfig{
			CertFile: s.config.TLS.CertFile,
			KeyFile:  s.config.TLS.KeyFile,
		})
	}

	if err := pop3Server.Start(); err != nil {
		return fmt.Errorf("failed to start POP3 server: %w", err)
	}
	s.pop3Server = pop3Server
	s.logger.Info("POP3 server started", "addr", pop3Addr)
	return nil
}
