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
	perIPConns  map[string]int // concurrent connections per client IP (F6615), guarded by connMu
	connMu      sync.RWMutex
	listenersMu sync.Mutex // protects listeners slice
	running     atomic.Bool
	shutdown    chan struct{}
	stopOnce    sync.Once
	logger      *slog.Logger

	// active tracks registered sessions so Stop can drain in-flight ones.
	active       sync.WaitGroup
	drainTimeout time.Duration // 0 selects DefaultShutdownDrainTimeout

	// Hooks for message processing
	onAuth              func(username, password string) (bool, error)
	onValidate          func(from string, to []string) error
	onSenderAllowed     func(username, from string) bool
	onLocalDomain       func(domain string) bool
	rcptPerHour         int
	userRcpt            map[string][]time.Time
	userRcptMu          sync.Mutex
	onDeliverWithDSN    func(from string, to []string, notify []string, dsn *DSNInfo, data []byte) error
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
	userFailures     map[string][]time.Time // canonical username -> failure timestamps (F6330)

	// Tracing provider for OpenTelemetry
	tracingProvider *tracing.Provider
}

// ConnectionRateLimiter checks if a connection is allowed
type ConnectionRateLimiter interface {
	Allow(key string, limitType string) bool
}

// DefaultShutdownDrainTimeout bounds how long Stop lets in-flight sessions
// (mid-command, e.g. inside DATA/BDAT) finish before force-closing them.
const DefaultShutdownDrainTimeout = 10 * time.Second

// tlsHandshakeTimeout bounds the implicit-TLS handshake when neither
// ReadTimeout nor WriteTimeout bounds it. Variable so tests can shorten it.
var tlsHandshakeTimeout = 30 * time.Second

// SetShutdownDrainTimeout sets how long Stop waits for in-flight sessions.
func (s *Server) SetShutdownDrainTimeout(d time.Duration) {
	s.connMu.Lock()
	s.drainTimeout = d
	s.connMu.Unlock()
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
	// sessionBytesFactor: with MaxSessionBytes unset a session may read this
	// many maximum-size messages before it is closed with 421.
	sessionBytesFactor = 10
	minSessionBytes    = 1 << 20 // floor so tiny MaxMessageSize values stay usable
)

// maxSessionBytes returns the cap on total octets one session may send.
func (c *Config) maxSessionBytes() int64 {
	if c.MaxSessionBytes > 0 {
		return c.MaxSessionBytes
	}
	if n := sessionBytesFactor * c.maxMessageSize(); n > minSessionBytes {
		return n
	}
	return minSessionBytes
}

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
	// MaxConnectionsPerIP caps concurrent connections from one client IP
	// (F6615); 0 selects DefaultMaxConnectionsPerIP, negative disables it.
	MaxConnectionsPerIP int
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	// DataTimeout is the absolute time allowed for one DATA phase, however
	// steadily the client trickles bytes (each line resets ReadTimeout).
	// 0 selects max(10*ReadTimeout, 10m).
	DataTimeout time.Duration
	// MaxErrors is the number of 5xx replies tolerated per session before
	// the connection is dropped with 421; 0 selects 20.
	MaxErrors int
	// MaxCommands bounds the commands per session; 0 selects 10000.
	MaxCommands int
	// MaxSessionBytes caps the total octets read from one connection
	// (commands and message data); 0 selects 10*MaxMessageSize (at least 1 MiB).
	MaxSessionBytes int64
	// StageTimeout bounds each pipeline stage; 0 selects DefaultStageTimeout
	// (30s), negative disables it.
	StageTimeout  time.Duration
	AllowInsecure bool
	TLSConfig     *tls.Config

	// Submission mode settings
	RequireAuth  bool // Reject MAIL FROM if not authenticated (submission mode)
	RequireTLS   bool // Require TLS before AUTH
	IsSubmission bool // Submission server mode (port 587/465)
}

// DefaultMaxConnectionsPerIP is the per-IP concurrent connection cap used
// when Config.MaxConnectionsPerIP is 0.
const DefaultMaxConnectionsPerIP = 50

func (c *Config) maxConnectionsPerIP() int {
	switch {
	case c.MaxConnectionsPerIP < 0:
		return 0
	case c.MaxConnectionsPerIP == 0:
		return DefaultMaxConnectionsPerIP
	}
	return c.MaxConnectionsPerIP
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
		userFailures: make(map[string][]time.Time),
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

// DSNInfo carries the RFC 3461 envelope parameters other than NOTIFY to a
// delivery handler: RET and ENVID of MAIL FROM, and the ORCPT of each
// recipient (parallel to the recipient list; "" when not given).
type DSNInfo struct {
	Ret   string // FULL, HDRS or ""
	EnvID string // xtext, as received
	ORCPT []string
}

// SetDeliveryHandlerWithDSN sets a delivery handler that also receives the
// RET/ENVID/ORCPT parameters. It takes precedence over
// SetDeliveryHandlerWithNotify (F7310).
func (s *Server) SetDeliveryHandlerWithDSN(handler func(from string, to []string, notify []string, dsn *DSNInfo, data []byte) error) {
	s.onDeliverWithDSN = handler
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

	var backoff time.Duration
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
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if s.running.Load() {
				s.logger.Error("accept error", slog.Any("error", err))
			}
			// Back off so a persistent error does not spin a core (F7081).
			if backoff == 0 {
				backoff = 5 * time.Millisecond
			} else if backoff *= 2; backoff > time.Second {
				backoff = time.Second
			}
			select {
			case <-s.shutdown:
				return nil
			case <-time.After(backoff):
			}
			continue
		}
		backoff = 0

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

	// Enforce the per-IP concurrent connection cap (F6615). The slot is
	// reserved under connMu so concurrent accepts cannot overshoot it.
	if limit := s.config.maxConnectionsPerIP(); limit > 0 {
		peer := getIPFromAddr(conn.RemoteAddr().String())
		s.connMu.Lock()
		if s.perIPConns[peer] >= limit {
			s.connMu.Unlock()
			_, _ = conn.Write([]byte("421 4.7.0 Too many connections from your IP, try again later\r\n"))
			_ = conn.Close()
			return
		}
		if s.perIPConns == nil {
			s.perIPConns = make(map[string]int)
		}
		s.perIPConns[peer]++
		s.connMu.Unlock()
		defer func() {
			s.connMu.Lock()
			if s.perIPConns[peer] <= 1 {
				delete(s.perIPConns, peer)
			} else {
				s.perIPConns[peer]--
			}
			s.connMu.Unlock()
		}()
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
	// Re-check the global cap under the write lock: the early check above is
	// not atomic with registration, so concurrent accepts could overshoot.
	if s.config.MaxConnections > 0 && len(s.connections) >= s.config.MaxConnections {
		s.connMu.Unlock()
		_, _ = conn.Write([]byte("421 4.7.0 Too many connections, try again later\r\n"))
		_ = conn.Close()
		return
	}
	s.connections[session.ID()] = session
	s.active.Add(1)
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
		s.active.Done()

		s.logger.Debug("SMTP connection closed",
			slog.String("remote_addr", conn.RemoteAddr().String()),
			slog.String("session_id", session.ID()),
		)
	}()

	// Implicit-TLS: bound the handshake so a silent peer cannot hold a slot.
	if tc, ok := conn.(*tls.Conn); ok {
		d := tlsHandshakeTimeout
		if s.config.ReadTimeout > 0 && s.config.ReadTimeout < d {
			d = s.config.ReadTimeout
		}
		_ = conn.SetDeadline(time.Now().Add(d))
		if err := tc.Handshake(); err != nil {
			s.logger.Debug("TLS handshake failed", slog.Any("error", err))
			return
		}
		_ = conn.SetDeadline(time.Time{})
	}

	// Send greeting
	_ = session.WriteResponse(220, fmt.Sprintf("%s ESMTP uMailServer", s.config.Hostname))

	// Handle commands
	reader := bufio.NewReader(&countingReader{r: conn, n: &session.bytesIn})
	session.reader = reader
	for {
		// Graceful shutdown: mark the session idle, then re-check shutdown.
		// Stop closes shutdown before scanning for idle sessions, so at least
		// one side observes the other and no session blocks in read forever.
		session.idle.Store(true)
		select {
		case <-s.shutdown:
			return
		default:
		}
		if s.config.ReadTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
		}

		// Bounded read: ReadString would buffer an unterminated command line
		// without limit (F5671). RFC 5321 §4.5.3.1.4 minimum is 512 octets;
		// extensions (SMTPUTF8, parameters) need more, so allow 2048.
		raw, total, _, err := readBoundedLine(reader, maxCommandLine+1)
		session.idle.Store(false)
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

		if session.bytesIn.Load() > s.config.maxSessionBytes() {
			_ = session.WriteResponse(421, "4.7.0 Session data limit exceeded, closing connection")
			return
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

// countingReader counts the octets read through it.
type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
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

	// Close idle sessions now; sessions inside a command (DATA/BDAT, AUTH...)
	// finish and reply, then exit at the top of their loop. Bounded by the
	// drain timeout, after which the rest are force-closed.
	s.connMu.Lock()
	drain := s.drainTimeout
	for _, session := range s.connections {
		if session.idle.Load() {
			_ = session.Close()
		}
	}
	s.connMu.Unlock()
	if drain <= 0 {
		drain = DefaultShutdownDrainTimeout
	}

	done := make(chan struct{})
	go func() { s.active.Wait(); close(done) }()
	timer := time.NewTimer(drain)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.connMu.Lock()
		for _, session := range s.connections {
			_ = session.Close()
		}
		s.connMu.Unlock()
		// A handler stuck outside the connection (e.g. a delivery hook)
		// cannot be interrupted; do not let it hang Stop forever.
		grace := time.NewTimer(5 * time.Second)
		defer grace.Stop()
		select {
		case <-done:
		case <-grace.C:
		}
	}

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

// canonicalAuthUser is the key form for per-username lockout counters: trimmed
// and lower-cased (the lookup form is lower-cased), bounded in length so a
// client cannot grow the map with huge keys (F6330).
func canonicalAuthUser(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	if len(u) > 256 {
		u = u[:256]
	}
	return u
}

// isUserLockedOut reports whether the username has too many recent failures,
// regardless of source IP.
func (s *Server) isUserLockedOut(user string) bool {
	user = canonicalAuthUser(user)
	if s.maxLoginAttempts <= 0 || user == "" {
		return false
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	cutoff := time.Now().Add(-s.lockoutDuration)
	var recent []time.Time
	for _, t := range s.userFailures[user] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(s.userFailures, user)
	} else {
		s.userFailures[user] = recent
	}
	return len(recent) >= s.maxLoginAttempts
}

// recordUserFailure records a failed attempt against the canonical username.
func (s *Server) recordUserFailure(user string) {
	user = canonicalAuthUser(user)
	if s.maxLoginAttempts <= 0 || user == "" {
		return
	}
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	s.userFailures[user] = append(s.userFailures[user], time.Now())
	if len(s.userFailures) >= 100 {
		cutoff := time.Now().Add(-s.lockoutDuration)
		for k, times := range s.userFailures {
			var recent []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					recent = append(recent, t)
				}
			}
			if len(recent) > 0 {
				s.userFailures[k] = recent
			} else {
				delete(s.userFailures, k)
			}
		}
	}
}

// clearUserFailures resets the username's counter after a successful login.
func (s *Server) clearUserFailures(user string) {
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	delete(s.userFailures, canonicalAuthUser(user))
}
