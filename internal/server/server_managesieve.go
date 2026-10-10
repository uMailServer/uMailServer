package server

import (
	"fmt"
	"net"
	"strconv"

	"github.com/umailserver/umailserver/internal/sieve"
)

// startManageSieve creates and starts the ManageSieve server on port 4190
func (s *Server) startManageSieve() error {
	if !s.config.ManageSieve.Enabled {
		return nil
	}

	addr, tlsAddr := manageSieveAddrs(s.config.ManageSieve.Bind, s.config.ManageSieve.Port)
	tlsCfg := s.tlsManager.GetTLSConfig()

	sieveServer := sieve.NewManageSieveServer(s.sieveManager, tlsCfg)
	// F5116: the configured bind/port were only logged; the listener always
	// bound the package default 0.0.0.0:4190 (and 0.0.0.0:4191 for TLS).
	sieveServer.SetListenAddrs(addr, tlsAddr)
	// Set auth handler for ManageSieve (uses same auth as submission SMTP)
	sieveServer.SetAuthHandler(func(user, pass string) bool {
		ok, _ := s.authenticate(user, pass)
		return ok
	})
	sieveServer.SetTracingProvider(s.tracingProvider)
	if err := sieveServer.Listen(); err != nil {
		// Listen may fail after the plain listener is already serving
		// (TLS bind error); close it so it is not left running unowned.
		_ = sieveServer.Close()
		return fmt.Errorf("failed to start ManageSieve server: %w", err)
	}

	s.manageSieveServer = sieveServer
	s.logger.Info("ManageSieve server started", "addr", addr, "tls_addr", tlsAddr)
	return nil
}

// manageSieveAddrs returns the plain and implicit-TLS listen addresses for
// the configured bind host and port. The TLS listener takes the next port
// (4191 for the default 4190), or an ephemeral one when port is 0.
func manageSieveAddrs(bind string, port int) (addr, tlsAddr string) {
	tlsPort := 0
	if port > 0 && port < 65535 {
		tlsPort = port + 1
	}
	return net.JoinHostPort(bind, strconv.Itoa(port)), net.JoinHostPort(bind, strconv.Itoa(tlsPort))
}
