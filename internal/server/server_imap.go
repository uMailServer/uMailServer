package server

import (
	"fmt"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

// startIMAP creates and starts the IMAP server.
func (s *Server) startIMAP(mailstore *imap.BboltMailstore) error {
	imapAddr := fmt.Sprintf("%s:%d", s.config.IMAP.Bind, s.config.IMAP.Port)
	imapCfg := &imap.Config{
		Addr:      imapAddr,
		TLSConfig: s.tlsManager.GetTLSConfig(),
		Logger:    s.logger,
	}

	imapServer := imap.NewServer(imapCfg, mailstore)
	imapServer.SetAuthFunc(s.authenticate)
	imapServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	imapServer.SetReadTimeout(10 * time.Minute)
	imapServer.SetWriteTimeout(10 * time.Minute)
	imapServer.SetIdleTimeout(time.Duration(s.config.IMAP.IdleTimeout))
	imapServer.SetMaxConnections(s.config.IMAP.MaxConnections)
	imapServer.SetTracingProvider(s.tracingProvider)
	imapServer.SetLoginResultHandler(s.protoLoginHandler("imap"))
	if s.searchSvc != nil {
		imapServer.SetOnExpunge(func(user, mailbox string, uid uint32) {
			s.searchSvc.RemoveMessage(user, mailbox, uid)
		})
	}

	if err := imapServer.Start(); err != nil {
		return fmt.Errorf("failed to start IMAP server: %w", err)
	}
	s.imapServer = imapServer
	s.logger.Info("IMAP server started", "addr", imapAddr)
	return nil
}
