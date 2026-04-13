package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Stop gracefully stops all server components
func (s *Server) Stop() error {
	s.logger.Info("Stopping uMailServer...")

	// Remove PID file
	pidFile := NewPIDFile(s.config.Server.DataDir)
	if err := pidFile.Remove(); err != nil {
		s.logger.Debug("Failed to remove PID file", "error", err)
	}

	// Signal cancellation
	s.cancel()

	// Close search indexing work queue to drain workers (once only)
	s.stopOnce.Do(func() { close(s.indexWork) })

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
