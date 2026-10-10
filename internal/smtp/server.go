package smtp

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/tracing"
)

// maxCommandLine is the longest SMTP command line (including CRLF) the server
// accepts before answering 500.
const maxCommandLine = 2048

// ErrSessionQuit is returned when the session handles a QUIT command
var ErrSessionQuit = errors.New("QUIT")

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
	onAuth              func(username, password string) (bool, error)
	onValidate          func(from string, to []string) error
	onSenderAllowed     func(username, from string) bool
	onLocalDomain       func(domain string) bool
	rcptPerHour         int
	userRcpt            map[string][]time.Time
	userRcptMu          sync.Mutex
	onDeliverWithNotify func(from string, to []string, notify []string, data []byte) error
	onDeliver           func(from string, to []string, data []byte) error
	onDeliverWithAuth   func(from string, to []string, notify []string, data []byte, info *DeliveryAuth) error
	onDeliverWithSieve  func(from string, to []string, data []byte, sieveActions []string) error
	onGetUserSecret     func(username string) (string, error) // Get user's shared secret for CRAM-MD5
	onGetPassword       func(username string) (string, error) // Get user's password for SCRAM-SHA-256
	onLoginResult       func(username string, success bool, ip, reason string)
	pipeline            *Pipeline

	// Rate limiting
	rateLimiter ConnectionRateLimiter

	// Auth brute-force protection
	maxLoginAttempts int
	lockoutDuration  time.Duration
	authFailures     map[string][]time.Time // IP -> failure timestamps
	authFailuresMu   sync.Mutex

	// Tracing provider for OpenTelemetry
	tracingProvider *tracing.Provider
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

const (
	defaultMaxMessageSize = 50 << 20 // used when Config.MaxMessageSize <= 0
	defaultMaxRecipients  = 100      // used when Config.MaxRecipients <= 0
	defaultMaxErrors      = 20
	defaultMaxCommands    = 10000
	minDataTimeout        = 10 * time.Minute
)

// maxMessageSize returns the effective message size limit (0 = default).
func (c *Config) maxMessageSize() int64 {
	if c.MaxMessageSize <= 0 {
		return defaultMaxMessageSize
	}
	return c.MaxMessageSize
}

// maxRecipients returns the effective recipient limit (0 = default).
func (c *Config) maxRecipients() int {
	if c.MaxRecipients <= 0 {
		return defaultMaxRecipients
	}
	return c.MaxRecipients
}

// dataTimeout returns the absolute DATA phase deadline length.
func (c *Config) dataTimeout() time.Duration {
	if c.DataTimeout > 0 {
		return c.DataTimeout
	}
	d := 10 * c.ReadTimeout
	if d < minDataTimeout {
		d = minDataTimeout
	}
	return d
}

// Config holds SMTP server configuration
type Config struct {
	Hostname       string
	MaxMessageSize int64
	MaxRecipients  int
	MaxConnections int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	// DataTimeout is the absolute time allowed for one DATA phase, however
	// steadily the client trickles bytes (each line resets ReadTimeout).
	// 0 selects max(10*ReadTimeout, 10m).
	DataTimeout time.Duration
	// MaxErrors is the number of 5xx replies tolerated per session before
	// the connection is dropped with 421; 0 selects 20.
	MaxErrors int
	// MaxCommands bounds the commands per session; 0 selects 10000.
	MaxCommands   int
	AllowInsecure bool
	TLSConfig     *tls.Config

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

// SetSenderAllowedHandler installs the check that an authenticated user may
// use the MAIL FROM address (the user's own address or one of their aliases).
// It is also called with an empty from for the null sender. When unset any
// sender is accepted, which lets one user spoof another (F6041).
func (s *Server) SetSenderAllowedHandler(handler func(username, from string) bool) {
	s.onSenderAllowed = handler
}

// SetLocalDomainHandler installs the predicate telling whether mail for a
// domain is delivered locally. With it set, an unauthenticated client's RCPT
// to a non-local domain is refused at once with 554 5.7.1 (F6042).
func (s *Server) SetLocalDomainHandler(handler func(domain string) bool) {
	s.onLocalDomain = handler
}

// SetUserRecipientLimit caps the recipients one authenticated user may have
// accepted per rolling hour (0 disables the cap) (F6044).
func (s *Server) SetUserRecipientLimit(perHour int) {
	s.rcptPerHour = perHour
}

// allowUserRecipient records one accepted recipient for username and reports
// whether the hourly cap still allows it.
func (s *Server) allowUserRecipient(username string) bool {
	if s.rcptPerHour <= 0 {
		return true
	}
	s.userRcptMu.Lock()
	defer s.userRcptMu.Unlock()
	if s.userRcpt == nil {
		s.userRcpt = make(map[string][]time.Time)
	}
	now := time.Now()
	cutoff := now.Add(-time.Hour)
	key := strings.ToLower(username)
	kept := s.userRcpt[key][:0]
	for _, t := range s.userRcpt[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= s.rcptPerHour {
		s.userRcpt[key] = kept
		return false
	}
	s.userRcpt[key] = append(kept, now)
	return true
}

// SetValidateHandler sets the message validation handler
func (s *Server) SetValidateHandler(handler func(from string, to []string) error) {
	s.onValidate = handler
}

// SetDeliveryHandlerWithNotify sets the message delivery handler with per-recipient
// DSN notify preferences. This is the preferred handler when DSN NOTIFY support
// is required.
func (s *Server) SetDeliveryHandlerWithNotify(handler func(from string, to []string, notify []string, data []byte) error) {
	s.onDeliverWithNotify = handler
}

// DeliveryAuth carries the pipeline verdicts for an accepted message to a
// delivery handler. It is nil-safe to ignore: the same information (except
// the structured form) is already in the message as Authentication-Results.
type DeliveryAuth struct {
	// AuthResults is the resinfo of the Authentication-Results header this
	// server added (the text after "authserv-id;"), "" if none. It includes
	// the ARC verdict as "arc=<cv>". Pass it as authResults to
	// auth.ARCSigner.SealWithCV.
	AuthResults  string
	SieveActions []string
	SPF          SPFResult
	DKIM         DKIMResult
	DMARC        DMARCResult
	// ARC is the validation of the chain as received; ARC.Result is the cv
	// to give to auth.ARCSigner.SealWithCV when forwarding. Zero Result
	// means no ARC stage ran.
	ARC ARCResult
}

// SetDeliveryHandlerWithAuth sets the most capable delivery handler: it gets
// the DSN notify preferences, sieve actions and the pipeline's authentication
// verdicts (info is never nil). It takes precedence over the other handlers.
func (s *Server) SetDeliveryHandlerWithAuth(handler func(from string, to []string, notify []string, data []byte, info *DeliveryAuth) error) {
	s.onDeliverWithAuth = handler
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

// SetPasswordHandler sets the handler for retrieving a user's password for SCRAM-SHA-256 auth
func (s *Server) SetPasswordHandler(handler func(username string) (string, error)) {
	s.onGetPassword = handler
}

// SetLoginResultHandler sets the handler for login result events. The reason
// argument is empty on success and populated with a short tag on failure
// (e.g. "invalid_credentials") so consumers can record audit trails.
func (s *Server) SetLoginResultHandler(handler func(username string, success bool, ip, reason string)) {
	s.onLoginResult = handler
}

// SetTracingProvider sets the OpenTelemetry tracing provider
func (s *Server) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
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
	// Serve owns the listener once it returns: if Stop already ran it never
	// saw this listener, so it would otherwise stay bound. (F5056)
	defer func() { _ = listener.Close() }()
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
			_ = tl.SetDeadline(time.Now().Add(time.Second))
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
			_ = conn.Close()
		}
	}()

	// Enforce global connection limit
	s.connMu.RLock()
	atLimit := s.config.MaxConnections > 0 && len(s.connections) >= s.config.MaxConnections
	s.connMu.RUnlock()
	if atLimit {
		if _, err := conn.Write([]byte("421 4.7.0 Too many connections, try again later\r\n")); err != nil {
			s.logger.Debug("failed to write rate limit response", "error", err)
		}
		_ = conn.Close()
		return
	}

	// Check rate limit
	if s.rateLimiter != nil {
		ip := getIPFromAddr(conn.RemoteAddr().String())
		if !s.rateLimiter.Allow(ip, "smtp_connection") {
			s.logger.Warn("SMTP connection rate limited",
				slog.String("remote_addr", conn.RemoteAddr().String()),
			)
			if _, err := conn.Write([]byte("421 4.7.0 Rate limit exceeded, try again later\r\n")); err != nil {
				s.logger.Debug("failed to write rate limit response", "error", err)
			}
			_ = conn.Close()
			return
		}
	}

	session := NewSession(conn, s)
	metrics.Get().SMTPConnection()

	s.connMu.Lock()
	// Stop closes the shutdown channel before it closes the registered
	// sessions under connMu, so a connection that reaches this point after
	// Stop is not in the map Stop walked: refuse it here instead of serving
	// it on a stopped server. (F5057)
	select {
	case <-s.shutdown:
		s.connMu.Unlock()
		_ = conn.Close()
		return
	default:
	}
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

		_ = conn.Close()

		s.logger.Debug("SMTP connection closed",
			slog.String("remote_addr", conn.RemoteAddr().String()),
			slog.String("session_id", session.ID()),
		)
	}()

	// Send greeting
	_ = session.WriteResponse(220, fmt.Sprintf("%s ESMTP uMailServer", s.config.Hostname))

	// Handle commands
	reader := bufio.NewReader(conn)
	session.reader = reader
	for {
		if s.config.ReadTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		}

		// Bounded read: ReadString would buffer an unterminated command line
		// without limit (F5671). RFC 5321 §4.5.3.1.4 minimum is 512 octets;
		// extensions (SMTPUTF8, parameters) need more, so allow 2048.
		raw, total, _, err := readBoundedLine(reader, maxCommandLine+1)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Debug("read error", slog.Any("error", err))
			}
			return
		}
		if total > maxCommandLine {
			_ = session.WriteResponse(500, "5.5.2 Line too long")
			if session.errCount.Load() >= int64(defaultMaxErrors) {
				return
			}
			continue
		}

		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}

		s.logger.Debug("SMTP command",
			slog.String("session_id", session.ID()),
			slog.String("command", truncate(line, 50)),
		)

		maxCmds := s.config.MaxCommands
		if maxCmds <= 0 {
			maxCmds = defaultMaxCommands
		}
		maxErrs := s.config.MaxErrors
		if maxErrs <= 0 {
			maxErrs = defaultMaxErrors
		}
		if session.cmdCount.Add(1) > int64(maxCmds) {
			_ = session.WriteResponse(421, "4.7.0 Too many commands, closing connection")
			return
		}

		if err := session.HandleCommand(line); err != nil {
			if errors.Is(err, ErrSessionQuit) {
				return
			}
			s.logger.Debug("command error",
				slog.String("session_id", session.ID()),
				slog.Any("error", err),
			)
		}
		if session.errCount.Load() >= int64(maxErrs) {
			_ = session.WriteResponse(421, "4.7.0 Too many errors, closing connection")
			return
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
		_ = ln.Close()
	}
	s.listenersMu.Unlock()

	// Close all connections
	s.connMu.Lock()
	for _, session := range s.connections {
		_ = session.Close()
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
		// net/mail is ASCII-only: accept an internationalized (RFC 6531)
		// address, but never a malformed one (F5945).
		if validUTF8Address(email) {
			return email, nil
		}
		return "", err
	}
	// net/mail returns a quoted local part unquoted, so "a@evil.com"@local.com
	// would flatten into the ambiguous a@evil.com@local.com. Refuse local
	// parts that need quoting (F6040).
	at := strings.LastIndexByte(addr.Address, '@')
	if at <= 0 || strings.ContainsAny(addr.Address[:at], "@\"\\ \t,;:<>()[]") {
		return "", errors.New("unsupported local part")
	}
	return addr.Address, nil
}

// validUTF8Address reports whether email is a plausible RFC 6531 address that
// net/mail could not parse because of non-ASCII characters: it contains a
// non-ASCII rune, exactly one '@' with a non-empty local part and domain, and
// no control character, whitespace or address-syntax delimiter.
func validUTF8Address(email string) bool {
	ascii := true
	for _, r := range email {
		if r > unicode.MaxASCII {
			ascii = false
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune("<>(),;:\\\"[]", r) {
			return false
		}
	}
	if ascii || strings.Count(email, "@") != 1 {
		return false
	}
	at := strings.IndexByte(email, '@')
	return at > 0 && at < len(email)-1
}

// tlsAvailable reports whether STARTTLS can actually be served: a TLS config
// must exist and be able to supply a certificate (F5831).
func (s *Server) tlsAvailable() bool {
	c := s.config.TLSConfig
	if c == nil {
		return false
	}
	return len(c.Certificates) > 0 || c.GetCertificate != nil || c.GetConfigForClient != nil
}
