package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/metrics"
)

// SessionState represents the current state of an SMTP session
type SessionState int

const (
	// StateNew - initial state after connection
	StateNew SessionState = iota
	// StateGreeted - after EHLO/HELO
	StateGreeted
	// StateMailFrom - after MAIL FROM
	StateMailFrom
	// StateRcptTo - after RCPT TO (can have multiple)
	StateRcptTo
	// StateData - during DATA command
	StateData
)

// Session represents an SMTP client session
type Session struct {
	id     string
	conn   net.Conn
	server *Server
	state  SessionState
	mutex  sync.RWMutex

	// Session data
	helloDomain string
	isTLS       bool
	isAuth      bool
	username    string

	// Message data
	mailFrom   string
	rcptTo     []string
	data       []byte
	bdatBuffer *bytes.Buffer
}

// NewSession creates a new SMTP session
func NewSession(conn net.Conn, server *Server) *Session {
	return &Session{
		id:     uuid.New().String(),
		conn:   conn,
		server: server,
		state:  StateNew,
		rcptTo: make([]string, 0),
	}
}

// ID returns the session ID
func (s *Session) ID() string {
	return s.id
}

// RemoteAddr returns the remote address
func (s *Session) RemoteAddr() net.Addr {
	return s.conn.RemoteAddr()
}

// State returns the current session state
func (s *Session) State() SessionState {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.state
}

// IsTLS returns whether the connection is using TLS
func (s *Session) IsTLS() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isTLS
}

// IsAuthenticated returns whether the session is authenticated
func (s *Session) IsAuthenticated() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isAuth
}

// Username returns the authenticated username
func (s *Session) Username() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.username
}

// WriteResponse writes an SMTP response to the client
func (s *Session) WriteResponse(code int, message string) error {
	if s.server.config.WriteTimeout > 0 {
		s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	_, err := fmt.Fprintf(s.conn, "%d %s\r\n", code, message)
	return err
}

// WriteMultiLineResponse writes a multi-line SMTP response
func (s *Session) WriteMultiLineResponse(code int, lines []string) error {
	if s.server.config.WriteTimeout > 0 {
		s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	for i, line := range lines {
		if i < len(lines)-1 {
			fmt.Fprintf(s.conn, "%d-%s\r\n", code, line)
		} else {
			fmt.Fprintf(s.conn, "%d %s\r\n", code, line)
		}
	}
	return nil
}

// Close closes the session connection
func (s *Session) Close() error {
	return s.conn.Close()
}

// HandleCommand processes an SMTP command
func (s *Session) HandleCommand(line string) error {
	cmd, arg := parseCommand(line)
	cmd = strings.ToUpper(cmd)

	switch cmd {
	case "EHLO":
		return s.handleEHLO(arg)
	case "HELO":
		return s.handleHELO(arg)
	case "MAIL":
		return s.handleMAIL(arg)
	case "RCPT":
		return s.handleRCPT(arg)
	case "DATA":
		return s.handleDATA()
	case "BDAT":
		return s.handleBDAT(arg)
	case "RSET":
		return s.handleRSET()
	case "VRFY":
		return s.handleVRFY(arg)
	case "EXPN":
		return s.handleEXPN(arg)
	case "HELP":
		return s.handleHELP()
	case "NOOP":
		return s.handleNOOP()
	case "QUIT":
		return s.handleQUIT()
	case "AUTH":
		return s.handleAUTH(arg)
	case "STARTTLS":
		return s.handleSTARTTLS()
	default:
		return s.WriteResponse(500, "5.5.2 Syntax error, command unrecognized")
	}
}

// handleEHLO handles the EHLO command
func (s *Session) handleEHLO(arg string) error {
	if arg == "" {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	s.mutex.Lock()
	s.helloDomain = arg
	s.state = StateGreeted
	s.resetTransaction()
	s.mutex.Unlock()

	// Send capabilities
	capabilities := []string{
		s.server.config.Hostname,
		"SIZE " + fmt.Sprintf("%d", s.server.config.MaxMessageSize),
		"8BITMIME",
		"PIPELINING",
		"ENHANCEDSTATUSCODES",
		"SMTPUTF8",
		"CHUNKING",
	}

	if s.server.config.TLSConfig != nil && !s.isTLS {
		capabilities = append(capabilities, "STARTTLS")
	}

	// Only advertise AUTH after TLS or if insecure auth is allowed
	if s.isTLS || s.server.config.AllowInsecure {
		authMechs := []string{"AUTH PLAIN LOGIN"}
		if s.server.onGetUserSecret != nil {
			authMechs = append(authMechs, "CRAM-MD5")
		}
		capabilities = append(capabilities, strings.Join(authMechs, " "))
	}

	return s.WriteMultiLineResponse(250, capabilities)
}

// handleHELO handles the HELO command (legacy)
func (s *Session) handleHELO(arg string) error {
	if arg == "" {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	s.mutex.Lock()
	s.helloDomain = arg
	s.state = StateGreeted
	s.resetTransaction()
	s.mutex.Unlock()

	return s.WriteResponse(250, fmt.Sprintf("%s", s.server.config.Hostname))
}

// handleMAIL handles the MAIL FROM command
func (s *Session) handleMAIL(arg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have greeted first
	if s.state == StateNew {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	// Submission mode: require authentication before MAIL FROM
	if s.server.config.RequireAuth && !s.isAuth {
		return s.WriteResponse(530, "5.7.0 Authentication required")
	}

	// Parse MAIL FROM:<address> [params]
	from, err := parseMailFrom(arg)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	// Validate from address
	if from != "" {
		validated, err := ValidateEmail(from)
		if err != nil {
			return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
		}
		from = validated
	}

	s.mailFrom = from
	s.state = StateMailFrom

	return s.WriteResponse(250, "OK")
}

// handleRCPT handles the RCPT TO command
func (s *Session) handleRCPT(arg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have MAIL FROM first
	if s.state != StateMailFrom && s.state != StateRcptTo {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	// Parse RCPT TO:<address>
	to, err := parseRcptTo(arg)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	// Validate to address
	validated, err := ValidateEmail(to)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	// Check max recipients
	if len(s.rcptTo) >= s.server.config.MaxRecipients {
		return s.WriteResponse(452, "4.5.3 Too many recipients")
	}

	s.rcptTo = append(s.rcptTo, validated)
	s.state = StateRcptTo

	return s.WriteResponse(250, "OK")
}

// handleDATA handles the DATA command
func (s *Session) handleDATA() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have RCPT TO first
	if s.state != StateRcptTo {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	s.state = StateData

	// Send ready for data
	if err := s.WriteResponse(354, "Start mail input; end with <CRLF>.<CRLF>"); err != nil {
		return err
	}

	// Read message data
	data, err := s.readData()
	if err != nil {
		return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
	}

	// Check message size
	if int64(len(data)) > s.server.config.MaxMessageSize {
		s.resetTransaction()
		return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
	}

	s.data = data

	// Run message through pipeline if configured
	if s.server.pipeline != nil {
		remoteIP := net.ParseIP("")
		if parts := strings.SplitN(s.conn.RemoteAddr().String(), ":", 2); len(parts) > 0 {
			remoteIP = net.ParseIP(parts[0])
		}

		ctx := NewMessageContext(remoteIP, s.mailFrom, s.rcptTo, data)
		ctx.RemoteHost = s.helloDomain
		ctx.TLS = s.isTLS
		ctx.Authenticated = s.isAuth
		ctx.Username = s.username

		// Parse message headers for pipeline stages
		if idx := bytes.Index(data, []byte("\r\n\r\n")); idx > 0 {
			headerBlock := string(data[:idx])
			for _, line := range strings.Split(headerBlock, "\r\n") {
				if colonIdx := strings.Index(line, ":"); colonIdx > 0 {
					key := strings.TrimSpace(line[:colonIdx])
					value := strings.TrimSpace(line[colonIdx+1:])
					ctx.Headers[key] = append(ctx.Headers[key], value)
				}
			}
		}

		result, err := s.server.pipeline.Process(ctx)
		if err != nil {
			s.resetTransaction()
			return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
		}

		switch result {
		case ResultReject:
			s.resetTransaction()
			code := 550
			msg := "Message rejected"
			if ctx.RejectionCode > 0 {
				code = ctx.RejectionCode
			}
			if ctx.RejectionMessage != "" {
				msg = ctx.RejectionMessage
			}
			return s.WriteResponse(code, msg)
		case ResultQuarantine:
			// Add spam headers but continue delivery
			spamHeader := fmt.Sprintf("X-Spam-Status: Yes, score=%.1f\r\n", ctx.SpamScore)
			data = append([]byte(spamHeader), data...)
			s.data = data
		}

		// Add Authentication-Results header with SPF/DKIM/DMARC results
		if ctx.SPFResult.Result != "" || ctx.DKIMResult.Domain != "" || ctx.DMARCResult.Result != "" {
			var arParts []string
			hostname := s.server.config.Hostname
			if hostname == "" {
				hostname = "localhost"
			}
			if ctx.SPFResult.Result != "" {
				arParts = append(arParts, fmt.Sprintf("spf=%s smtp.mailfrom=%s", ctx.SPFResult.Result, ctx.SPFResult.Domain))
			}
			if ctx.DKIMResult.Domain != "" {
				if ctx.DKIMResult.Valid {
					arParts = append(arParts, fmt.Sprintf("dkim=pass header.d=%s", ctx.DKIMResult.Domain))
				} else {
					reason := ctx.DKIMResult.Error
					if reason == "" {
						reason = "verification failed"
					}
					arParts = append(arParts, fmt.Sprintf("dkim=fail reason=\"%s\" header.d=%s", reason, ctx.DKIMResult.Domain))
				}
			}
			if ctx.DMARCResult.Result != "" {
				arParts = append(arParts, fmt.Sprintf("dmarc=%s header.from=%s", ctx.DMARCResult.Result, s.mailFrom))
			}
			if len(arParts) > 0 {
				arHeader := fmt.Sprintf("Authentication-Results: %s;\r\n\t%s\r\n", hostname, strings.Join(arParts, ";\r\n\t"))
				data = append([]byte(arHeader), data...)
				s.data = data
			}
		}

		// Add X-Spam headers for all messages processed by pipeline
		if ctx.SpamResult.Score > 0 {
			spamScoreHeader := fmt.Sprintf("X-Spam-Score: %.1f\r\n", ctx.SpamResult.Score)
			data = append([]byte(spamScoreHeader), data...)
			s.data = data
		}

		// Add Received trace header
		proto := "ESMTP"
		if s.isTLS {
			proto = "ESMTPS"
		}
		var received string
		if len(s.rcptTo) > 0 {
			received = fmt.Sprintf("Received: from %s ([%s]) by %s with %s for <%s>; %s\r\n",
				s.helloDomain, remoteIP.String(), s.server.config.Hostname, proto, s.rcptTo[0],
				time.Now().Format(time.RFC1123Z))
		} else {
			received = fmt.Sprintf("Received: from %s ([%s]) by %s with %s; %s\r\n",
				s.helloDomain, remoteIP.String(), s.server.config.Hostname, proto,
				time.Now().Format(time.RFC1123Z))
		}
		data = append([]byte(received), data...)
		s.data = data
	}

	// Add Message-ID if not present
	if !bytes.Contains(bytes.ToLower(data), []byte("message-id:")) {
		msgID := fmt.Sprintf("Message-ID: <%s@%s>\r\n", s.id, s.server.config.Hostname)
		data = append([]byte(msgID), data...)
		s.data = data
	}

	// Deliver message
	if s.server.onDeliver != nil {
		if err := s.server.onDeliver(s.mailFrom, s.rcptTo, s.data); err != nil {
			s.resetTransaction()
			return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
		}
	}

	s.resetTransaction()
	return s.WriteResponse(250, "OK")
}

// readData reads the email message data from the connection
func (s *Session) readData() ([]byte, error) {
	reader := bufio.NewReader(s.conn)
	var data []byte

	for {
		if s.server.config.ReadTimeout > 0 {
			s.conn.SetReadDeadline(time.Now().Add(s.server.config.ReadTimeout))
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}

		// Check for end of data marker
		if len(line) >= 3 && line[0] == '.' && line[1] == '\r' && line[2] == '\n' {
			break
		}

		// Remove dot-stuffing (leading dot is doubled)
		if len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}

		data = append(data, line...)
	}

	return data, nil
}

// handleBDAT handles the BDAT command (RFC 3030)
func (s *Session) handleBDAT(arg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have RCPT TO first
	if s.state != StateRcptTo {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	// Parse: BDAT <size> [LAST]
	parts := strings.Fields(arg)
	if len(parts) < 1 {
		return s.WriteResponse(501, "5.5.4 Syntax error in BDAT parameters")
	}

	size, err := strconv.Atoi(parts[0])
	if err != nil || size < 0 {
		return s.WriteResponse(501, "5.5.4 Syntax error in BDAT size parameter")
	}

	isLast := len(parts) > 1 && strings.ToUpper(parts[1]) == "LAST"

	// Initialize chunking buffer if needed
	if s.bdatBuffer == nil {
		s.bdatBuffer = &bytes.Buffer{}
	}

	// Check total size
	if int64(size) > s.server.config.MaxMessageSize {
		s.bdatBuffer = nil
		s.resetTransaction()
		return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
	}

	// Read chunk data
	if size > 0 {
		chunk := make([]byte, size)
		_, err := io.ReadFull(s.conn, chunk)
		if err != nil {
			return fmt.Errorf("failed to read BDAT chunk: %w", err)
		}
		s.bdatBuffer.Write(chunk)
	}

	if isLast {
		// Final chunk — process the complete message
		data := s.bdatBuffer.Bytes()
		s.bdatBuffer = nil

		// Check total message size
		if int64(len(data)) > s.server.config.MaxMessageSize {
			s.resetTransaction()
			return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
		}

		s.data = data

		// Run through pipeline if configured
		if s.server.pipeline != nil {
			remoteIP := net.ParseIP("")
			if parts := strings.SplitN(s.conn.RemoteAddr().String(), ":", 2); len(parts) > 0 {
				remoteIP = net.ParseIP(parts[0])
			}

			ctx := NewMessageContext(remoteIP, s.mailFrom, s.rcptTo, data)
			ctx.RemoteHost = s.helloDomain
			ctx.TLS = s.isTLS
			ctx.Authenticated = s.isAuth
			ctx.Username = s.username

			if idx := bytes.Index(data, []byte("\r\n\r\n")); idx > 0 {
				headerBlock := string(data[:idx])
				for _, line := range strings.Split(headerBlock, "\r\n") {
					if colonIdx := strings.Index(line, ":"); colonIdx > 0 {
						key := strings.TrimSpace(line[:colonIdx])
						value := strings.TrimSpace(line[colonIdx+1:])
						ctx.Headers[key] = append(ctx.Headers[key], value)
					}
				}
			}

			result, err := s.server.pipeline.Process(ctx)
			if err != nil {
				s.resetTransaction()
				return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
			}

			switch result {
			case ResultReject:
				s.resetTransaction()
				code := 550
				msg := "5.7.1 Message rejected"
				if ctx.RejectionCode > 0 {
					code = ctx.RejectionCode
				}
				if ctx.RejectionMessage != "" {
					msg = ctx.RejectionMessage
				}
				return s.WriteResponse(code, msg)
			case ResultQuarantine:
				spamHeader := fmt.Sprintf("X-Spam-Status: Yes, score=%.1f\r\n", ctx.SpamScore)
				data = append([]byte(spamHeader), data...)
				s.data = data
			}
		}

		// Deliver message
		if s.server.onDeliver != nil {
			if err := s.server.onDeliver(s.mailFrom, s.rcptTo, s.data); err != nil {
				s.resetTransaction()
				return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
			}
		}

		s.resetTransaction()
		return s.WriteResponse(250, "2.0.0 OK")
	}

	// Non-last chunk — acknowledge and wait for more
	return s.WriteResponse(250, "2.0.0 OK")
}

// handleRSET handles the RSET command
func (s *Session) handleRSET() error {
	s.mutex.Lock()
	s.resetTransaction()
	s.mutex.Unlock()

	return s.WriteResponse(250, "OK")
}

// handleVRFY handles the VRFY command
func (s *Session) handleVRFY(arg string) error {
	// We don't support address verification
	return s.WriteResponse(252, "Cannot VRFY user, but will accept message and attempt delivery")
}

// handleEXPN handles the EXPN command
func (s *Session) handleEXPN(arg string) error {
	// We don't support mailing list expansion
	return s.WriteResponse(550, "Action not taken: mailbox unavailable")
}

// handleHELP handles the HELP command
func (s *Session) handleHELP() error {
	return s.WriteResponse(214, "See https://tools.ietf.org/html/rfc5321")
}

// handleNOOP handles the NOOP command
func (s *Session) handleNOOP() error {
	return s.WriteResponse(250, "OK")
}

// handleQUIT handles the QUIT command
func (s *Session) handleQUIT() error {
	s.WriteResponse(221, fmt.Sprintf("%s closing connection", s.server.config.Hostname))
	return fmt.Errorf("QUIT")
}

// handleAUTH handles the AUTH command
func (s *Session) handleAUTH(arg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Require TLS for authentication (unless insecure auth is allowed)
	if !s.isTLS && !s.server.config.AllowInsecure {
		return s.WriteResponse(538, "5.7.10 Encryption required for requested authentication mechanism")
	}

	// Must have greeted first
	if s.state == StateNew {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	// Already authenticated
	if s.isAuth {
		return s.WriteResponse(503, "5.5.1 Already authenticated")
	}

	parts := strings.SplitN(arg, " ", 2)
	mechanism := strings.ToUpper(parts[0])

	switch mechanism {
	case "PLAIN":
		return s.handleAuthPLAIN(parts)
	case "LOGIN":
		return s.handleAuthLOGIN(parts)
	case "CRAM-MD5":
		return s.handleAuthCRAMMD5()
	default:
		return s.WriteResponse(504, "Unrecognized authentication type")
	}
}

// handleAuthPLAIN handles PLAIN authentication
func (s *Session) handleAuthPLAIN(parts []string) error {
	var credentials string

	if len(parts) > 1 {
		// Credentials inline
		credentials = parts[1]
	} else {
		// Wait for credentials
		if err := s.WriteResponse(334, " "); err != nil {
			return err
		}

		reader := bufio.NewReader(s.conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		credentials = strings.TrimSpace(line)
	}

	// Decode credentials
	decoded, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	// PLAIN format: \0username\0password
	credParts := strings.Split(string(decoded), "\x00")
	if len(credParts) != 3 {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	username := credParts[1]
	password := credParts[2]

	// Authenticate
	if s.server.onAuth != nil {
		ok, err := s.server.onAuth(username, password)
		if err != nil || !ok {
			if m := metrics.Get(); m != nil {
				m.SMTPAuthFailure()
			}
			return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
		}
	}

	s.isAuth = true
	s.username = username

	return s.WriteResponse(235, "Authentication successful")
}

// handleAuthLOGIN handles LOGIN authentication
func (s *Session) handleAuthLOGIN(parts []string) error {
	// Request username
	if err := s.WriteResponse(334, "VXNlcm5hbWU6"); err != nil { // base64("Username:")
		return err
	}

	// Read username
	reader := bufio.NewReader(s.conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	usernameEnc := strings.TrimSpace(line)

	usernameBytes, err := base64.StdEncoding.DecodeString(usernameEnc)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}
	username := string(usernameBytes)

	// Request password
	if err := s.WriteResponse(334, "UGFzc3dvcmQ6"); err != nil { // base64("Password:")
		return err
	}

	// Read password
	line, err = reader.ReadString('\n')
	if err != nil {
		return err
	}
	passwordEnc := strings.TrimSpace(line)

	passwordBytes, err := base64.StdEncoding.DecodeString(passwordEnc)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}
	password := string(passwordBytes)

	// Authenticate
	if s.server.onAuth != nil {
		ok, err := s.server.onAuth(username, password)
		if err != nil || !ok {
			if m := metrics.Get(); m != nil {
				m.SMTPAuthFailure()
			}
			return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
		}
	}

	s.isAuth = true
	s.username = username

	return s.WriteResponse(235, "Authentication successful")
}

// handleAuthCRAMMD5 handles CRAM-MD5 authentication (RFC 2195)
func (s *Session) handleAuthCRAMMD5() error {
	if s.server.onGetUserSecret == nil {
		return s.WriteResponse(504, "5.5.4 Unrecognized authentication type")
	}

	// Generate challenge
	_, challengeB64, err := auth.GenerateCRAMMD5Challenge()
	if err != nil {
		return s.WriteResponse(454, "4.7.0 Temporary authentication failure")
	}

	// Send challenge
	if err := s.WriteResponse(334, challengeB64); err != nil {
		return err
	}

	// Read client response
	reader := bufio.NewReader(s.conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	response := strings.TrimSpace(line)

	// Verify using the shared auth function
	username, ok := auth.VerifyCRAMMD5(challengeB64, response, s.server.onGetUserSecret)
	if !ok {
		if m := metrics.Get(); m != nil {
			m.SMTPAuthFailure()
		}
		return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
	}

	s.isAuth = true
	s.username = username

	return s.WriteResponse(235, "Authentication successful")
}

// handleSTARTTLS handles the STARTTLS command
func (s *Session) handleSTARTTLS() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isTLS {
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	if s.server.config.TLSConfig == nil {
		return s.WriteResponse(502, "5.5.1 Command not implemented")
	}

	if err := s.WriteResponse(220, "Ready to start TLS"); err != nil {
		return err
	}

	// Perform TLS handshake
	tlsConn := tls.Server(s.conn, s.server.config.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		return err
	}

	s.conn = tlsConn
	s.isTLS = true

	// Reset state after TLS upgrade
	s.state = StateNew
	s.resetTransaction()

	return nil
}

// resetTransaction resets the transaction state
func (s *Session) resetTransaction() {
	s.mailFrom = ""
	s.rcptTo = make([]string, 0)
	s.data = nil
	if s.state > StateGreeted {
		s.state = StateGreeted
	}
}

// parseCommand parses an SMTP command line
func parseCommand(line string) (cmd, arg string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}

	parts := strings.SplitN(line, " ", 2)
	cmd = strings.ToUpper(parts[0])
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	return cmd, arg
}

// parseMailFrom parses the MAIL FROM command argument
func parseMailFrom(arg string) (string, error) {
	// Format: FROM:<address> [SIZE=nnnn] [BODY=8BITMIME] etc.
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		return "", fmt.Errorf("invalid MAIL FROM format")
	}

	arg = arg[5:] // Remove "FROM:"

	// Extract address from <address> or just address
	arg = strings.TrimSpace(arg)

	// Find end of address (space or end of string)
	idx := strings.IndexAny(arg, " ")
	if idx > 0 {
		arg = arg[:idx]
	}

	// Remove < and >
	arg = strings.Trim(arg, "<>")

	return arg, nil
}

// parseRcptTo parses the RCPT TO command argument
func parseRcptTo(arg string) (string, error) {
	// Format: TO:<address> [params]
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return "", fmt.Errorf("invalid RCPT TO format")
	}

	arg = arg[3:] // Remove "TO:"

	// Extract address
	arg = strings.TrimSpace(arg)

	// Find end of address (space or end of string)
	idx := strings.IndexAny(arg, " ")
	if idx > 0 {
		arg = arg[:idx]
	}

	// Remove < and >
	arg = strings.Trim(arg, "<>")

	return arg, nil
}

// Ensure io.Reader is implemented
var _ io.Reader = (*Session)(nil)

// Read implements io.Reader
func (s *Session) Read(p []byte) (n int, err error) {
	return s.conn.Read(p)
}
