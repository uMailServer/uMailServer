package server

import (
	"fmt"
	"path/filepath"

	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/queue"
)

// Start starts all server components
func (s *Server) Start() error {
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

	// Initialize queue manager
	queueDir := filepath.Join(s.config.Server.DataDir, "queue")
	s.queue = queue.NewManager(s.database, nil, queueDir, s.logger)
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

	s.startSMTP()

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

	s.startMCP()
	s.startManageSieve()
	s.startCalDAV()
	s.startCardDAV()
	s.startJMAP()
	s.startAPI()

	return nil
}
