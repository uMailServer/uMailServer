package server

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/spam"
)

// startSMTP starts the configured SMTP listeners: the inbound MX server,
// plus the optional submission (587) and submission-TLS (465) servers.
// Each honours its own Enabled toggle. A listener that cannot bind fails
// Start (F5530) instead of only being logged.
func (s *Server) startSMTP() error {
	if s.config.SMTP.Inbound.Enabled {
		if err := s.startInboundSMTP(); err != nil {
			return err
		}
	} else {
		s.logger.Info("Inbound SMTP disabled; skipping listener")
	}

	// Submission SMTP server (port 587, STARTTLS)
	if s.config.SMTP.Submission.Enabled {
		if err := s.startSubmissionSMTP(); err != nil {
			return err
		}
	}

	// Submission TLS SMTP server (port 465, implicit TLS)
	if s.config.SMTP.SubmissionTLS.Enabled {
		if err := s.startSubmissionTLSSMTP(); err != nil {
			return err
		}
	}
	return nil
}

// smtpTLSConfig returns the TLS config for the SMTP servers, or nil when no
// certificate can be obtained (no usable cert_file/key_file and ACME off).
// A config whose GetCertificate always fails would make smtp advertise
// STARTTLS and then fail every handshake; nil keeps it from claiming TLS.
func (s *Server) smtpTLSConfig() *tls.Config {
	if err := s.imapCertificateError(); err != nil {
		s.logger.Warn("No TLS certificate available; SMTP will not offer STARTTLS", "error", err)
		return nil
	}
	return s.tlsManager.GetTLSConfig()
}

// serveSMTP binds addr synchronously, so a bind failure reaches the caller
// (F5530), and then serves srv in the background. A non-nil tlsConfig
// selects implicit TLS. smtp.Server.Serve closes the listener when it
// returns, including when Stop ran before it started.
func (s *Server) serveSMTP(name string, srv *smtp.Server, addr string, tlsConfig *tls.Config) error {
	var ln net.Listener
	var err error
	if tlsConfig != nil {
		ln, err = tls.Listen("tcp", addr, tlsConfig)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("failed to start %s server: %w", name, err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil {
			s.logger.Error(name+" server error", "error", err)
		}
	}()
	return nil
}

// startInboundSMTP creates and starts the inbound SMTP server with the
// message processing pipeline.
func (s *Server) startInboundSMTP() error {
	smtpAddr := fmt.Sprintf("%s:%d", s.config.SMTP.Inbound.Bind, s.config.SMTP.Inbound.Port)
	smtpCfg := &smtp.Config{
		Hostname:       s.config.Server.Hostname,
		MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
		MaxConnections: s.config.SMTP.Inbound.MaxConnections,
		ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
		WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
		TLSConfig:      s.smtpTLSConfig(),
	}

	smtpServer := smtp.NewServer(smtpCfg, s.logger)
	smtpServer.SetAuthHandler(s.authenticate)
	// Refuse relaying to foreign domains at RCPT time (F6042).
	smtpServer.SetLocalDomainHandler(s.isLocalDomain)
	// Inbound mail honours the pipeline's spam verdict (F4975).
	smtpServer.SetDeliveryHandlerWithNotify(s.deliverInboundWithNotify)
	// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken (CVE-2022-37454, etc.)
	// smtpServer.SetUserSecretHandler(s.getUserSecret)
	smtpServer.SetLoginResultHandler(s.protoLoginHandler("smtp"))
	smtpServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	smtpServer.SetTracingProvider(s.tracingProvider)

	// Wire up the message processing pipeline
	pipeline := smtp.NewPipeline(smtp.NewPipelineLogger(s.logger))
	pipeline.SetTracingProvider(s.tracingProvider)

	// Create DNS resolver for auth checks
	resolver := smtp.NewNetDNSResolver()

	// Auth pipeline stages (SPF, DKIM, DMARC, ARC)
	spfChecker := auth.NewSPFChecker(resolver)
	if ttl := s.config.Security.SPFCacheTTL.ToDuration(); ttl > 0 {
		spfChecker.SetCacheTTL(ttl)
	}
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

	// Sender-supplied X-Spam-* headers are neutralised before any stage
	// runs, so only the verdict this server adds can file mail into Junk
	// (F4975).
	pipeline.AddStage(spamHeaderGuardStage{})
	// Relay policy first: an unauthenticated client on the MX port may only
	// send to local domains (F4880).
	pipeline.AddStage(&relayPolicyStage{isLocalDomain: s.isLocalDomain})
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
		pipeline.AddStage(smtp.NewSieveStage(s.sieveManager))
	}

	// Antivirus scanning stage
	if s.config.AV.Enabled {
		avScanner := av.NewScanner(av.Config{
			Enabled: s.config.AV.Enabled,
			Addr:    s.config.AV.Addr,
			Timeout: s.config.AV.Timeout.ToDuration(),
			Action:  s.config.AV.Action,
		})
		pipeline.AddStage(&avFailClosedStage{scanner: avScanner, action: s.config.AV.Action, logger: s.logger})
	}

	smtpServer.SetPipeline(pipeline)

	if err := s.serveSMTP("SMTP", smtpServer, smtpAddr, nil); err != nil {
		return err
	}
	s.smtpServer = smtpServer
	s.logger.Info("SMTP server started", "addr", smtpAddr)
	return nil
}

// relayPolicyStage refuses to relay for unauthenticated clients: without
// it the inbound MX server queued mail for any external recipient, making
// the server an open relay (F4880). Authenticated sessions may still relay.
type relayPolicyStage struct {
	isLocalDomain func(domain string) bool
}

func (r *relayPolicyStage) Name() string { return "RelayPolicy" }

func (r *relayPolicyStage) Process(ctx *smtp.MessageContext) smtp.PipelineResult {
	if ctx.Authenticated {
		return smtp.ResultAccept
	}
	for _, rcpt := range ctx.To {
		_, domain := parseEmail(rcpt)
		if domain == "" || !r.isLocalDomain(domain) {
			ctx.Rejected = true
			ctx.RejectionCode = 550
			ctx.RejectionMessage = "5.7.1 Relaying denied"
			return smtp.ResultReject
		}
	}
	return smtp.ResultAccept
}

// spamHeaderGuardStage renames every X-Spam-* header the client sent to
// X-Orig-*, so that a sender cannot forge the spam verdict that
// deliverInboundWithNotify routes on (F4975). The rename is done in place,
// keeping the length: the session prepends its own headers to the same
// message bytes it handed the pipeline. Parsed copies are dropped from
// ctx.Headers so later stages (Sieve) do not see the forged values either.
type spamHeaderGuardStage struct{}

func (spamHeaderGuardStage) Name() string { return "SpamHeaderGuard" }

func (spamHeaderGuardStage) Process(ctx *smtp.MessageContext) smtp.PipelineResult {
	forEachHeaderField(ctx.Data, func(name string, start, _ int) {
		if isSpamHeaderName(name) {
			copy(ctx.Data[start:], "X-Orig-")
		}
	})
	for k := range ctx.Headers {
		if isSpamHeaderName(k) {
			delete(ctx.Headers, k)
		}
	}
	return smtp.ResultAccept
}

// isLocalDomain reports whether mail for domain is delivered locally, using
// the same test as deliverMessageWithNotify (an existing, active domain).
func (s *Server) isLocalDomain(domain string) bool {
	d, err := s.database.GetDomain(strings.ToLower(domain))
	return err == nil && d != nil && d.IsActive
}

// startSubmissionSMTP creates and starts the submission (587) server.
func (s *Server) startSubmissionSMTP() error {
	submissionAddr := fmt.Sprintf("%s:%d", s.config.SMTP.Submission.Bind, s.config.SMTP.Submission.Port)
	submissionCfg := &smtp.Config{
		Hostname:       s.config.Server.Hostname,
		MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
		MaxConnections: s.config.SMTP.Submission.MaxConnections,
		ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
		WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
		TLSConfig:      s.smtpTLSConfig(),
		RequireAuth:    true,
		RequireTLS:     true,
		IsSubmission:   true,
	}

	submissionServer := smtp.NewServer(submissionCfg, s.logger)
	submissionServer.SetAuthHandler(s.authenticate)
	submissionServer.SetDeliveryHandlerWithNotify(s.deliverMessageWithNotify)
	submissionServer.SetSenderAllowedHandler(s.senderAllowed)
	// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken
	// submissionServer.SetUserSecretHandler(s.getUserSecret)
	submissionServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	submissionServer.SetTracingProvider(s.tracingProvider)
	submissionServer.SetUserRecipientLimit(s.submissionRecipientLimit())

	if err := s.serveSMTP("Submission", submissionServer, submissionAddr, nil); err != nil {
		return err
	}
	s.submissionServer = submissionServer
	s.logger.Info("Submission server started", "addr", submissionAddr)
	return nil
}

// startSubmissionTLSSMTP creates and starts the implicit-TLS (465) server.
func (s *Server) startSubmissionTLSSMTP() error {
	submissionTLSAddr := fmt.Sprintf("%s:%d", s.config.SMTP.SubmissionTLS.Bind, s.config.SMTP.SubmissionTLS.Port)
	tlsCfg := s.smtpTLSConfig()
	if tlsCfg == nil {
		s.logger.Error("Submission TLS (implicit TLS) listener not started: no TLS certificate is available (set tls.cert_file/tls.key_file or enable ACME)", "addr", submissionTLSAddr)
		return nil
	}
	submissionTLSCfg := &smtp.Config{
		Hostname:       s.config.Server.Hostname,
		MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
		MaxConnections: s.config.SMTP.SubmissionTLS.MaxConnections,
		ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
		WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
		TLSConfig:      tlsCfg,
		RequireAuth:    true,
		RequireTLS:     false, // Already on TLS
		IsSubmission:   true,
	}

	submissionTLSServer := smtp.NewServer(submissionTLSCfg, s.logger)
	submissionTLSServer.SetAuthHandler(s.authenticate)
	submissionTLSServer.SetDeliveryHandlerWithNotify(s.deliverMessageWithNotify)
	submissionTLSServer.SetSenderAllowedHandler(s.senderAllowed)
	// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken
	// submissionTLSServer.SetUserSecretHandler(s.getUserSecret)
	submissionTLSServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	submissionTLSServer.SetTracingProvider(s.tracingProvider)
	submissionTLSServer.SetUserRecipientLimit(s.submissionRecipientLimit())

	if err := s.serveSMTP("Submission TLS", submissionTLSServer, submissionTLSAddr, tlsCfg); err != nil {
		return err
	}
	s.submissionTLSServer = submissionTLSServer
	s.logger.Info("Submission TLS server started", "addr", submissionTLSAddr)
	return nil
}

// submissionRecipientLimit derives the per-user rolling-hour recipient cap for
// submission listeners from the existing security.rate_limit.user_per_hour
// setting (0 or negative disables the cap).
func (s *Server) submissionRecipientLimit() int {
	if s.config == nil || s.config.Security.RateLimit.UserPerHour <= 0 {
		return 0
	}
	return s.config.Security.RateLimit.UserPerHour
}
