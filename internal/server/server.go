package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/storage"
)

// Server is the main uMailServer instance
type Server struct {
	config     *config.Config
	logger     *slog.Logger
	database   *db.DB
	queue      *queue.Manager
	msgStore   *storage.MessageStore
	smtpServer *smtp.Server
	imapServer *imap.Server

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new Server instance
func New(cfg *config.Config) (*Server, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Logging.Level),
	}))

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		config: cfg,
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
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

	// Initialize message store
	msgStorePath := cfg.Server.DataDir + "/messages"
	msgStore, err := storage.NewMessageStore(msgStorePath)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to create message store: %w", err)
	}
	s.msgStore = msgStore

	return s, nil
}

// parseLogLevel parses log level string
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
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

	// Create mailstore for IMAP
	mailstore, err := imap.NewBboltMailstore(s.config.Server.DataDir + "/mail")
	if err != nil {
		return fmt.Errorf("failed to create mailstore: %w", err)
	}

	// Start SMTP server
	smtpAddr := fmt.Sprintf("%s:%d", s.config.SMTP.Inbound.Bind, s.config.SMTP.Inbound.Port)
	smtpCfg := &smtp.Config{
		Hostname:       s.config.Server.Hostname,
		MaxMessageSize: int64(s.config.SMTP.Inbound.MaxMessageSize),
		MaxRecipients:  s.config.SMTP.Inbound.MaxRecipients,
		ReadTimeout:    s.config.SMTP.Inbound.ReadTimeout.ToDuration(),
		WriteTimeout:   s.config.SMTP.Inbound.WriteTimeout.ToDuration(),
	}

	smtpServer := smtp.NewServer(smtpCfg, s.logger)
	smtpServer.SetAuthHandler(s.authenticate)
	smtpServer.SetDeliveryHandler(s.deliverMessage)

	// Start SMTP in background
	go func() {
		if err := smtpServer.ListenAndServe(smtpAddr); err != nil {
			s.logger.Error("SMTP server error", "error", err)
		}
	}()
	s.smtpServer = smtpServer
	s.logger.Info("SMTP server started", "addr", smtpAddr)

	// Start IMAP server
	imapAddr := fmt.Sprintf("%s:%d", s.config.IMAP.Bind, s.config.IMAP.Port)
	imapCfg := &imap.Config{
		Addr:   imapAddr,
		Logger: s.logger,
	}

	imapServer := imap.NewServer(imapCfg, mailstore)
	imapServer.SetAuthFunc(s.authenticate)

	if err := imapServer.Start(); err != nil {
		return fmt.Errorf("failed to start IMAP server: %w", err)
	}
	s.imapServer = imapServer
	s.logger.Info("IMAP server started", "addr", imapAddr)

	return nil
}

// Stop gracefully stops all server components
func (s *Server) Stop() error {
	s.logger.Info("Stopping uMailServer...")

	// Signal cancellation
	s.cancel()

	// Stop SMTP server
	if s.smtpServer != nil {
		if err := s.smtpServer.Stop(); err != nil {
			s.logger.Error("Failed to stop SMTP server", "error", err)
		}
	}

	// Stop IMAP server
	if s.imapServer != nil {
		if err := s.imapServer.Stop(); err != nil {
			s.logger.Error("Failed to stop IMAP server", "error", err)
		}
	}

	// Stop queue manager
	if s.queue != nil {
		s.queue.Stop()
	}

	// Close message store
	if s.msgStore != nil {
		s.msgStore.Close()
	}

	// Close database
	if s.database != nil {
		s.database.Close()
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

	// Check password (TODO: Implement proper password hashing)
	if account.PasswordHash != password {
		return false, nil
	}

	// Check if account is active
	if !account.IsActive {
		return false, fmt.Errorf("account is not active")
	}

	return true, nil
}

// deliverMessage delivers an incoming message
func (s *Server) deliverMessage(from string, to []string, data []byte) error {
	for _, recipient := range to {
		// Parse recipient to get user and domain
		user, domain := parseEmail(recipient)

		// Check if domain is local
		domainData, err := s.database.GetDomain(domain)
		if err != nil {
			// Domain not found, relay to remote
			if err := s.relayMessage(from, recipient, data); err != nil {
				return fmt.Errorf("failed to relay message: %w", err)
			}
			continue
		}

		if domainData == nil || !domainData.IsActive {
			// Domain not active, relay to remote
			if err := s.relayMessage(from, recipient, data); err != nil {
				return fmt.Errorf("failed to relay message: %w", err)
			}
			continue
		}

		// Local delivery
		if err := s.deliverLocal(user, domain, from, data); err != nil {
			s.logger.Error("Failed to deliver locally",
				"user", user,
				"domain", domain,
				"error", err,
			)
			return err
		}
	}

	return nil
}

// relayMessage relays a message to a remote server
func (s *Server) relayMessage(from, to string, data []byte) error {
	// TODO: Implement queue for remote delivery
	s.logger.Debug("Relaying message", "from", from, "to", to)
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
		return fmt.Errorf("user does not exist or is not active: %s", email)
	}

	// Check quota
	if account.QuotaLimit > 0 && account.QuotaUsed >= account.QuotaLimit {
		return fmt.Errorf("quota exceeded for user: %s", email)
	}

	// Store message
	messageID, err := s.msgStore.StoreMessage(email, data)
	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}

	// Update quota
	account.QuotaUsed += int64(len(data))
	s.database.UpdateAccount(account)

	s.logger.Debug("Message delivered",
		"to", email,
		"from", from,
		"message_id", messageID,
	)

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
