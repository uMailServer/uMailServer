package server

import (
	"fmt"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/spam"
)

// startSMTP creates and starts the inbound SMTP server with the message
// processing pipeline, plus the optional submission (587) and
// submission-TLS (465) servers.
func (s *Server) startSMTP() {
	smtpAddr := fmt.Sprintf("%s:%d", s.config.SMTP.Inbound.Bind, s.config.SMTP.Inbound.Port)
	smtpCfg := &smtp.Config{
		Hostname:       s.config.Server.Hostname,
		MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
		MaxConnections: s.config.SMTP.Inbound.MaxConnections,
		ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
		WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
		TLSConfig:      s.tlsManager.GetTLSConfig(),
	}

	smtpServer := smtp.NewServer(smtpCfg, s.logger)
	smtpServer.SetAuthHandler(s.authenticate)
	smtpServer.SetDeliveryHandlerWithSieve(s.deliverMessageWithSieve)
	smtpServer.SetUserSecretHandler(s.getUserSecret)
	smtpServer.SetLoginResultHandler(s.loginResult)
	smtpServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))

	// Wire up the message processing pipeline
	pipeline := smtp.NewPipeline(smtp.NewPipelineLogger(s.logger))

	// Create DNS resolver for auth checks
	resolver := smtp.NewNetDNSResolver()

	// Auth pipeline stages (SPF, DKIM, DMARC, ARC)
	spfChecker := auth.NewSPFChecker(resolver)
	dkimVerifier := auth.NewDKIMVerifier(resolver)
	dmarcEvaluator := auth.NewDMARCEvaluator(resolver)
	arcValidator := auth.NewARCValidator(resolver)

	dmarcStage := smtp.NewAuthDMARCStage(dmarcEvaluator, s.logger)

	// Wire DMARC reporter if enabled
	if s.config.DMARC.Enabled && s.config.DMARC.ReportEmail != "" {
		dmarcReporterConfig := auth.DMARCReporterConfig{
			OrgName:     s.config.DMARC.OrgName,
			FromEmail:   s.config.DMARC.FromEmail,
			ReportEmail: s.config.DMARC.ReportEmail,
			Interval:    24 * time.Hour, // Default to 24h
		}
		dmarcReporter := auth.NewDMARCReporter(resolver, s.logger, dmarcReporterConfig)
		dmarcStage.SetReporter(dmarcReporter)
		s.logger.Info("DMARC reporting enabled", "org", s.config.DMARC.OrgName)
	}

	pipeline.AddStage(smtp.NewAuthSPFStage(spfChecker, s.logger))
	pipeline.AddStage(smtp.NewAuthDKIMStage(dkimVerifier, s.logger))
	pipeline.AddStage(dmarcStage)
	pipeline.AddStage(smtp.NewAuthARCStage(arcValidator, s.logger))

	// Rate limiting stage (uses per-IP and per-user limits)
	if s.rateLimiter != nil {
		pipeline.AddStage(smtp.NewRateLimitStage(s.rateLimiter))
	}

	// Spam filtering stages
	if s.config.Spam.Greylisting.Enabled {
		pipeline.AddStage(smtp.NewGreylistStage())
	}
	if len(s.config.Spam.RBLServers) > 0 {
		pipeline.AddStage(smtp.NewRBLStage(s.config.Spam.RBLServers, smtp.NewRealRBLDNSResolver()))
	}
	pipeline.AddStage(smtp.NewHeuristicStage())

	// Bayesian spam classification (if storage available)
	if s.storageDB != nil {
		classifier := spam.NewClassifier(s.storageDB.Bolt())
		if err := classifier.Initialize(); err != nil {
			s.logger.Error("failed to initialize Bayesian classifier", "error", err)
		} else {
			pipeline.AddStage(smtp.NewBayesianStage(classifier))
		}
	}

	pipeline.AddStage(smtp.NewScoreStage(s.config.Spam.RejectThreshold, s.config.Spam.JunkThreshold))

	// Sieve mail filtering (if sieve manager available)
	if s.sieveManager != nil {
		sieveStage := smtp.NewSieveStage(s.sieveManager)
		sieveStage.SetVacationHandler(s.handleSieveVacation)
		pipeline.AddStage(sieveStage)
	}

	// S/MIME processing stage
	pipeline.AddStage(smtp.NewSMIMEStage(s.smimeKeystore))

	// OpenPGP processing stage
	pipeline.AddStage(smtp.NewOpenPGPStage(s.openpgpKeystore))

	// Antivirus scanning stage
	if s.config.AV.Enabled {
		avScanner := av.NewScanner(av.Config{
			Enabled: s.config.AV.Enabled,
			Addr:    s.config.AV.Addr,
			Timeout: s.config.AV.Timeout.ToDuration(),
			Action:  s.config.AV.Action,
		})
		pipeline.AddStage(smtp.NewAVStage(&avScannerAdapter{inner: avScanner}, s.config.AV.Action))
	}

	smtpServer.SetPipeline(pipeline)

	go func() {
		if err := smtpServer.ListenAndServe(smtpAddr); err != nil {
			s.logger.Error("SMTP server error", "error", err)
		}
	}()
	s.smtpServer = smtpServer
	s.logger.Info("SMTP server started", "addr", smtpAddr)

	// Submission SMTP server (port 587, STARTTLS)
	if s.config.SMTP.Submission.Enabled {
		submissionAddr := fmt.Sprintf("%s:%d", s.config.SMTP.Submission.Bind, s.config.SMTP.Submission.Port)
		submissionCfg := &smtp.Config{
			Hostname:       s.config.Server.Hostname,
			MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
			MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
			MaxConnections: s.config.SMTP.Submission.MaxConnections,
			ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
			WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
			TLSConfig:      s.tlsManager.GetTLSConfig(),
			RequireAuth:    true,
			RequireTLS:     true,
			IsSubmission:   true,
		}

		submissionServer := smtp.NewServer(submissionCfg, s.logger)
		submissionServer.SetAuthHandler(s.authenticate)
		submissionServer.SetDeliveryHandlerWithSieve(s.deliverMessageWithSieve)
		submissionServer.SetUserSecretHandler(s.getUserSecret)
		submissionServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))

		go func() {
			if err := submissionServer.ListenAndServe(submissionAddr); err != nil {
				s.logger.Error("Submission server error", "error", err)
			}
		}()
		s.submissionServer = submissionServer
		s.logger.Info("Submission server started", "addr", submissionAddr)
	}

	// Submission TLS SMTP server (port 465, implicit TLS)
	if s.config.SMTP.SubmissionTLS.Enabled {
		submissionTLSAddr := fmt.Sprintf("%s:%d", s.config.SMTP.SubmissionTLS.Bind, s.config.SMTP.SubmissionTLS.Port)
		submissionTLSCfg := &smtp.Config{
			Hostname:       s.config.Server.Hostname,
			MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
			MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
			MaxConnections: s.config.SMTP.SubmissionTLS.MaxConnections,
			ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
			WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
			TLSConfig:      s.tlsManager.GetTLSConfig(),
			RequireAuth:    true,
			RequireTLS:     false, // Already on TLS
			IsSubmission:   true,
		}

		submissionTLSServer := smtp.NewServer(submissionTLSCfg, s.logger)
		submissionTLSServer.SetAuthHandler(s.authenticate)
		submissionTLSServer.SetDeliveryHandlerWithSieve(s.deliverMessageWithSieve)
		submissionTLSServer.SetUserSecretHandler(s.getUserSecret)
		submissionTLSServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))

		tlsConfig := s.tlsManager.GetTLSConfig()
		go func() {
			if err := submissionTLSServer.ListenAndServeTLS(submissionTLSAddr, tlsConfig); err != nil {
				s.logger.Error("Submission TLS server error", "error", err)
			}
		}()
		s.submissionTLSServer = submissionTLSServer
		s.logger.Info("Submission TLS server started", "addr", submissionTLSAddr)
	}
}
