package imap

import (
	"bufio"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
)

// Server represents an IMAP server
type Server struct {
	addr      string
	tlsConfig *tls.Config
	logger    *slog.Logger

	listeners  []net.Listener
	sessions   map[string]*Session
	sessionsMu sync.RWMutex
	shutdown   chan struct{}
	stopOnce   sync.Once
	running    atomic.Bool

	// Authentication
	authFunc func(username, password string) (bool, error)

	// Mailbox operations
	mailstore Mailstore

	// Called when messages are expunged so search index can be updated
	onExpunge func(user, mailbox string, uid uint32)

	// Auth brute-force protection
	maxLoginAttempts int
	lockoutDuration  time.Duration
	authFailures     map[string][]time.Time // IP -> failure timestamps
	authFailuresMu   sync.Mutex

	// Connection timeouts
	readTimeout  time.Duration
	writeTimeout time.Duration
	idleTimeout  time.Duration

	// Connection limits
	maxConnections int
}

// Mailstore interface for mailbox operations
type Mailstore interface {
	// Authentication
	Authenticate(username, password string) (bool, error)

	// Mailbox operations
	SelectMailbox(user, mailbox string) (*Mailbox, error)
	CreateMailbox(user, mailbox string) error
	DeleteMailbox(user, mailbox string) error
	RenameMailbox(user, oldName, newName string) error
	ListMailboxes(user, pattern string) ([]string, error)

	// Message operations
	FetchMessages(user, mailbox string, seqSet string, items []string) ([]*Message, error)
	StoreFlags(user, mailbox string, seqSet string, flags []string, op FlagOperation) error
	Expunge(user, mailbox string) error
	AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error
	SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error)
	CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error
	MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error

	// Default mailbox provisioning
	EnsureDefaultMailboxes(user string) error
}

// Config holds server configuration
type Config struct {
	Addr      string
	TLSConfig *tls.Config
	Logger    *slog.Logger
}

// NewServer creates a new IMAP server
func NewServer(config *Config, mailstore Mailstore) *Server {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &Server{
		addr:         config.Addr,
		tlsConfig:    config.TLSConfig,
		logger:       config.Logger,
		sessions:     make(map[string]*Session),
		shutdown:     make(chan struct{}),
		mailstore:    mailstore,
		authFailures: make(map[string][]time.Time),
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// SetOnExpunge sets a callback invoked when messages are expunged.
// This is used to keep the search index in sync with mailbox state.
func (s *Server) SetOnExpunge(fn func(user, mailbox string, uid uint32)) {
	s.onExpunge = fn
}

// SetAuthLimits configures brute-force protection for IMAP AUTH
func (s *Server) SetAuthLimits(maxAttempts int, lockoutDuration time.Duration) {
	s.maxLoginAttempts = maxAttempts
	s.lockoutDuration = lockoutDuration
}

// SetReadTimeout sets the read timeout for IMAP connections (except during IDLE).
func (s *Server) SetReadTimeout(d time.Duration) {
	s.readTimeout = d
}

// SetWriteTimeout sets the write timeout for IMAP connections.
func (s *Server) SetWriteTimeout(d time.Duration) {
	s.writeTimeout = d
}

// SetIdleTimeout sets the maximum duration for the IDLE command.
func (s *Server) SetIdleTimeout(d time.Duration) {
	s.idleTimeout = d
}

// SetMaxConnections sets the maximum number of concurrent IMAP connections.
func (s *Server) SetMaxConnections(n int) {
	s.maxConnections = n
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
}

// clearAuthFailures removes recorded failures for the given IP
func (s *Server) clearAuthFailures(ip string) {
	s.authFailuresMu.Lock()
	defer s.authFailuresMu.Unlock()
	delete(s.authFailures, ip)
}

// Start starts the IMAP server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listeners = append(s.listeners, listener)
	s.running.Store(true)

	s.logger.Info("IMAP server started", "addr", s.addr)

	go s.acceptLoop(listener)

	return nil
}

// StartTLS starts the IMAP server with implicit TLS
func (s *Server) StartTLS() error {
	if s.tlsConfig == nil {
		return fmt.Errorf("TLS config not provided")
	}

	listener, err := tls.Listen("tcp", s.addr, s.tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen TLS: %w", err)
	}

	s.listeners = append(s.listeners, listener)
	s.running.Store(true)

	s.logger.Info("IMAP server started with TLS", "addr", s.addr)

	go s.acceptLoop(listener)

	return nil
}

// Stop stops the IMAP server
func (s *Server) Stop() error {
	s.running.Store(false)
	s.stopOnce.Do(func() { close(s.shutdown) })

	for _, listener := range s.listeners {
		if err := listener.Close(); err != nil {
			s.logger.Debug("failed to close listener", "error", err)
		}
	}

	// Close all sessions
	s.sessionsMu.Lock()
	for _, session := range s.sessions {
		session.Close()
	}
	s.sessions = make(map[string]*Session)
	s.sessionsMu.Unlock()

	s.logger.Info("IMAP server stopped")
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop(listener net.Listener) {
	for {
		select {
		case <-s.shutdown:
			return
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			if s.running.Load() {
				s.logger.Error("Failed to accept connection", "error", err)
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single IMAP connection
func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in IMAP connection handler", "error", r)
			conn.Close()
		}
	}()

	s.sessionsMu.RLock()
	atLimit := s.maxConnections > 0 && len(s.sessions) >= s.maxConnections
	s.sessionsMu.RUnlock()
	if atLimit {
		_, _ = conn.Write([]byte("* BYE Too many connections\r\n"))
		_ = conn.Close()
		return
	}

	session := NewSession(conn, s)
	metrics.Get().IMAPConnection()

	s.sessionsMu.Lock()
	s.sessions[session.ID()] = session
	s.sessionsMu.Unlock()

	s.logger.Info("New IMAP session", "session", session.ID(), "remote", conn.RemoteAddr())

	// Send greeting with capability advertisement
	caps := defaultCapabilities()
	session.WriteResponse("*", "OK [CAPABILITY IMAP4rev2 IMAP4rev1 "+strings.Join(caps, " ")+"] uMailServer ready")

	// Handle commands
	session.Handle()

	// Cleanup
	s.sessionsMu.Lock()
	delete(s.sessions, session.ID())
	s.sessionsMu.Unlock()

	s.logger.Info("IMAP session ended", "session", session.ID())
}

// Session represents an IMAP client session
type Session struct {
	id       string
	conn     net.Conn
	reader   *bufio.Reader
	writer   *bufio.Writer
	server   *Server
	state    State
	user     string
	selected *Mailbox

	// TLS
	tlsConn   *tls.Conn
	tlsActive bool

	// Compression
	compressActive bool
	compressReader *gzip.Reader
	compressWriter *gzip.Writer

	// Capabilities
	capabilities []string

	// Command tag
	tag string

	// IDLE state
	idleActive     bool
	idleStop       chan struct{}
	idleNotifyChan chan MailboxNotification
}

// State represents the IMAP session state
type State int

const (
	StateNotAuthenticated State = iota
	StateAuthenticated
	StateSelected
	StateLoggedOut
)

// NewSession creates a new IMAP session
func NewSession(conn net.Conn, server *Server) *Session {
	return &Session{
		id:           generateSessionID(),
		conn:         conn,
		reader:       bufio.NewReader(conn),
		writer:       bufio.NewWriter(conn),
		server:       server,
		state:        StateNotAuthenticated,
		capabilities: defaultCapabilities(),
	}
}

// ID returns the session ID
func (s *Session) ID() string {
	return s.id
}

// State returns the current session state
func (s *Session) State() State {
	return s.state
}

// User returns the authenticated user
func (s *Session) User() string {
	return s.user
}

// Selected returns the currently selected mailbox
func (s *Session) Selected() *Mailbox {
	return s.selected
}

// Close closes the session
func (s *Session) Close() {
	s.state = StateLoggedOut
	_ = s.conn.Close() // Best-effort close
}

// Handle processes commands from the client
func (s *Session) Handle() {
	for s.state != StateLoggedOut {
		if s.server.readTimeout > 0 && !s.idleActive {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.server.readTimeout)) // Best-effort deadline
		}
		line, err := s.readLine()
		if err != nil {
			if s.state != StateLoggedOut {
				s.server.logger.Error("Failed to read command", "error", err)
			}
			return
		}

		if line == "" {
			continue
		}

		s.server.logger.Debug("IMAP command", "session", s.id, "line", truncateCommand(line, 80))

		err = s.handleCommand(line)
		if err != nil {
			s.server.logger.Error("Command error", "error", err)
		}
	}
}

// readLine reads a line from the connection
func (s *Session) readLine() (string, error) {
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// setWriteDeadline sets the write deadline if configured.
func (s *Session) setWriteDeadline() {
	if s.server.writeTimeout > 0 {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.server.writeTimeout)) // Best-effort deadline
	}
}

// WriteResponse writes an IMAP response
func (s *Session) WriteResponse(tag string, response string) {
	s.setWriteDeadline()
	line := fmt.Sprintf("%s %s\r\n", tag, response)
	_, _ = s.writer.WriteString(line) // Best-effort write
	_ = s.writer.Flush()              // Best-effort flush
}

// WriteContinuation writes a continuation request
func (s *Session) WriteContinuation(text string) {
	s.setWriteDeadline()
	line := fmt.Sprintf("+ %s\r\n", text)
	_, _ = s.writer.WriteString(line) // Best-effort write
	_ = s.writer.Flush()              // Best-effort flush
}

// WriteData writes an untagged response
func (s *Session) WriteData(response string) {
	s.setWriteDeadline()
	line := fmt.Sprintf("* %s\r\n", response)
	_, _ = s.writer.WriteString(line) // Best-effort write
	_ = s.writer.Flush()              // Best-effort flush
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// defaultCapabilities returns the default server capabilities
func defaultCapabilities() []string {
	return []string{
		"IMAP4rev2",
		"IMAP4rev1",
		"STARTTLS",
		"AUTH=PLAIN",
		"AUTH=LOGIN",
		"IDLE",
		"NAMESPACE",
		"CHILDREN",
		"UIDPLUS",
		"MOVE",
		"CONDSTORE",
		"ENABLE",
		"LITERAL+",
		"SASL-IR",
		"ESEARCH",
		"ID",
		"COMPRESS=DEFLATE",
		"SORT",
		"THREAD=REFERENCES",
		"THREAD=ORDEREDSUBJECT",
	}
}

// truncateCommand truncates a command line for safe logging.
func truncateCommand(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
