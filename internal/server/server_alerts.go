package server

import (
	"time"

	"github.com/umailserver/umailserver/internal/alert"
	"github.com/umailserver/umailserver/internal/config"
)

// buildAlertConfig converts the YAML-facing config.AlertConfig into the
// internal alert.Config consumed by the alert manager. It applies defaults
// for fields the user left zero so behaviour matches alert.DefaultConfig().
func buildAlertConfig(c config.AlertConfig) alert.Config {
	defaults := alert.DefaultConfig()
	out := alert.Config{
		Enabled:         c.Enabled,
		WebhookURL:      c.WebhookURL,
		WebhookHeaders:  c.WebhookHeaders,
		WebhookTemplate: c.WebhookTemplate,
		SMTPServer:      c.SMTPServer,
		SMTPPort:        c.SMTPPort,
		SMTPUsername:    c.SMTPUsername,
		SMTPPassword:    alert.SecureString(c.SMTPPassword),
		FromAddress:     c.FromAddress,
		ToAddresses:     c.ToAddresses,
		UseTLS:          c.UseTLS,
		MinInterval:     time.Duration(c.MinInterval),
		MaxAlerts:       c.MaxAlerts,
		DiskThreshold:   c.DiskThreshold,
		MemoryThreshold: c.MemoryThreshold,
		ErrorThreshold:  c.ErrorThreshold,
		TLSWarningDays:  c.TLSWarningDays,
		QueueThreshold:  c.QueueThreshold,
	}
	if out.MinInterval == 0 {
		out.MinInterval = defaults.MinInterval
	}
	if out.MaxAlerts == 0 {
		out.MaxAlerts = defaults.MaxAlerts
	}
	if out.DiskThreshold == 0 {
		out.DiskThreshold = defaults.DiskThreshold
	}
	if out.MemoryThreshold == 0 {
		out.MemoryThreshold = defaults.MemoryThreshold
	}
	if out.ErrorThreshold == 0 {
		out.ErrorThreshold = defaults.ErrorThreshold
	}
	if out.TLSWarningDays == 0 {
		out.TLSWarningDays = defaults.TLSWarningDays
	}
	if out.QueueThreshold == 0 {
		out.QueueThreshold = defaults.QueueThreshold
	}
	return out
}

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
