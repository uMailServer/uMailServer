package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/av"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/health"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/logging"
	"github.com/umailserver/umailserver/internal/mcp"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/pop3"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/sieve"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/spam"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/tls"
	"github.com/umailserver/umailserver/internal/webhook"
	"golang.org/x/crypto/bcrypt"
)

// Server is the main uMailServer instance
type Server struct {
	config        *config.Config
	logger        *slog.Logger
	database      *db.DB
	queue         *queue.Manager
	msgStore      *storage.MessageStore
	smtpServer    *smtp.Server
	imapServer    *imap.Server
	apiServer     *api.Server
	tlsManager    *tls.Manager
	webhookMgr    *webhook.Manager
	searchSvc     *search.Service
	sieveManager  *sieve.Manager
	storageDB     *storage.Database
	mailstore     *imap.BboltMailstore
	pop3Server    *pop3.Server
	mcpHTTPServer *http.Server
	healthMonitor *health.Monitor

	// Submission SMTP servers (ports 587/465)
	submissionServer    *smtp.Server
	submissionTLSServer *smtp.Server

	// Search indexing worker pool
	indexWork chan indexJob

	// Vacation reply deduplication: key = recipient+"|"+sender -> last sent time
	vacationReplies   map[string]time.Time
	vacationRepliesMu sync.Mutex

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// New creates a new Server instance
func New(cfg *config.Config) (*Server, error) {
	// Setup log output
	var logHandler slog.Handler
	if cfg.Logging.Output == "stdout" || cfg.Logging.Output == "" {
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	} else if cfg.Logging.Output == "stderr" {
		logHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	} else {
		// File output with rotation
		writer, err := logging.NewRotatingWriter(
			cfg.Logging.Output,
			cfg.Logging.MaxSizeMB,
			cfg.Logging.MaxBackups,
			cfg.Logging.MaxAgeDays,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create log writer: %w", err)
		}
		logHandler = slog.NewJSONHandler(writer, &slog.HandlerOptions{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	}

	logger := slog.New(logHandler)

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		sieveManager: sieve.NewManager(),
	}

	// Initialize database
	dbPath := cfg.Database.Path
	if dbPath == "" {
		dbPath = cfg.Server.DataDir + "/umailserver.db"
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	s.database = database

	// Initialize message store (use same path as IMAP mailstore)
	msgStorePath := s.config.Server.DataDir + "/mail/messages"
	msgStore, err := storage.NewMessageStore(msgStorePath)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to create message store: %w", err)
	}
	s.msgStore = msgStore

	// Initialize TLS manager
	tlsConfig := tls.Config{
		AutoTLS:    cfg.TLS.ACME.Enabled,
		Email:      cfg.TLS.ACME.Email,
		Domains:    []string{cfg.Server.Hostname},
		UseStaging: cfg.TLS.ACME.Provider == "letsencrypt-staging",
		CertFile:   cfg.TLS.CertFile,
		KeyFile:    cfg.TLS.KeyFile,
	}

	tlsManager, err := tls.NewManager(tlsConfig, logger)
	if err != nil {
		msgStore.Close()
		database.Close()
		return nil, fmt.Errorf("failed to create TLS manager: %w", err)
	}
	s.tlsManager = tlsManager

	// Initialize webhook manager
	webhookMgr := webhook.NewManager(database, cfg.Security.JWTSecret)
	s.webhookMgr = webhookMgr

	// Initialize storage database for search
	storageDBPath := s.config.Server.DataDir + "/mail/mail.db"
	storageDB, err := storage.OpenDatabase(storageDBPath)
	if err != nil {
		tlsManager.Close()
		msgStore.Close()
		database.Close()
		return nil, fmt.Errorf("failed to open storage database: %w", err)
	}

	// Initialize search service
	s.storageDB = storageDB
	searchSvc := search.NewService(storageDB, msgStore, logger)
	s.searchSvc = searchSvc
	s.indexWork = make(chan indexJob, 1000)

	// Initialize health monitor
	s.healthMonitor = health.NewMonitor("1.0.0")

	return s, nil
}

// parseLogLevel parses log level string
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug", "trace":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "fatal":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

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

	// Create mailstore for IMAP using shared storage
	s.mailstore = imap.NewBboltMailstoreWithInterfaces(s.storageDB, s.msgStore)

	s.startSMTP()

	// Start search indexing worker pool
	if s.searchSvc != nil {
		for i := 0; i < 10; i++ {
			s.wg.Add(1)
			go s.runIndexWorker()
		}
	}

	if err := s.startIMAP(s.mailstore); err != nil {
		return err
	}

	if err := s.startPOP3(s.mailstore); err != nil {
		return err
	}

	s.startMCP()
	s.startAPI()

	return nil
}

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
	smtpServer.SetDeliveryHandler(s.deliverMessage)
	smtpServer.SetUserSecretHandler(s.getUserSecret)
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

	pipeline.AddStage(smtp.NewAuthSPFStage(spfChecker, s.logger))
	pipeline.AddStage(smtp.NewAuthDKIMStage(dkimVerifier, s.logger))
	pipeline.AddStage(smtp.NewAuthDMARCStage(dmarcEvaluator, s.logger))
	pipeline.AddStage(smtp.NewAuthARCStage(arcValidator, s.logger))

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
		submissionServer.SetDeliveryHandler(s.deliverMessage)
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
		submissionTLSServer.SetDeliveryHandler(s.deliverMessage)
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

// startIMAP creates and starts the IMAP server.
func (s *Server) startIMAP(mailstore *imap.BboltMailstore) error {
	imapAddr := fmt.Sprintf("%s:%d", s.config.IMAP.Bind, s.config.IMAP.Port)
	imapCfg := &imap.Config{
		Addr:      imapAddr,
		TLSConfig: s.tlsManager.GetTLSConfig(),
		Logger:    s.logger,
	}

	imapServer := imap.NewServer(imapCfg, mailstore)
	imapServer.SetAuthFunc(s.authenticate)
	imapServer.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	imapServer.SetReadTimeout(10 * time.Minute)
	imapServer.SetWriteTimeout(10 * time.Minute)
	imapServer.SetIdleTimeout(time.Duration(s.config.IMAP.IdleTimeout))
	imapServer.SetMaxConnections(s.config.IMAP.MaxConnections)
	if s.searchSvc != nil {
		imapServer.SetOnExpunge(func(user, mailbox string, uid uint32) {
			s.searchSvc.RemoveMessage(user, mailbox, uid)
		})
	}

	if err := imapServer.Start(); err != nil {
		return fmt.Errorf("failed to start IMAP server: %w", err)
	}
	s.imapServer = imapServer
	s.logger.Info("IMAP server started", "addr", imapAddr)
	return nil
}

// startPOP3 creates and starts the POP3 server (if enabled).
func (s *Server) startPOP3(mailstore *imap.BboltMailstore) error {
	if !s.config.POP3.Enabled {
		return nil
	}

	pop3Addr := fmt.Sprintf("%s:%d", s.config.POP3.Bind, s.config.POP3.Port)
	pop3Adapter := &pop3MailstoreAdapter{
		mailstore: mailstore,
		msgStore:  s.msgStore,
	}
	pop3Server := pop3.NewServer(pop3Addr, pop3Adapter, s.logger)
	pop3Server.SetAuthFunc(s.authenticate)
	pop3Server.SetAPOPSecretHandler(s.getAPOPSecret)
	pop3Server.SetAuthLimits(s.config.Security.MaxLoginAttempts, time.Duration(s.config.Security.LockoutDuration))
	pop3Server.SetReadTimeout(10 * time.Minute)
	pop3Server.SetWriteTimeout(10 * time.Minute)
	pop3Server.SetMaxConnections(s.config.POP3.MaxConnections)

	if s.tlsManager.IsEnabled() {
		pop3Server.SetTLSConfig(&pop3.TLSConfig{
			CertFile: s.config.TLS.CertFile,
			KeyFile:  s.config.TLS.KeyFile,
		})
	}

	if err := pop3Server.Start(); err != nil {
		return fmt.Errorf("failed to start POP3 server: %w", err)
	}
	s.pop3Server = pop3Server
	s.logger.Info("POP3 server started", "addr", pop3Addr)
	return nil
}

// startMCP creates and starts the MCP server (if enabled).
func (s *Server) startMCP() {
	if !s.config.MCP.Enabled {
		return
	}

	mcpAddr := fmt.Sprintf("%s:%d", s.config.MCP.Bind, s.config.MCP.Port)
	mcpSrv := mcp.NewServer(s.database)
	if s.config.MCP.AuthToken == "" {
		token := generateSecureToken()
		s.config.MCP.AuthToken = token
		s.logger.Warn("MCP: no auth token configured; generated a random token", "token", token)
	}
	mcpSrv.SetAuthToken(s.config.MCP.AuthToken)
	if len(s.config.HTTP.CorsOrigins) > 0 {
		mcpSrv.SetCorsOrigin(strings.Join(s.config.HTTP.CorsOrigins, ","))
	}
	// Configure MCP rate limiting (use same limit as HTTP API)
	mcpSrv.SetRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", mcpSrv.HandleHTTP)

	s.mcpHTTPServer = &http.Server{
		Addr:    mcpAddr,
		Handler: mux,
	}

	go func() {
		if err := s.mcpHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("MCP server error", "error", err)
		}
	}()
	s.logger.Info("MCP server started", "addr", mcpAddr)
}

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

// setupHealthChecks registers health checkers and wires up endpoints
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
		s.healthMonitor.Register("storage", health.MessageStoreCheck(func() error {
			// Simple ping - try to get store path
			_ = s.msgStore
			return nil
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

// startAPI creates and starts the HTTP API server (webmail + admin).
func (s *Server) startAPI() {
	apiCfg := api.Config{
		Addr:        fmt.Sprintf("%s:%d", s.config.HTTP.Bind, s.config.HTTP.Port),
		JWTSecret:   s.config.Security.JWTSecret,
		CorsOrigins: s.config.HTTP.CorsOrigins,
	}
	s.apiServer = api.NewServer(s.database, s.logger, apiCfg)
	s.apiServer.SetSearchService(s.searchSvc)
	if s.queue != nil {
		s.apiServer.SetQueueManager(s.queue)
	}
	// Set health monitor
	if s.healthMonitor != nil {
		s.apiServer.SetHealthMonitor(s.healthMonitor)
	}
	// Set mail database for email operations
	if s.storageDB != nil {
		s.apiServer.SetMailDB(s.storageDB)
	}
	// Set message store for email operations
	if s.msgStore != nil {
		s.apiServer.SetMsgStore(s.msgStore)
	}
	// Configure API rate limiting
	s.apiServer.SetAPIRateLimit(s.config.Security.RateLimit.HTTPRequestsPerMinute)

	go func() {
		if err := s.apiServer.Start(apiCfg.Addr); err != nil {
			s.logger.Error("API server error", "error", err)
		}
	}()
	s.logger.Info("API server started", "addr", apiCfg.Addr)
}



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

	// Stop API server
	if s.apiServer != nil {
		if err := s.apiServer.Stop(); err != nil {
			s.logger.Error("Failed to stop API server", "error", err)
		}
	}

	// Stop queue manager
	if s.queue != nil {
		s.queue.Stop()
	}

	// Wait for search index workers to drain before closing databases
	s.wg.Wait()

	// Close message store
	if s.msgStore != nil {
		s.msgStore.Close()
	}

	// Close mailstore (IMAP bbolt database)
	if s.mailstore != nil {
		s.mailstore.Close()
	}

	// Close database
	if s.database != nil {
		s.database.Close()
	}

	// Close storage database
	if s.storageDB != nil {
		s.storageDB.Close()
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

// authenticate validates user credentials
func (s *Server) authenticate(username, password string) (bool, error) {
	// Parse username to get domain and local part
	user, domain := parseEmail(username)

	// Get account from database
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return false, err
	}

	// Check password using bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)); err != nil {
		return false, nil
	}

	// Check if account is active
	if !account.IsActive {
		return false, fmt.Errorf("account is not active")
	}

	return true, nil
}

// getUserSecret returns the password hash for a user, used by CRAM-MD5 authentication
func (s *Server) getUserSecret(username string) (string, error) {
	user, domain := parseEmail(username)
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return "", err
	}
	if account == nil || !account.IsActive {
		return "", fmt.Errorf("user not found or inactive")
	}
	return account.PasswordHash, nil
}

// getAPOPSecret returns the APOP hash (MD5 of password) for a user, used by APOP authentication
func (s *Server) getAPOPSecret(username string) (string, error) {
	user, domain := parseEmail(username)
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return "", err
	}
	if account == nil || !account.IsActive {
		return "", fmt.Errorf("user not found or inactive")
	}
	return account.APOPHash, nil
}

// deliverMessage delivers an incoming message
func (s *Server) deliverMessage(from string, to []string, data []byte) error {
	var errs []error
	for _, recipient := range to {
		user, domain := parseEmail(recipient)

		domainData, err := s.database.GetDomain(domain)
		if err != nil || domainData == nil || !domainData.IsActive {
			if relayErr := s.relayMessage(from, recipient, data); relayErr != nil {
				s.logger.Error("Failed to relay message", "to", recipient, "error", relayErr)
				errs = append(errs, fmt.Errorf("relay %s: %w", recipient, relayErr))
			}
			continue
		}

		// Resolve alias
		target, aliasErr := s.database.ResolveAlias(domain, user)
		if aliasErr != nil {
			s.logger.Debug("Alias resolution failed, trying direct delivery", "domain", domain, "user", user, "error", aliasErr)
		}
		if target != "" {
			tUser, tDomain := parseEmail(target)
			if tUser != "" && tDomain != "" {
				user = tUser
				domain = tDomain
			}
		}

		if err := s.deliverLocal(user, domain, from, data); err != nil {
			s.logger.Error("Failed to deliver locally", "user", user, "domain", domain, "error", err)
			errs = append(errs, fmt.Errorf("deliver %s: %w", recipient, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("delivery had %d failure(s): %w", len(errs), errors.Join(errs...))
	}
	return nil
}

// relayMessage relays a message to a remote server
func (s *Server) relayMessage(from, to string, data []byte) error {
	if s.queue != nil {
		_, err := s.queue.Enqueue(from, []string{to}, data)
		if err != nil {
			s.logger.Error("Failed to enqueue relay message", "error", err)
			return fmt.Errorf("failed to queue message: %w", err)
		}
		s.logger.Debug("Message queued for relay", "from", from, "to", to)
		return nil
	}
	s.logger.Debug("Relaying message (queue not available)", "from", from, "to", to)
	return nil
}

// deliverLocal delivers a message to a local mailbox
func (s *Server) deliverLocal(user, domain, from string, data []byte) error {
	email := user + "@" + domain

	// Check if user exists
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return fmt.Errorf("user does not exist: %s", email)
	}

	if account == nil || !account.IsActive {
		// Check catch-all target for the domain
		if domainData, derr := s.database.GetDomain(domain); derr == nil && domainData != nil && domainData.CatchAllTarget != "" {
			tUser, tDomain := parseEmail(domainData.CatchAllTarget)
			if tUser != "" && tDomain != "" {
				return s.deliverLocal(tUser, tDomain, from, data)
			}
		}
		return fmt.Errorf("user does not exist or is not active: %s", email)
	}

	// Check quota
	if account.QuotaLimit > 0 && account.QuotaUsed >= account.QuotaLimit {
		return fmt.Errorf("quota exceeded for user: %s", email)
	}

	// Handle mail forwarding (before storing, so we skip local store if not keeping copy)
	if account.ForwardTo != "" {
		forwardTargets := strings.Split(account.ForwardTo, ",")
		for _, fwd := range forwardTargets {
			fwd = strings.TrimSpace(fwd)
			if fwd == "" {
				continue
			}
			if s.queue != nil {
				if _, err := s.queue.Enqueue(email, []string{fwd}, data); err != nil {
					s.logger.Error("Failed to enqueue forwarded message", "from", email, "to", fwd, "error", err)
				}
			}
		}
		if !account.ForwardKeepCopy {
			s.logger.Debug("Message forwarded (no local copy)",
				"to", email,
				"from", from,
			)
			return nil
		}
	}

	// Store message locally
	messageID, err := s.msgStore.StoreMessage(email, data)
	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Update quota
	account.QuotaUsed += int64(len(data))
	if err := s.database.UpdateAccount(account); err != nil {
		s.logger.Error("Failed to update quota", "email", email, "error", err)
	}

	s.logger.Debug("Message delivered",
		"to", email,
		"from", from,
		"message_id", messageID,
	)

	// Store metadata and index message for search
	if s.storageDB != nil {
		uid, uidErr := s.storageDB.GetNextUID(email, "INBOX")
		if uidErr == nil {
			subject, fromAddr, toAddr, dateStr := parseBasicHeaders(data)
			meta := &storage.MessageMetadata{
				MessageID:    messageID,
				UID:          uid,
				Flags:        []string{"\\Recent"},
				InternalDate: time.Now(),
				Size:         int64(len(data)),
				Subject:      subject,
				Date:         dateStr,
				From:         fromAddr,
				To:           toAddr,
			}
			if err := s.storageDB.StoreMessageMetadata(email, "INBOX", uid, meta); err != nil {
					s.logger.Error("Failed to store message metadata", "email", email, "uid", uid, "error", err)
				}

			if s.searchSvc != nil {
				select {
				case s.indexWork <- indexJob{email: email, uid: uid}:
				default:
					s.logger.Warn("Search index queue full, dropping index job", "email", email, "uid", uid)
				}
			}
		}
	}

	// Trigger webhook for mail received
	if s.webhookMgr != nil {
		s.webhookMgr.Trigger(webhook.EventMailReceived, map[string]interface{}{
			"message_id": messageID,
			"to":         email,
			"from":       from,
			"size":       len(data),
		})
	}

	// Track delivery metric
	metrics.Get().DeliverySuccess()

	// Send vacation auto-reply if configured
	if account.VacationSettings != "" && s.queue != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("Panic in vacation reply", "error", r)
				}
			}()
			s.sendVacationReply(email, from, account.VacationSettings)
		}()
	}

	return nil
}

// parseEmail splits an email address into user and domain
func parseEmail(email string) (user, domain string) {
	at := -1
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			at = i
			break
		}
	}
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// GetDatabase returns the database instance
func (s *Server) GetDatabase() *db.DB {
	return s.database
}

// GetQueue returns the queue manager
func (s *Server) GetQueue() *queue.Manager {
	return s.queue
}

// avScannerAdapter wraps an *av.Scanner to satisfy the smtp.AVScanner interface.
// The two packages define structurally identical but distinct result types,
// so a thin adapter is required.
type avScannerAdapter struct {
	inner *av.Scanner
}

func (a *avScannerAdapter) IsEnabled() bool { return a.inner.IsEnabled() }

func (a *avScannerAdapter) Scan(data []byte) (*smtp.AVScanResult, error) {
	res, err := a.inner.Scan(data)
	if err != nil {
		return nil, err
	}
	return &smtp.AVScanResult{
		Infected: res.Infected,
		Virus:    res.Virus,
	}, nil
}

// pop3MailstoreAdapter adapts imap.BboltMailstore and storage.MessageStore
// to satisfy the pop3.Mailstore interface for POP3 access.
type pop3MailstoreAdapter struct {
	mailstore *imap.BboltMailstore
	msgStore  *storage.MessageStore
}

func (a *pop3MailstoreAdapter) Authenticate(username, password string) (bool, error) {
	return a.mailstore.Authenticate(username, password)
}

func (a *pop3MailstoreAdapter) ListMessages(user string) ([]*pop3.Message, error) {
	// Fetch messages from the INBOX mailbox
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", "1:*", []string{"RFC822.SIZE"})
	if err != nil {
		return nil, err
	}

	result := make([]*pop3.Message, 0, len(msgs))
	for i, msg := range msgs {
		result = append(result, &pop3.Message{
			Index: i,
			UID:   fmt.Sprintf("%d", msg.UID),
			Size:  msg.Size,
		})
	}
	return result, nil
}

func (a *pop3MailstoreAdapter) GetMessage(user string, index int) (*pop3.Message, error) {
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", fmt.Sprintf("%d", index+1), []string{"RFC822.SIZE"})
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	msg := msgs[0]
	return &pop3.Message{
		Index: index,
		UID:   fmt.Sprintf("%d", msg.UID),
		Size:  msg.Size,
	}, nil
}

func (a *pop3MailstoreAdapter) GetMessageData(user string, index int) ([]byte, error) {
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", fmt.Sprintf("%d", index+1), []string{"RFC822"})
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	return msgs[0].Data, nil
}

func (a *pop3MailstoreAdapter) DeleteMessage(user string, index int) error {
	seqSet := fmt.Sprintf("%d", index+1)
	return a.mailstore.StoreFlags(user, "INBOX", seqSet, []string{"\\Deleted"}, imap.FlagAdd)
}

func (a *pop3MailstoreAdapter) GetMessageCount(user string) (int, error) {
	msgs, err := a.ListMessages(user)
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

func (a *pop3MailstoreAdapter) GetMessageSize(user string, index int) (int64, error) {
	msg, err := a.GetMessage(user, index)
	if err != nil {
		return 0, err
	}
	return msg.Size, nil
}

// indexJob represents a search indexing task.
type indexJob struct {
	email string
	uid   uint32
}

// runIndexWorker processes search indexing jobs.
func (s *Server) runIndexWorker() {
	defer s.wg.Done()
	for job := range s.indexWork {
		if err := s.searchSvc.IndexMessage(job.email, "INBOX", job.uid); err != nil {
			s.logger.Error("Failed to index message for search", "email", job.email, "uid", job.uid, "error", err)
		}
	}
}

// sendVacationReply generates and enqueues an auto-reply message.
func (s *Server) sendVacationReply(recipientEmail, senderEmail, settingsJSON string) {
	senderLower := strings.ToLower(senderEmail)
	for _, prefix := range []string{"mailer-daemon@", "postmaster@", "noreply@", "no-reply@", "bounce@"} {
		if strings.HasPrefix(senderLower, prefix) {
			return
		}
	}

	key := recipientEmail + "|" + senderEmail
	s.vacationRepliesMu.Lock()
	if s.vacationReplies == nil {
		s.vacationReplies = make(map[string]time.Time)
	}
	if lastSent, ok := s.vacationReplies[key]; ok && time.Since(lastSent) < 24*time.Hour {
		s.vacationRepliesMu.Unlock()
		return
	}
	s.vacationReplies[key] = time.Now()

	// Cleanup old entries every 100 entries to prevent unbounded growth
	if len(s.vacationReplies) > 100 {
		s.cleanupVacationReplies()
	}

	s.vacationRepliesMu.Unlock()

	var settings struct {
		Enabled   bool   `json:"enabled"`
		Message   string `json:"message"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil || !settings.Enabled {
		return
	}

	now := time.Now()
	if settings.StartDate != "" {
		if start, err := time.Parse("2006-01-02", settings.StartDate); err == nil && now.Before(start) {
			return
		}
	}
	if settings.EndDate != "" {
		if end, err := time.Parse("2006-01-02", settings.EndDate); err == nil && now.After(end.Add(24*time.Hour)) {
			return
		}
	}

	autoReply := "From: " + recipientEmail + "\r\n" +
		"To: " + senderEmail + "\r\n" +
		"Subject: Auto: Out of Office\r\n" +
		"Auto-Submitted: auto-replied\r\n" +
		"Precedence: bulk\r\n" +
		"Date: " + now.Format(time.RFC1123Z) + "\r\n" +
		"\r\n" +
		settings.Message

	if _, err := s.queue.Enqueue(recipientEmail, []string{senderEmail}, []byte(autoReply)); err != nil {
		s.logger.Error("Failed to enqueue vacation reply", "error", err)
	}
}


// parseBasicHeaders extracts subject, from, to, date from raw message data.
func parseBasicHeaders(data []byte) (subject, from, to, date string) {
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return "", "", "", ""
	}
	subject = msg.Header.Get("Subject")
	from = msg.Header.Get("From")
	to = msg.Header.Get("To")
	date = msg.Header.Get("Date")
	return
}

// generateSecureToken generates a cryptographically random 32-byte hex token.
func generateSecureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// cleanupVacationReplies removes entries older than 48 hours from vacationReplies map
func (s *Server) cleanupVacationReplies() {
	cutoff := time.Now().Add(-48 * time.Hour)
	for key, lastSent := range s.vacationReplies {
		if lastSent.Before(cutoff) {
			delete(s.vacationReplies, key)
		}
	}
}
