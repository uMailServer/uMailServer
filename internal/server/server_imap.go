package server

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/umailserver/umailserver/internal/imap"
)

// startIMAP creates and starts the IMAP server.
func (s *Server) startIMAP(mailstore *imap.BboltMailstore) error {
	if !s.config.IMAP.Enabled {
		s.logger.Info("IMAP disabled; skipping listener")
		return nil
	}

	imapAddr := fmt.Sprintf("%s:%d", s.config.IMAP.Bind, s.config.IMAP.Port)
	imapServer := s.newIMAPServer(imapAddr, mailstore)

	// imap.port is implicit TLS (RFC 8314, default 993). It used to be a
	// plaintext listener, so implicit-TLS clients (what autoconfig
	// advertises for 993) failed the handshake (F5520). Without a usable
	// certificate the listener stays plaintext: auth is refused there and
	// STARTTLS is offered once the certificate becomes loadable.
	implicitTLS := false
	if err := s.imapCertificateError(); err != nil {
		s.logger.Error("IMAP implicit TLS unavailable; clients cannot authenticate", "addr", imapAddr, "error", err)
	} else {
		// Every connection on this listener is TLS, but imap sessions only
		// count as TLS after STARTTLS, so LOGIN/AUTHENTICATE would be refused.
		imapServer.SetAllowPlainAuth(true)
		if err := imapServer.StartTLS(); err != nil {
			return fmt.Errorf("failed to start IMAP server: %w", err)
		}
		implicitTLS = true
	}
	if !implicitTLS {
		if err := imapServer.Start(); err != nil {
			return fmt.Errorf("failed to start IMAP server: %w", err)
		}
	}

	// imap.starttls_port (default 143) was never bound (F5521). It serves
	// plaintext IMAP with STARTTLS; auth stays refused until TLS is active.
	if port := s.config.IMAP.STARTTLSPort; port > 0 && port != s.config.IMAP.Port {
		starttlsAddr := fmt.Sprintf("%s:%d", s.config.IMAP.Bind, port)
		starttlsServer := s.newIMAPServer(starttlsAddr, mailstore)
		if err := starttlsServer.Start(); err != nil {
			if stopErr := imapServer.Stop(); stopErr != nil {
				s.logger.Debug("failed to stop IMAP server", "error", stopErr)
			}
			return fmt.Errorf("failed to start IMAP STARTTLS server: %w", err)
		}
		// Stop cancels s.ctx and waits on s.wg before closing the databases
		// the sessions use, so this listener is drained before them.
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			<-s.ctx.Done()
			if err := starttlsServer.Stop(); err != nil {
				s.logger.Error("Failed to stop IMAP STARTTLS server", "error", err)
			}
		}()
		s.logger.Info("IMAP STARTTLS server started", "addr", starttlsAddr)
	}

	s.imapServer = imapServer
	s.logger.Info("IMAP server started", "addr", imapAddr, "implicit_tls", implicitTLS)
	return nil
}

// newIMAPServer returns an IMAP server for addr with the shared settings.
func (s *Server) newIMAPServer(addr string, mailstore *imap.BboltMailstore) *imap.Server {
	imapServer := imap.NewServer(&imap.Config{
		Addr:      addr,
		TLSConfig: s.tlsManager.GetTLSConfig(),
		Logger:    s.logger,
	}, mailstore)
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
	return imapServer
}

// imapCertificateError reports why the TLS manager cannot serve a
// certificate for implicit TLS, or nil if it can. ACME certificates are
// obtained on demand at handshake time, so they are not probed here.
func (s *Server) imapCertificateError() error {
	if s.config.TLS.ACME.Enabled {
		return nil
	}
	_, err := s.tlsManager.GetCertificate(&tls.ClientHelloInfo{ServerName: s.config.Server.Hostname})
	return err
}
