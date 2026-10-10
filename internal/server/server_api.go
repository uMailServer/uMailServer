package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/backup"
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
	s.apiServer.SetSearchService(s.searchSvc)
	s.apiServer.SetTracingProvider(s.tracingProvider)
	if s.queue != nil {
		s.apiServer.SetQueueManager(s.queue)
	}
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
