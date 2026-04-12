package server

import (
	"time"
)

// startAlertChecker runs periodic health checks for alerting (TLS expiry, queue backlog)
func (s *Server) startAlertChecker() {
	if s.alertMgr == nil || !s.alertMgr.IsEnabled() {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Check every 10 minutes
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.checkAlerts()
			}
		}
	}()
}

// checkAlerts performs periodic alert checks
func (s *Server) checkAlerts() {
	if s.alertMgr == nil || !s.alertMgr.IsEnabled() {
		return
	}

	// Check queue backlog
	if s.queue != nil {
		stats, err := s.queue.GetStats()
		if err == nil {
			s.alertMgr.CheckQueueBacklog(stats.Pending)
		}
	}

	// Check TLS certificate expiry
	if s.tlsManager != nil {
		statuses := s.tlsManager.GetCertificateStatus()
		for _, status := range statuses {
			if status.Valid {
				daysUntil := int(time.Until(status.ExpiresAt).Hours() / 24)
				s.alertMgr.CheckTLSCertificate(status.Domain, daysUntil)
			}
		}
	}
}
