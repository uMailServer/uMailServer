package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/backup"
	"github.com/umailserver/umailserver/internal/push"
)

// startAPI creates and starts the HTTP API server (webmail + admin). The API
// and admin listeners bind synchronously, so a bind failure fails Start
// (F5530) instead of only being logged.
func (s *Server) startAPI() error {
	apiCfg := api.Config{
		Addr:             fmt.Sprintf("%s:%d", s.config.HTTP.Bind, s.config.HTTP.Port),
		JWTSecret:        s.config.Security.JWTSecret,
		DisableLegacyJWT: s.config.Security.DisableLegacyJWT,
		TOTPKey:          s.config.Security.TOTPKey,
		MaxLoginAttempts: s.config.Security.MaxLoginAttempts,
		CorsOrigins:      s.config.HTTP.CorsOrigins,
		PasswordHasher:   "bcrypt", // or "argon2id" (OWASP recommended)
		AuditLog: api.AuditLogConfig{
			Path:       s.config.Security.AuditLog.Path,
			MaxSizeMB:  s.config.Security.AuditLog.MaxSizeMB,
			MaxBackups: s.config.Security.AuditLog.MaxBackups,
			MaxAgeDays: s.config.Security.AuditLog.MaxAgeDays,
		},
		DataDir: s.config.Server.DataDir,
	}
	s.apiServer = api.NewServer(s.database, s.logger, apiCfg)
	// F5440/F5442: JMAP (started earlier) verifies API tokens with the API's
	// key set, so rotated keys and DisableLegacyJWT apply there too.
	if s.jmapServer != nil {
		s.jmapServer.SetKeyFunc(s.apiServer.JWTKeyFunc)
	}
	// F7770: share the store delivery reads, so a PUT /api/v1/vacation takes effect.
	if s.vacationMgr != nil {
		s.apiServer.SetVacationManager(s.vacationMgr)
	}
	s.apiServer.SetSearchService(s.searchSvc)
	s.apiServer.SetAccountPurger(accountPurger{srv: s}) // F6649c
	s.apiServer.SetQuotaAdjustFunc(s.quotaAdjust)       // F6680
	if s.pushSvc != nil {
		s.apiServer.SetPushService(push.NewAPIAdapter(s.pushSvc))
	}
	s.apiServer.SetTracingProvider(s.tracingProvider)
	if s.queue != nil {
		s.apiServer.SetQueueManager(s.queue)
	}
	// F7640: webmail sends take the same local/relay routing as SMTP
	// submission instead of queueing local recipients for an MX round trip.
	s.apiServer.SetDeliverFunc(func(from string, to []string, data []byte) error {
		return s.deliverMessageWithDSN(from, to, nil, nil, data)
	})
	// Set health monitor
	if s.healthMonitor != nil {
		s.apiServer.SetHealthMonitor(s.healthMonitor)
	}
	// Set mail database for email operations
	if s.storageDB != nil {
		s.apiServer.SetMailDB(s.storageDB)
	}
	// Set message store for email operations
	if s.msgStore != nil {
		s.apiServer.SetMsgStore(s.msgStore)
	}
	// Set backup manager for backup/restore operations
	if s.storageDB != nil {
		backupMgr := backup.NewManager(s.config.Server.DataDir, s.storageDB, s.msgStore)
		s.apiServer.SetBackupManager(backupMgr)
	}
	// Configure API rate limiting
	s.apiServer.SetAPIRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)

	apiLn, err := s.apiServer.Listen(apiCfg.Addr)
	if err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}
	// Enforce the TLS contract on the HTTP port: when the operator
	// explicitly configures TLS (cert_file/key_file, or ACME enabled — its
	// certificates are obtained at handshake time), the listener must speak
	// TLS. Without this the configured certs were ignored and the port
	// served plaintext. Nothing configured keeps the historical plaintext
	// behavior (and note: the manager's availability probe alone is NOT the
	// gate — GetCertificate returns nil-error for unconfigured managers).
	// Mirrors the IMAP/SMTP listener semantics for explicitly configured TLS.
	if (s.config.TLS.CertFile != "" && s.config.TLS.KeyFile != "") || s.config.TLS.ACME.Enabled {
		apiLn = tls.NewListener(apiLn, s.tlsManager.GetTLSConfig())
		s.logger.Info("API server serving TLS", "addr", apiCfg.Addr)
	} else {
		s.logger.Warn("No TLS certificate configured; API server serving plaintext", "addr", apiCfg.Addr)
	}
	apiServer := s.apiServer
	go func() {
		if err := apiServer.Serve(apiLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("API server error", "error", err)
		}
	}()
	s.logger.Info("API server started", "addr", apiCfg.Addr)

	// Start admin server on separate port (localhost only)
	if s.config.Admin.Enabled {
		adminCfg := api.AdminConfig{
			Addr:             fmt.Sprintf("%s:%d", s.config.Admin.Bind, s.config.Admin.Port),
			JWTSecret:        s.config.Security.JWTSecret,
			DisableLegacyJWT: s.config.Security.DisableLegacyJWT,
			AuditLog: api.AuditLogConfig{
				Path:       s.config.Security.AuditLog.Path,
				MaxSizeMB:  s.config.Security.AuditLog.MaxSizeMB,
				MaxBackups: s.config.Security.AuditLog.MaxBackups,
				MaxAgeDays: s.config.Security.AuditLog.MaxAgeDays,
			},
		}
		adminServer := api.NewAdminServer(s.apiServer, adminCfg)
		adminLn, err := adminServer.Listen()
		if err != nil {
			return fmt.Errorf("failed to start admin server: %w", err)
		}
		s.adminServer = adminServer
		go func() {
			if err := adminServer.Serve(adminLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.logger.Error("Admin API server error", "error", err)
			}
		}()
		s.logger.Info("Admin API server started", "addr", adminCfg.Addr)
	}
	return nil
}
