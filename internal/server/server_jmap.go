package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/jmap"
)

// startJMAP creates and starts the JMAP server. A bind failure is returned
// (F5530), as for the other listeners, instead of only being logged.
func (s *Server) startJMAP() error {
	if !s.config.JMAP.Enabled {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.config.JMAP.Bind, s.config.JMAP.Port)

	jmapConfig := jmap.Config{
		JWTSecret:   s.config.Security.JWTSecret,
		TokenExpiry: 24 * time.Hour,
		CorsOrigins: s.config.JMAP.CorsOrigins,
	}

	jmapServer := jmap.NewServer(s.storageDB, s.msgStore, s.logger, jmapConfig)
	jmapServer.SetTracingProvider(s.tracingProvider)
	jmapServer.SetQuotaLimitFunc(s.quotaLimit)
	jmapServer.SetQuotaAdjustFunc(s.quotaAdjust)
	jmapServer.SetTokenValidator(s.jmapTokenValidator)
	jmapServer.SetClaimsValidator(s.jmapClaimsValidator)
	// Clear orphaned uploads left from before a restart (F6280).
	jmapServer.StartUploadGC()

	srv := &http.Server{
		Addr:              addr,
		Handler:           jmapServer,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := s.serveHTTP("JMAP", srv); err != nil {
		return err
	}
	s.jmapServer = jmapServer
	s.jmapHTTPServer = srv

	s.logger.Info("JMAP server started", "addr", addr)
	return nil
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
	localPart, domain := parseEmail(normalizeLogin(subject))
	account, err := s.database.GetAccount(domain, localPart)
	if err == nil && account != nil && !account.IsActive {
		return errors.New("account is disabled")
	}
	return nil
}

// jmapClaimsValidator applies the API's session cut-off to JMAP tokens: a
// deleted account, or a token issued before a password change/reset, disable
// or demotion, no longer works on /jmap/* (F6250-F6252).
func (s *Server) jmapClaimsValidator(claims map[string]interface{}) error {
	if s.database == nil {
		return nil
	}
	sub, _ := claims["sub"].(string)
	localPart, domain := parseEmail(normalizeLogin(sub))
	account, err := s.database.GetAccount(domain, localPart)
	if err != nil || account == nil {
		return errors.New("account not found")
	}
	if !account.IsActive {
		return errors.New("account is disabled")
	}
	if !api.TokenIssuedAfterCutoff(claims, account) {
		return errors.New("session has been revoked")
	}
	return nil
}
