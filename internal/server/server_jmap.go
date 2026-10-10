package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/jmap"
)

// startJMAP creates and starts the JMAP server
func (s *Server) startJMAP() {
	if !s.config.JMAP.Enabled {
		return
	}

	addr := fmt.Sprintf("%s:%d", s.config.JMAP.Bind, s.config.JMAP.Port)

	jmapConfig := jmap.Config{
		JWTSecret:   s.config.Security.JWTSecret,
		TokenExpiry: 24 * time.Hour,
		CorsOrigins: s.config.JMAP.CorsOrigins,
	}

	jmapServer := jmap.NewServer(s.storageDB, s.msgStore, s.logger, jmapConfig)
	jmapServer.SetTracingProvider(s.tracingProvider)
	jmapServer.SetTokenValidator(s.jmapTokenValidator)

	s.jmapServer = jmapServer

	srv := &http.Server{
		Addr:              addr,
		Handler:           jmapServer,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.jmapHTTPServer = srv

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("JMAP server error", "error", err)
		}
	}()

	s.logger.Info("JMAP server started", "addr", addr)
}

// jmapTokenValidator applies the HTTP API's token-state checks to JMAP bearer
// tokens: the persistent logout/refresh revocation list (F5330; a lookup
// error is treated as revoked, as in api.IsTokenRevoked) and the account's
// active flag (F5331; mirrors api.sessionAccountState).
func (s *Server) jmapTokenValidator(tokenHash, subject string) error {
	if s.database == nil {
		return nil
	}
	revoked, err := s.database.IsTokenRevoked(tokenHash)
	if err != nil || revoked {
		return errors.New("token has been revoked")
	}
	localPart, domain := parseEmail(subject)
	account, err := s.database.GetAccount(domain, localPart)
	if err == nil && account != nil && !account.IsActive {
		return errors.New("account is disabled")
	}
	return nil
}
