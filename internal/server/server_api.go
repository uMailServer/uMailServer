package server

import (
	"fmt"

	"github.com/umailserver/umailserver/internal/api"
)

// startAPI creates and starts the HTTP API server (webmail + admin).
func (s *Server) startAPI() {
	apiCfg := api.Config{
		Addr:           fmt.Sprintf("%s:%d", s.config.HTTP.Bind, s.config.HTTP.Port),
		JWTSecret:      s.config.Security.JWTSecret,
		CorsOrigins:    s.config.HTTP.CorsOrigins,
		PasswordHasher: "bcrypt", // or "argon2id" (OWASP recommended)
		AuditLog: api.AuditLogConfig{
			Path:       s.config.Security.AuditLog.Path,
			MaxSizeMB:  s.config.Security.AuditLog.MaxSizeMB,
			MaxBackups: s.config.Security.AuditLog.MaxBackups,
			MaxAgeDays: s.config.Security.AuditLog.MaxAgeDays,
		},
	}
	s.apiServer = api.NewServer(s.database, s.logger, apiCfg)
	s.apiServer.SetSearchService(s.searchSvc)
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
	// Configure API rate limiting
	s.apiServer.SetAPIRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)

	go func() {
		if err := s.apiServer.Start(apiCfg.Addr); err != nil {
			s.logger.Error("API server error", "error", err)
		}
	}()
	s.logger.Info("API server started", "addr", apiCfg.Addr)

	// Start admin server on separate port (localhost only)
	if s.config.Admin.Enabled {
		adminCfg := api.AdminConfig{
			Addr:      fmt.Sprintf("%s:%d", s.config.Admin.Bind, s.config.Admin.Port),
			JWTSecret: s.config.Security.JWTSecret,
			AuditLog: api.AuditLogConfig{
				Path:       s.config.Security.AuditLog.Path,
				MaxSizeMB:  s.config.Security.AuditLog.MaxSizeMB,
				MaxBackups: s.config.Security.AuditLog.MaxBackups,
				MaxAgeDays: s.config.Security.AuditLog.MaxAgeDays,
			},
		}
		s.adminServer = api.NewAdminServer(s.apiServer, adminCfg)

		go func() {
			if err := s.adminServer.Start(); err != nil {
				s.logger.Error("Admin API server error", "error", err)
			}
		}()
		s.logger.Info("Admin API server started", "addr", adminCfg.Addr)
	}
}
