package server

import (
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
