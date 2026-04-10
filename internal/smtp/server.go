package smtp

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// Server represents an SMTP server
type Server struct {
	config      *Config
	listener    net.Listener
	listeners   []net.Listener
	connections map[string]*Session
	connMu      sync.RWMutex
	listenersMu sync.Mutex // protects listeners slice
	running     atomic.Bool
	shutdown    chan struct{}
	stopOnce    sync.Once
	logger      *slog.Logger

	// Hooks for message processing
	onAuth             func(username, password string) (bool, error)
	onValidate         func(from string, to []string) error
	onDeliver          func(from string, to []string, data []byte) error
	onDeliverWithSieve func(from string, to []string, data []byte, sieveActions []string) error
	onGetUserSecret    func(username string) (string, error) // Get user's shared secret for CRAM-MD5
	pipeline           *Pipeline

	// Rate limiting
	rateLimiter ConnectionRateLimiter

	// Auth brute-force protection
	maxLoginAttempts int
	lockoutDuration  time.Duration
	authFailures     map[string][]time.Time // IP -> failure timestamps
	authFailuresMu   sync.Mutex
}

// ConnectionRateLimiter checks if a connection is allowed
type ConnectionRateLimiter interface {
	Allow(key string, limitType string) bool
}

// SetRateLimiter sets the rate limiter for the server
func (s *Server) SetRateLimiter(rl ConnectionRateLimiter) {
	s.rateLimiter = rl
}

// SetAuthLimits configures brute-force protection for SMTP AUTH
func (s *Server) SetAuthLimits(maxAttempts int, lockoutDuration time.Duration) {
	s.maxLoginAttempts = maxAttempts
	s.lockoutDuration = lockoutDuration
}

// isAuthLockedOut returns true if the given IP is temporarily locked out
func (s *Server) isAuthLockedOut(ip string) bool {
	if s.maxLoginAttempts <= 0 {
		return false
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()

	cutoff := time.Now().Add(-s.lockoutDuration)
	var recent []time.Time
	for _, t := range s.authFailures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	s.authFailures[ip] = recent
	return len(recent) >= s.maxLoginAttempts
}

// recordAuthFailure records a failed authentication attempt from the given IP
func (s *Server) recordAuthFailure(ip string) {
	if s.maxLoginAttempts <= 0 {
		return
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	s.authFailures[ip] = append(s.authFailures[ip], time.Now())

	// Periodic cleanup when map reaches threshold
	if len(s.authFailures) >= 100 {
		s.cleanupAuthFailuresLocked()
	}
}

// cleanupAuthFailuresLocked removes old entries from authFailures map
// Must be called with authFailuresMu held
func (s *Server) cleanupAuthFailuresLocked() {
	cutoff := time.Now().Add(-s.lockoutDuration)
	for ip, times := range s.authFailures {
		var recent []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) > 0 {
			s.authFailures[ip] = recent
		} else {
			delete(s.authFailures, ip)
		}
	}
}

// clearAuthFailures removes recorded failures for the given IP
func (s *Server) clearAuthFailures(ip string) {
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	delete(s.authFailures, ip)
}

// Config holds SMTP server configuration
type Config struct {
	Hostname       string
	MaxMessageSize int64
	MaxRecipients  int
	MaxConnections int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	AllowInsecure  bool
	TLSConfig      *tls.Config

	// Submission mode settings
	RequireAuth  bool // Reject MAIL FROM if not authenticated (submission mode)
	RequireTLS   bool // Require TLS before AUTH
	IsSubmission bool // Submission server mode (port 587/465)
}

// NewServer creates a new SMTP server
func NewServer(config *Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		config:       config,
		connections:  make(map[string]*Session),
		shutdown:     make(chan struct{}),
		logger:       logger,
		authFailures: make(map[string][]time.Time),
	}
}

// SetAuthHandler sets the authentication handler
func (s *Server) SetAuthHandler(handler func(username, password string) (bool, error)) {
	s.onAuth = handler
}

// SetValidateHandler sets the message validation handler
func (s *Server) SetValidateHandler(handler func(from string, to []string) error) {
	s.onValidate = handler
}

// SetDeliveryHandler sets the message delivery handler
func (s *Server) SetDeliveryHandler(handler func(from string, to []string, data []byte) error) {
	s.onDeliver = handler
}

// SetDeliveryHandlerWithSieve sets the message delivery handler with sieve action support
func (s *Server) SetDeliveryHandlerWithSieve(handler func(from string, to []string, data []byte, sieveActions []string) error) {
	s.onDeliverWithSieve = handler
}

// SetPipeline sets the message processing pipeline
func (s *Server) SetPipeline(p *Pipeline) {
	s.pipeline = p
}

// SetUserSecretHandler sets the handler for retrieving a user's shared secret for CRAM-MD5 auth
func (s *Server) SetUserSecretHandler(handler func(username string) (string, error)) {
	s.onGetUserSecret = handler
}

// ListenAndServe starts listening on the specified address
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return s.Serve(ln)
}

// ListenAndServeTLS starts listening with TLS on the specified address
func (s *Server) ListenAndServeTLS(addr string, tlsConfig *tls.Config) error {
	ln, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return s.Serve(ln)
}

// Serve accepts connections on the listener
func (s *Server) Serve(listener net.Listener) error {
	s.listener = listener
	s.listenersMu.Lock()
	s.listeners = append(s.listeners, listener)
	s.listenersMu.Unlock()
	s.running.Store(true)

	s.logger.Info("SMTP server listening",
		slog.String("address", listener.Addr().String()),
		slog.String("hostname", s.config.Hostname),
	)

	for {
		select {
		case <-s.shutdown:
			return nil
		default:
		}

		if tl, ok := listener.(interface{ SetDeadline(time.Time) error }); ok {
			tl.SetDeadline(time.Now().Add(time.Second))
		}
		conn, err := listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			if s.running.Load() {
				s.logger.Error("accept error", slog.Any("error", err))
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a new SMTP connection
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in SMTP connection handler", "error", r)
			conn.Close()
		}
	}()

	// Enforce global connection limit
	s.connMu.RLock()
	atLimit := s.config.MaxConnections > 0 && len(s.connections) >= s.config.MaxConnections
	s.connMu.RUnlock()
	if atLimit {
		conn.Write([]byte("421 4.7.0 Too many connections, try again later\r\n"))
		conn.Close()
		return
	}

	// Check rate limit
	if s.rateLimiter != nil {
		ip := getIPFromAddr(conn.RemoteAddr().String())
		if !s.rateLimiter.Allow(ip, "smtp_connection") {
			s.logger.Warn("SMTP connection rate limited",
				slog.String("remote_addr", conn.RemoteAddr().String()),
			)
			conn.Write([]byte("421 4.7.0 Rate limit exceeded, try again later\r\n"))
			conn.Close()
			return
		}
	}

	session := NewSession(conn, s)
	metrics.Get().SMTPConnection()

	s.connMu.Lock()
	s.connections[session.ID()] = session
	s.connMu.Unlock()

	s.logger.Debug("SMTP connection established",
		slog.String("remote_addr", conn.RemoteAddr().String()),
		slog.String("session_id", session.ID()),
	)

	defer func() {
		s.connMu.Lock()
		delete(s.connections, session.ID())
		s.connMu.Unlock()

		conn.Close()

		s.logger.Debug("SMTP connection closed",
			slog.String("remote_addr", conn.RemoteAddr().String()),
			slog.String("session_id", session.ID()),
		)
	}()

	// Send greeting
	session.WriteResponse(220, fmt.Sprintf("%s ESMTP uMailServer", s.config.Hostname))

	// Handle commands
	reader := bufio.NewReader(conn)
	session.reader = reader
	for {
		if s.config.ReadTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err.Error() != "EOF" {
				s.logger.Debug("read error", slog.Any("error", err))
			}
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		s.logger.Debug("SMTP command",
			slog.String("session_id", session.ID()),
			slog.String("command", truncate(line, 50)),
		)

		if err := session.HandleCommand(line); err != nil {
			if err.Error() == "QUIT" {
				return
			}
			s.logger.Debug("command error",
				slog.String("session_id", session.ID()),
				slog.Any("error", err),
			)
		}
	}
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	s.running.Store(false)
	s.stopOnce.Do(func() {
		close(s.shutdown)
	})

	// Close all listeners
	s.listenersMu.Lock()
	for _, ln := range s.listeners {
		ln.Close()
	}
	s.listenersMu.Unlock()

	// Close all connections
	s.connMu.Lock()
	for _, session := range s.connections {
		session.Close()
	}
	s.connMu.Unlock()

	return nil
}

// getIPFromAddr extracts IP from an address string
func getIPFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// Helper function
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ValidateEmail validates an email address
func ValidateEmail(email string) (string, error) {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		// Try to handle international addresses (SMTPUTF8)
		// Some UTF-8 addresses may not parse with the strict parser
		// Basic validation: check for non-ASCII and @ sign
		if strings.Contains(email, "@") && !strings.HasPrefix(email, "@") && !strings.HasSuffix(email, "@") {
			return email, nil
		}
		return "", err
	}
	return addr.Address, nil
}
