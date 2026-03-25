package imap

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Server represents an IMAP server
type Server struct {
	addr      string
	tlsConfig *tls.Config
	logger    *slog.Logger

	listeners   []net.Listener
	sessions    map[string]*Session
	sessionsMu  sync.RWMutex
	shutdown    chan struct{}
	running     bool

	// Authentication
	authFunc func(username, password string) (bool, error)

	// Mailbox operations
	mailstore Mailstore
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
	StoreFlags(user, mailbox string, seqSet string, flags []string, add bool) error
	Expunge(user, mailbox string) error
	AppendMessage(user, mailbox string, flags []string, date time.Time, data []byte) error
	SearchMessages(user, mailbox string, criteria SearchCriteria) ([]uint32, error)
	CopyMessages(user, sourceMailbox, destMailbox string, seqSet string) error
	MoveMessages(user, sourceMailbox, destMailbox string, seqSet string) error
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
		addr:      config.Addr,
		tlsConfig: config.TLSConfig,
		logger:    config.Logger,
		sessions:  make(map[string]*Session),
		shutdown:  make(chan struct{}),
		mailstore: mailstore,
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// Start starts the IMAP server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listeners = append(s.listeners, listener)
	s.running = true

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
	s.running = true

	s.logger.Info("IMAP server started with TLS", "addr", s.addr)

	go s.acceptLoop(listener)

	return nil
}

// Stop stops the IMAP server
func (s *Server) Stop() error {
	s.running = false
	close(s.shutdown)

	for _, listener := range s.listeners {
		listener.Close()
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
			if s.running {
				s.logger.Error("Failed to accept connection", "error", err)
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single IMAP connection
func (s *Server) handleConnection(conn net.Conn) {
	session := NewSession(conn, s)

	s.sessionsMu.Lock()
	s.sessions[session.ID()] = session
	s.sessionsMu.Unlock()

	s.logger.Info("New IMAP session", "session", session.ID(), "remote", conn.RemoteAddr())

	// Send greeting
	session.WriteResponse("*", "OK IMAP4rev1 server ready")

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
	tlsConn *tls.Conn
	tlsActive bool

	// Capabilities
	capabilities []string

	// Command tag
	tag string
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
	s.conn.Close()
}

// Handle processes commands from the client
func (s *Session) Handle() {
	for s.state != StateLoggedOut {
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

		s.server.logger.Debug("IMAP command", "session", s.id, "line", line)

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

// WriteResponse writes an IMAP response
func (s *Session) WriteResponse(tag string, response string) {
	line := fmt.Sprintf("%s %s\r\n", tag, response)
	s.writer.WriteString(line)
	s.writer.Flush()
}

// WriteContinuation writes a continuation request
func (s *Session) WriteContinuation(text string) {
	line := fmt.Sprintf("+ %s\r\n", text)
	s.writer.WriteString(line)
	s.writer.Flush()
}

// WriteData writes an untagged response
func (s *Session) WriteData(response string) {
	line := fmt.Sprintf("* %s\r\n", response)
	s.writer.WriteString(line)
	s.writer.Flush()
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// defaultCapabilities returns the default server capabilities
func defaultCapabilities() []string {
	return []string{
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
	}
}
