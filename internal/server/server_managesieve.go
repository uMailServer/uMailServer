package server

import (
	"fmt"

	"github.com/umailserver/umailserver/internal/sieve"
)

// startManageSieve creates and starts the ManageSieve server on port 4190
func (s *Server) startManageSieve() {
	if !s.config.ManageSieve.Enabled {
		return
	}

	addr := fmt.Sprintf("%s:%d", s.config.ManageSieve.Bind, s.config.ManageSieve.Port)
	tlsCfg := s.tlsManager.GetTLSConfig()

	sieveServer := sieve.NewManageSieveServer(s.sieveManager, tlsCfg)
	// Set auth handler for ManageSieve (uses same auth as submission SMTP)
	sieveServer.SetAuthHandler(func(user, pass string) bool {
		ok, _ := s.authenticate(user, pass)
		return ok
	})
	if err := sieveServer.Listen(); err != nil {
		s.logger.Error("Failed to start ManageSieve server", "error", err)
		return
	}

	s.manageSieveServer = sieveServer
	s.logger.Info("ManageSieve server started", "addr", addr)
}
