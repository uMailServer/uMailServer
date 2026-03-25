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
	"time"
)

// Server represents an SMTP server
type Server struct {
	config      *Config
	tlsConfig   *tls.Config
	listener    net.Listener
	listeners   []net.Listener
	connections map[string]*Session
	connMu      sync.RWMutex
	running     bool
	shutdown    chan struct{}
	logger      *slog.Logger

	// Hooks for message processing
	onAuth      func(username, password string) (bool, error)
	onValidate  func(from string, to []string) error
	onDeliver   func(from string, to []string, data []byte) error
}

// Config holds SMTP server configuration
type Config struct {
	Hostname       string
	MaxMessageSize int64
	MaxRecipients  int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	AllowInsecure  bool
	TLSConfig      *tls.Config
}

// NewServer creates a new SMTP server
func NewServer(config *Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		config:      config,
		connections: make(map[string]*Session),
		shutdown:    make(chan struct{}),
		logger:      logger,
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
	s.listeners = append(s.listeners, listener)
	s.running = true

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

		listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
		conn, err := listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			if s.running {
				s.logger.Error("accept error", slog.Any("error", err))
			}
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a new SMTP connection
func (s *Server) handleConnection(conn net.Conn) {
	session := NewSession(conn, s)

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
	s.running = false
	close(s.shutdown)

	// Close all listeners
	for _, ln := range s.listeners {
		ln.Close()
	}

	// Close all connections
	s.connMu.Lock()
	for _, session := range s.connections {
		session.Close()
	}
	s.connMu.Unlock()

	return nil
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	return s.running
}

// ActiveConnections returns the number of active connections
func (s *Server) ActiveConnections() int {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return len(s.connections)
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
		return "", err
	}
	return addr.Address, nil
}

// ExtractDomain extracts the domain from an email address
func ExtractDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
