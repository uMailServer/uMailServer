package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/umailserver/umailserver/internal/alert"
	"github.com/umailserver/umailserver/internal/api"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/caldav"
	"github.com/umailserver/umailserver/internal/carddav"
	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/health"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/jmap"
	"github.com/umailserver/umailserver/internal/logging"
	"github.com/umailserver/umailserver/internal/pop3"
	"github.com/umailserver/umailserver/internal/push"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/ratelimit"
	"github.com/umailserver/umailserver/internal/search"
	"github.com/umailserver/umailserver/internal/sieve"
	"github.com/umailserver/umailserver/internal/smtp"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/tls"
	"github.com/umailserver/umailserver/internal/webhook"
)

// Server is the main uMailServer instance
type Server struct {
	config            *config.Config
	logger            *slog.Logger
	database          *db.DB
	queue             *queue.Manager
	msgStore          *storage.MessageStore
	smtpServer        *smtp.Server
	imapServer        *imap.Server
	apiServer         *api.Server
	adminServer       *api.AdminServer
	tlsManager        *tls.Manager
	webhookMgr        *webhook.Manager
	alertMgr          *alert.Manager
	pushSvc           *push.Service
	searchSvc         *search.Service
	sieveManager      *sieve.Manager
	storageDB         *storage.Database
	mailstore         *imap.BboltMailstore
	pop3Server        *pop3.Server
	mcpHTTPServer     *http.Server
	healthMonitor     *health.Monitor
	rateLimiter       *ratelimit.RateLimiter
	manageSieveServer *sieve.ManageSieveServer
	caldavServer      *caldav.Server
	caldavHTTPServer  *http.Server
	carddavServer     *carddav.Server
	carddavHTTPServer *http.Server
	jmapServer        *jmap.Server
	jmapHTTPServer    *http.Server

	// S/MIME and OpenPGP keystores
	smimeKeystore   *smtp.SMIMEKeystore
	openpgpKeystore *smtp.OpenPGPKeystore

	// LDAP authentication client (optional, nil if LDAP disabled)
	ldapClient *auth.LDAPClient

	// Submission SMTP servers (ports 587/465)
	submissionServer    *smtp.Server
	submissionTLSServer *smtp.Server

	// Search indexing worker pool
	indexWork chan indexJob

	// Vacation reply deduplication: key = recipient+"|"+sender -> last sent time
	vacationReplies   map[string]time.Time
	vacationRepliesMu sync.Mutex

	// Background task semaphore to limit concurrent goroutines spawned per delivery
	bgSem chan struct{}

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
		config:          cfg,
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		sieveManager:    sieve.NewManager(),
		smimeKeystore:   smtp.NewSMIMEKeystore(),
		openpgpKeystore: smtp.NewOpenPGPKeystore(),
		bgSem:           make(chan struct{}, 100),
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

	// Run pending database migrations
	if err := database.RunMigrations(); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

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

	// Initialize alert manager (disabled by default unless configured)
	alertCfg := alert.DefaultConfig()
	s.alertMgr = alert.NewManager(alertCfg, s.logger)

	// Initialize push notification service
	pushDataDir := filepath.Join(s.config.Server.DataDir, "push")
	pushSvc, err := push.NewService(pushDataDir, logger)
	if err != nil {
		logger.Warn("Failed to initialize push service", "error", err)
	} else {
		s.pushSvc = pushSvc
		logger.Info("Push notification service initialized")
	}

	// Initialize LDAP client if enabled
	if cfg.LDAP.Enabled {
		ldapCfg := auth.LDAPConfig{
			Enabled:        cfg.LDAP.Enabled,
			URL:            cfg.LDAP.URL,
			BindDN:         cfg.LDAP.BindDN,
			BindPassword:   cfg.LDAP.BindPassword,
			BaseDN:         cfg.LDAP.BaseDN,
			UserFilter:     cfg.LDAP.UserFilter,
			EmailAttribute: cfg.LDAP.EmailAttribute,
			NameAttribute:  cfg.LDAP.NameAttribute,
			GroupAttribute: cfg.LDAP.GroupAttribute,
			AdminGroups:    cfg.LDAP.AdminGroups,
			StartTLS:       cfg.LDAP.StartTLS,
			SkipVerify:     cfg.LDAP.SkipVerify,
			Timeout:        cfg.LDAP.Timeout,
		}
		ldapClient, err := auth.NewLDAPClient(ldapCfg)
		if err != nil {
			tlsManager.Close()
			msgStore.Close()
			database.Close()
			return nil, fmt.Errorf("failed to create LDAP client: %w", err)
		}
		s.ldapClient = ldapClient
		logger.Info("LDAP authentication enabled", "url", cfg.LDAP.URL)
	}

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

	// Initialize rate limiter with config
	rateLimiterConfig := &ratelimit.Config{
		IPPerMinute:       cfg.Security.RateLimit.IPPerMinute,
		IPPerHour:         cfg.Security.RateLimit.IPPerHour,
		IPPerDay:          cfg.Security.RateLimit.IPPerDay,
		IPConnections:     cfg.Security.RateLimit.IPConnections,
		UserPerMinute:     cfg.Security.RateLimit.UserPerMinute,
		UserPerHour:       cfg.Security.RateLimit.UserPerHour,
		UserPerDay:        cfg.Security.RateLimit.UserPerDay,
		UserMaxRecipients: cfg.Security.RateLimit.UserMaxRecipients,
		GlobalPerMinute:   cfg.Security.RateLimit.GlobalPerMinute,
		GlobalPerHour:     cfg.Security.RateLimit.GlobalPerHour,
		CleanupInterval:   5 * time.Minute,
	}
	s.rateLimiter = ratelimit.New(storageDB.Bolt(), rateLimiterConfig)

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

// GetDatabase returns the database instance
func (s *Server) GetDatabase() *db.DB {
	return s.database
}

// GetQueue returns the queue manager
func (s *Server) GetQueue() *queue.Manager {
	return s.queue
}
