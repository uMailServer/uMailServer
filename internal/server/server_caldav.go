package server

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/umailserver/umailserver/internal/caldav"
)

// startCalDAV creates and starts the CalDAV server
func (s *Server) startCalDAV() {
	if !s.config.CalDAV.Enabled {
		return
	}

	addr := fmt.Sprintf("%s:%d", s.config.CalDAV.Bind, s.config.CalDAV.Port)
	caldavDataDir := filepath.Join(s.config.Server.DataDir, "caldav")

	caldavServer := caldav.NewServer(caldavDataDir, s.logger)
	// Set auth handler - use same auth as submission SMTP
	caldavServer.SetAuthFunc(func(user, pass string) (bool, error) {
		ok, err := s.authenticate(user, pass)
		return ok, err
	})

	s.caldavServer = caldavServer

	srv := &http.Server{
		Addr:              addr,
		Handler:           caldavServer,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.caldavHTTPServer = srv

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("CalDAV server error", "error", err)
		}
	}()

	s.logger.Info("CalDAV server started", "addr", addr)
}
