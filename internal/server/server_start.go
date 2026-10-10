package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/queue"
)

// Start starts all server components
func (s *Server) Start() (err error) {
	s.logger.Info("Starting uMailServer",
		"hostname", s.config.Server.Hostname,
		"data_dir", s.config.Server.DataDir,
	)

	// Create PID file
	pidFile := NewPIDFile(s.config.Server.DataDir)
	if err := pidFile.Create(); err != nil {
		return fmt.Errorf("failed to create PID file: %w", err)
	}
	s.logger.Debug("PID file created")

	// F4915: a failure past this point must not leave the PID file, the
	// queue, SMTP listeners or background goroutines running. Roll back
	// everything started so far; Stop is safe on a partially started server.
	defer func() {
		if err != nil {
			s.logger.Error("Start failed; rolling back started components", "error", err)
			_ = s.Stop()
		}
	}()

	// Initialize queue manager
	queueDir := filepath.Join(s.config.Server.DataDir, "queue")
	if err := os.MkdirAll(queueDir, 0o750); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}
	s.queue = queue.NewManager(s.database, nil, queueDir, s.logger)
	s.queue.SetTracingProvider(s.tracingProvider)
	s.queue.Start(s.ctx)
	s.logger.Info("Queue manager started")

	// Wire webhook manager to queue for delivery events
	if s.webhookMgr != nil {
		s.queue.SetWebhookTrigger(s.webhookMgr)
	}

	// Create mailstore for IMAP using shared storage
	s.mailstore = imap.NewBboltMailstoreWithInterfaces(s.storageDB, s.msgStore)

	// Set MDN handler for read receipts
	s.mailstore.SetMDNHandler(s.sendMDN)

	// F5530: SMTP (25/587/465), JMAP and the HTTP API/admin listeners bind
	// synchronously too; a bind failure fails Start and is rolled back.
	if err := s.startSMTP(); err != nil {
		return err
	}

	// Start search indexing worker pool
	if s.searchSvc != nil {
		for i := 0; i < 10; i++ {
			s.wg.Add(1)
			go s.runIndexWorker()
		}
	}

	// Start vacation reply cleanup goroutine (time-based, runs hourly)
	s.startVacationCleanup()

	// Start alert checker goroutine (periodic health checks for alerting)
	s.startAlertChecker()

	if err := s.startIMAP(s.mailstore); err != nil {
		return err
	}

	if err := s.startPOP3(s.mailstore); err != nil {
		return err
	}

	// F5430: an enabled optional listener that cannot bind fails Start, as
	// IMAP/POP3 do, instead of being logged while Start reports success.
	if err := s.startMCP(); err != nil {
		return err
	}
	if err := s.startManageSieve(); err != nil {
		return err
	}
	if err := s.startCalDAV(); err != nil {
		return err
	}
	if err := s.startCardDAV(); err != nil {
		return err
	}
	// startJMAP runs before startAPI: startAPI installs the API's JWT key
	// resolver on the JMAP server (F5440).
	if err := s.startJMAP(); err != nil {
		return err
	}
	if err := s.startAPI(); err != nil {
		return err
	}
	if err := s.startMetrics(); err != nil {
		return err
	}

	return nil
}

// serveHTTP binds srv.Addr synchronously, so a bind failure reaches the
// caller (F5430), and then serves srv in the background. Errors after the
// bind are logged; Shutdown ends Serve with http.ErrServerClosed.
func (s *Server) serveHTTP(name string, srv *http.Server) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to start %s server: %w", name, err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error(name+" server error", "error", err)
		}
	}()
	return nil
}
