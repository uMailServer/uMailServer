package server

import (
	"time"

	"github.com/umailserver/umailserver/internal/auth"
)

// startDMARCReporter starts sending DMARC aggregate reports (RFC 7489 §7.2)
// for the inbound mail it is given, every dmarc.interval until shutdown.
// Reports go out through the normal delivery path, so remote rua=
// addresses are reached through the queue. It returns nil when reporting is
// off or dmarc.from_email is unset.
func (s *Server) startDMARCReporter(resolver auth.DNSResolver) *auth.DMARCReporter {
	cfg := s.config.DMARC
	if !cfg.Enabled {
		return nil
	}
	if cfg.FromEmail == "" {
		s.logger.Warn("DMARC reporting disabled: dmarc.from_email is not set")
		return nil
	}
	interval := s.dmarcReportInterval()
	reporter := auth.NewDMARCReporter(resolver, s.logger, auth.DMARCReporterConfig{
		OrgName:     cfg.OrgName,
		FromEmail:   cfg.FromEmail,
		ReportEmail: cfg.ReportEmail,
		Receiver:    s.config.Server.Hostname,
		Interval:    interval,
		Send: func(from string, to []string, msg []byte) error {
			return s.deliverMessageWithDSN(from, to, nil, nil, msg)
		},
	})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		reporter.Run(s.ctx)
	}()
	s.logger.Info("DMARC aggregate reporting enabled", "org", cfg.OrgName, "interval", interval)
	return reporter
}

// dmarcReportInterval is dmarc.interval, or the RFC 7489 default of 24h when
// it is unset, unparsable or shorter than an hour.
func (s *Server) dmarcReportInterval() time.Duration {
	v := s.config.DMARC.Interval
	if v == "" {
		return 24 * time.Hour
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Hour {
		s.logger.Warn("invalid dmarc.interval, using 24h", "interval", v)
		return 24 * time.Hour
	}
	return d
}
