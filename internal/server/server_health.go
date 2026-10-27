package server

import (
	"os"
	"path/filepath"

	"github.com/umailserver/umailserver/internal/health"
	"github.com/umailserver/umailserver/internal/queue"
)

// queueStatsAdapter wraps a *queue.Manager to satisfy the health.QueueStats interface.
type queueStatsAdapter struct {
	mgr *queue.Manager
}

func (a *queueStatsAdapter) GetStats() (health.QueueStatInfo, error) {
	stats, err := a.mgr.GetStats()
	if err != nil {
		return health.QueueStatInfo{}, err
	}
	return health.QueueStatInfo{
		Pending:  stats.Pending,
		Sending:  stats.Sending,
		Failed:   stats.Failed,
		Deferred: stats.Bounced, // Use bounced as deferred proxy
	}, nil
}

// setupHealthChecks registers the component checks behind /health and the
// MCP get_system_status tool. It needs the queue, so Start calls it once the
// queue manager exists.
func (s *Server) setupHealthChecks() {
	// Database health check
	s.healthMonitor.Register("database", health.DatabaseCheck(func() error {
		_, err := s.database.ListDomains()
		return err
	}))

	// Queue health check
	if s.queue != nil {
		// Wrap queue manager to match health.QueueStats interface
		queueStats := &queueStatsAdapter{mgr: s.queue}
		s.healthMonitor.Register("queue", health.QueueCheck(queueStats, 1000))
	}

	// Message store health check
	if s.msgStore != nil {
		// The store can take a new message: its directory accepts a file.
		dir := filepath.Join(s.config.Server.DataDir, "mail", "messages")
		s.healthMonitor.Register("storage", health.MessageStoreCheck(func() error {
			f, err := os.CreateTemp(dir, ".health-*")
			if err != nil {
				return err
			}
			name := f.Name()
			_ = f.Close()
			return os.Remove(name)
		}))
	}

	// TLS certificate health check
	if s.tlsManager != nil && s.config.TLS.CertFile != "" {
		s.healthMonitor.Register("tls_certificate", health.TLSCertificateCheck(
			s.config.TLS.CertFile,
			s.config.TLS.KeyFile,
			30, // warning at 30 days
			7,  // critical at 7 days
		))
	}

	// Disk space health check
	s.healthMonitor.Register("disk_space", health.DiskSpaceCheck(
		s.config.Server.DataDir,
		80, // warning at 80%
		95, // critical at 95%
	))

	s.logger.Info("Health checks configured")
}
