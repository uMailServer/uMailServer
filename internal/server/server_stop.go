package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/umailserver/umailserver/internal/autoconfig"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Stop gracefully stops all server components
func (s *Server) Stop() error {
	s.logger.Info("Stopping uMailServer...")

	// Remove PID file, but only when it records this process (F4916): a
	// Server that never started, or whose Start lost the PID file to another
	// live instance, must not delete that instance's PID file.
	pidFile := NewPIDFile(s.config.Server.DataDir)
	if pid, err := pidFile.Read(); err == nil && pid == os.Getpid() {
		if err := pidFile.Remove(); err != nil {
			s.logger.Debug("Failed to remove PID file", "error", err)
		}
	}

	// Signal cancellation
	s.cancel()

	// F6592: stop accepting first, so sessions that are already past the
	// pipeline can still register their delivery before the gate closes
	// (otherwise they were answered 451 by a gate that closed ahead of the
	// listeners). The gate then fences stragglers and drains in-flight ones.
	// Stop SMTP server
	if s.smtpServer != nil {
		if err := s.smtpServer.Stop(); err != nil {
			s.logger.Error("Failed to stop SMTP server", "error", err)
		}
	}

	// Stop submission SMTP servers
	if s.submissionServer != nil {
		if err := s.submissionServer.Stop(); err != nil {
			s.logger.Error("Failed to stop submission server", "error", err)
		}
	}
	if s.submissionTLSServer != nil {
		if err := s.submissionTLSServer.Stop(); err != nil {
			s.logger.Error("Failed to stop submission TLS server", "error", err)
		}
	}

	// Refuse new deliveries and wait for those in flight (F4976): SMTP
	// sessions outlive smtp.Server.Stop, and a delivery finishing after
	// indexWork or the databases are closed panics or loses the message.
	s.deliveryMu.Lock()
	s.deliveryClosed = true
	s.deliveryMu.Unlock()
	s.deliveries.Wait()
	// Push/vacation goroutines started by those deliveries use the queue and
	// databases closed below; wait for them (bounded, push may hit the network).
	s.waitBackground(10 * time.Second)

	// Close search indexing work queue to drain workers (once only)
	s.stopOnce.Do(func() { close(s.indexWork) })

	// Stop JMAP background work (upload GC) and webhook retries (F6280, F6294).
	if s.jmapServer != nil {
		s.jmapServer.Stop()
	}
	if s.webhookMgr != nil {
		s.webhookMgr.Stop()
	}

	// Stop IMAP server
	if s.imapServer != nil {
		if err := s.imapServer.Stop(); err != nil {
			s.logger.Error("Failed to stop IMAP server", "error", err)
		}
	}

	// Stop POP3 server
	if s.pop3Server != nil {
		if err := s.pop3Server.Stop(); err != nil {
			s.logger.Error("Failed to stop POP3 server", "error", err)
		}
	}

	// Stop MCP server
	if s.mcpHTTPServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.mcpHTTPServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Failed to stop MCP server", "error", err)
		}
		shutdownCancel()
	}

	// Stop ManageSieve server
	if s.manageSieveServer != nil {
		if err := s.manageSieveServer.Close(); err != nil {
			s.logger.Error("Failed to stop ManageSieve server", "error", err)
		}
	}

	// Stop CalDAV server
	if s.caldavHTTPServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.caldavHTTPServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Failed to stop CalDAV server", "error", err)
		}
		shutdownCancel()
		s.logger.Debug("CalDAV server stopped")
	}

	// Stop CardDAV server
	if s.carddavHTTPServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.carddavHTTPServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Failed to stop CardDAV server", "error", err)
		}
		shutdownCancel()
		s.logger.Debug("CardDAV server stopped")
	}

	// Stop JMAP server
	if s.jmapHTTPServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.jmapHTTPServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Failed to stop JMAP server", "error", err)
		}
		shutdownCancel()
		s.logger.Debug("JMAP server stopped")
	}

	// Stop API server
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			s.logger.Error("Failed to stop API server", "error", err)
		}
	}

	// Stop Prometheus metrics server
	s.stopMetrics(context.Background())

	// Stop admin server
	if s.adminServer != nil {
		if err := s.adminServer.Stop(); err != nil {
			s.logger.Error("Failed to stop admin server", "error", err)
		}
	}

	// Stop queue manager
	if s.queue != nil {
		s.queue.Stop()
	}

	// Stop rate limiter cleanup goroutine
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}

	// Wait for search index workers to drain before closing databases
	s.wg.Wait()

	// Close message store
	if s.msgStore != nil {
		_ = s.msgStore.Close()
	}

	// Close mailstore (IMAP bbolt database)
	if s.mailstore != nil {
		_ = s.mailstore.Close()
	}

	// Close database
	if s.database != nil {
		_ = s.database.Close()
	}

	// Close storage database
	if s.storageDB != nil {
		_ = s.storageDB.Close()
	}

	// Stop tracing provider
	if s.tracingProvider != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.tracingProvider.Stop(shutdownCtx); err != nil {
			s.logger.Error("Failed to stop tracing provider", "error", err)
		}
		shutdownCancel()
	}

	s.logger.Info("uMailServer stopped")
	return nil
}

// Wait waits for shutdown signal
func (s *Server) Wait() error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	s.logger.Info("Received signal", "signal", sig)

	return s.Stop()
}

// waitBackground waits up to d for the delivery-spawned background goroutines.
func (s *Server) waitBackground(d time.Duration) {
	done := make(chan struct{})
	go func() { s.bgWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		s.logger.Warn("Background tasks still running at shutdown")
	}
}

// ReloadTLS is the SIGHUP hook (F6594). It re-reads the configured manual
// certificate/key pair and reports an error if it is unusable, leaving the
// served certificate untouched in that case. The TLS manager already swaps a
// changed cert/key file on the next handshake (it stamps the files), so a
// valid pair takes effect without restarting listeners. Nothing else (config,
// listeners, ACME issuance) is reloaded.
func (s *Server) ReloadTLS() error {
	if s.tlsManager == nil || s.config == nil {
		return nil
	}
	certFile, keyFile := s.config.TLS.CertFile, s.config.TLS.KeyFile
	if certFile == "" || keyFile == "" {
		s.logger.Info("TLS reload: no manual certificate configured; nothing to reload")
		return nil
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		s.logger.Error("TLS reload: certificate pair is invalid; keeping the previous one", "error", err)
		return fmt.Errorf("tls reload: %w", err)
	}
	s.logger.Info("TLS reload: certificate pair valid; new handshakes use it", "cert", certFile)
	return nil
}

// IsHostedDomain reports whether domain is one of the domains in the accounts
// database (case-insensitive, trailing dot ignored) (F6590).
func (s *Server) IsHostedDomain(domain string) bool {
	domain = strings.TrimRight(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || s.database == nil {
		return false
	}
	d, err := s.database.GetDomain(domain)
	return err == nil && d != nil
}

// AutoconfigProvider returns the Autoconfig/Autodiscover provider restricted
// to hosted domains; wire it into autoconfig.NewHandler (F6590).
func (s *Server) AutoconfigProvider() autoconfig.ConfigProvider {
	return autoconfig.NewHostedProvider(nil, s.IsHostedDomain)
}
