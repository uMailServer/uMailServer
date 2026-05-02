package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/umailserver/umailserver/internal/auth"
	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	reader *bufio.Reader // set by server's command loop; reset after STARTTLS

	// Session data
	helloDomain string
	isTLS       bool
	isAuth      bool
	username    string

	// Message data
	mailFrom     string
	mailFromRet  string // RET parameter (FULL or HDRS)
	rcptTo       []string
	rcptToNotify []string // NOTIFY parameter per recipient
	data         []byte
	bdatBuffer   *bytes.Buffer

	// Sieve actions from pipeline processing
	sieveActions []string
}

// NewSession creates a new SMTP session
func NewSession(conn net.Conn, server *Server) *Session {
	return &Session{
		id:           uuid.New().String(),
		conn:         conn,
		server:       server,
		state:        StateNew,
		rcptTo:       make([]string, 0),
		rcptToNotify: make([]string, 0),
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
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	_, err := fmt.Fprintf(s.conn, "%d %s\r\n", code, message)
	return err
}

// WriteMultiLineResponse writes a multi-line SMTP response
func (s *Session) WriteMultiLineResponse(code int, lines []string) error {
	if s.server.config.WriteTimeout > 0 {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	var firstErr error
	for i, line := range lines {
		var err error
		if i < len(lines)-1 {
			_, err = fmt.Fprintf(s.conn, "%d-%s\r\n", code, line)
		} else {
			_, err = fmt.Fprintf(s.conn, "%d %s\r\n", code, line)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
		"DELIVERYSTATUS",
	}

	if s.server.config.TLSConfig != nil && !s.isTLS {
		capabilities = append(capabilities, "STARTTLS")
	}

	// Only advertise AUTH after TLS or if insecure auth is allowed on submission
	if s.isTLS || (s.server.config.IsSubmission && s.server.config.AllowInsecure) {
		authMechs := []string{"PLAIN LOGIN", "SCRAM-SHA-256"}
		// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken
		// if s.server.onGetUserSecret != nil {
		// 	authMechs = append(authMechs, "CRAM-MD5")
		// }
		capabilities = append(capabilities, strings.Join(authMechs, " "))
		// Warn if AUTH is advertised over non-TLS connection
		if !s.isTLS && s.server.config.IsSubmission && s.server.config.AllowInsecure {
			s.server.logger.Warn("SMTP AUTH advertised over unencrypted connection - credentials may be exposed",
				"remote_addr", s.conn.RemoteAddr().String(),
				"allow_insecure", s.server.config.AllowInsecure)
		}
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

	return s.WriteResponse(250, s.server.config.Hostname)
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
	from, ret, err := parseMailFromWithRet(arg)
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
	s.mailFromRet = ret
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

	// Parse RCPT TO:<address> [NOTIFY=<notify>]
	to, notify, err := parseRcptToWithNotify(arg)
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
	s.rcptToNotify = append(s.rcptToNotify, notify)
	s.state = StateRcptTo

	return s.WriteResponse(250, "OK")
}

// handleDATA handles the DATA command
func (s *Session) handleDATA() error {
	ctx := context.Background()

	// Create tracing span if provider is available
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "smtp.data", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("mail.from", s.mailFrom),
			attribute.Int("mail.recipients", len(s.rcptTo)),
			attribute.Bool("session.tls", s.isTLS),
		)
		defer span.End()
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have RCPT TO first
	if s.state != StateRcptTo {
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "bad sequence of commands")
		}
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
		if errors.Is(err, errMessageTooLarge) {
			return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
		}
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
		var remoteIP net.IP
		if host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String()); err == nil {
			remoteIP = net.ParseIP(host)
		}
		// Fallback for Unix sockets or invalid addresses
		if remoteIP == nil {
			remoteIP = net.IPv4zero
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

		// Store sieve actions for delivery processing
		s.sieveActions = ctx.SpamResult.Reasons

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
	if s.server.onDeliverWithSieve != nil {
		if err := s.server.onDeliverWithSieve(s.mailFrom, s.rcptTo, s.data, s.sieveActions); err != nil {
			s.resetTransaction()
			return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
		}
	} else if s.server.onDeliver != nil {
		if err := s.server.onDeliver(s.mailFrom, s.rcptTo, s.data); err != nil {
			s.resetTransaction()
			return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
		}
	}

	s.resetTransaction()
	return s.WriteResponse(250, "OK")
}

// errMessageTooLarge is returned by readData when the message exceeds the size limit
var errMessageTooLarge = errors.New("message too large")

// readData reads the email message data from the connection
func (s *Session) readData() ([]byte, error) {
	reader := bufio.NewReader(s.conn)
	var data []byte
	const maxLineLength = 1000 // RFC 5322: max 1000 bytes per line including CRLF

	for {
		if s.server.config.ReadTimeout > 0 {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.server.config.ReadTimeout))
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}

		// RFC 5322 line length limit check
		if len(line) > maxLineLength {
			return nil, fmt.Errorf("line exceeds maximum length of %d bytes", maxLineLength)
		}

		// Check for null bytes (security: prevent header injection)
		if bytes.Contains(line, []byte{0}) {
			return nil, fmt.Errorf("message contains null bytes")
		}

		// RFC 3629: validate UTF-8 well-formedness in message data
		// Lines that are not valid UTF-8 should be rejected per RFC 6532
		if !utf8.Valid(line) {
			return nil, fmt.Errorf("message contains invalid UTF-8 sequence")
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

		// Check accumulated size during read to prevent memory exhaustion
		if int64(len(data)) > s.server.config.MaxMessageSize {
			return nil, fmt.Errorf("%w: message exceeds maximum size of %d bytes", errMessageTooLarge, s.server.config.MaxMessageSize)
		}
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

	// Check cumulative size against limit
	if int64(s.bdatBuffer.Len()+size) > s.server.config.MaxMessageSize {
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
			var remoteIP net.IP
			if host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String()); err == nil {
				remoteIP = net.ParseIP(host)
			}
			// Fallback for Unix sockets or invalid addresses
			if remoteIP == nil {
				remoteIP = net.IPv4zero
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
	_ = s.WriteResponse(221, fmt.Sprintf("%s closing connection", s.server.config.Hostname))
	return ErrSessionQuit
}

// handleAUTH handles the AUTH command
func (s *Session) handleAUTH(arg string) error {
	ctx := context.Background()

	// Create tracing span if provider is available
	var span trace.Span
	if s.server.tracingProvider != nil && s.server.tracingProvider.IsEnabled() {
		ctx, span = s.server.tracingProvider.StartSpanWithKind(ctx, "smtp.auth", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("session.ip", getIPFromAddr(s.conn.RemoteAddr().String())),
		)
		defer span.End()
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Require TLS for authentication. Port 25 never allows insecure auth;
	// submission only allows it when explicitly configured.
	if !s.isTLS && (!s.server.config.IsSubmission || !s.server.config.AllowInsecure) {
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "encryption required")
		}
		return s.WriteResponse(538, "5.7.10 Encryption required for requested authentication mechanism")
	}

	// Must have greeted first
	if s.state == StateNew {
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "bad sequence")
		}
		return s.WriteResponse(503, "5.5.1 Bad sequence of commands")
	}

	// Already authenticated
	if s.isAuth {
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "already authenticated")
		}
		return s.WriteResponse(503, "5.5.1 Already authenticated")
	}

	// Brute-force lockout check
	if s.server.isAuthLockedOut(getIPFromAddr(s.conn.RemoteAddr().String())) {
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "auth locked out")
		}
		return s.WriteResponse(535, "5.7.8 Too many failed authentication attempts")
	}

	parts := strings.SplitN(arg, " ", 2)
	mechanism := strings.ToUpper(parts[0])

	if span != nil {
		tracing.SetStringAttribute(span, "auth.mechanism", mechanism)
	}

	switch mechanism {
	case "PLAIN":
		return s.handleAuthPLAIN(parts)
	case "LOGIN":
		return s.handleAuthLOGIN(parts)
	case "CRAM-MD5":
		// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "CRAM-MD5 disabled")
		}
		return s.WriteResponse(504, "CRAM-MD5 authentication mechanism is disabled")
	case "SCRAM-SHA-256":
		return s.handleAuthSCRAMSHA256(parts)
	default:
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "unrecognized mechanism")
		}
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
		// Record failure to prevent user enumeration via malformed auth
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	// PLAIN format: \0username\0password
	credParts := strings.Split(string(decoded), "\x00")
	if len(credParts) != 3 {
		// Record failure to prevent user enumeration via malformed auth
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}

	username := credParts[1]
	password := credParts[2]

	// RFC 7616 PRECIS: normalize username (UsernameCaseMapped: lowercase)
	usernameNormalized := strings.ToLower(username)

	// Authenticate
	if s.server.onAuth != nil {
		ok, err := s.server.onAuth(usernameNormalized, password)
		if err != nil || !ok {
			s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
			if m := metrics.Get(); m != nil {
				m.SMTPAuthFailure()
			}
			if s.server.onLoginResult != nil {
				s.server.onLoginResult(username, false, getIPFromAddr(s.conn.RemoteAddr().String()), "invalid_credentials")
			}
			return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
		}
	}

	s.isAuth = true
	s.username = username
	s.server.clearAuthFailures(getIPFromAddr(s.conn.RemoteAddr().String()))
	if s.server.onLoginResult != nil {
		s.server.onLoginResult(username, true, getIPFromAddr(s.conn.RemoteAddr().String()), "")
	}

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
		// Record failure to prevent user enumeration via malformed auth
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}
	username := string(usernameBytes)

	// RFC 7616 PRECIS: normalize username (UsernameCaseMapped: lowercase)
	usernameNormalized := strings.ToLower(username)

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
		// Record failure to prevent user enumeration via malformed auth
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}
	password := string(passwordBytes)

	// Authenticate using normalized username
	if s.server.onAuth != nil {
		ok, err := s.server.onAuth(usernameNormalized, password)
		if err != nil || !ok {
			s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
			if m := metrics.Get(); m != nil {
				m.SMTPAuthFailure()
			}
			if s.server.onLoginResult != nil {
				s.server.onLoginResult(usernameNormalized, false, getIPFromAddr(s.conn.RemoteAddr().String()), "invalid_credentials")
			}
			return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
		}
	}

	s.isAuth = true
	s.username = usernameNormalized
	s.server.clearAuthFailures(getIPFromAddr(s.conn.RemoteAddr().String()))
	if s.server.onLoginResult != nil {
		s.server.onLoginResult(username, true, getIPFromAddr(s.conn.RemoteAddr().String()), "")
	}

	return s.WriteResponse(235, "Authentication successful")
}

// handleAuthSCRAMSHA256 handles SCRAM-SHA-256 authentication (RFC 7677)
func (s *Session) handleAuthSCRAMSHA256(parts []string) error {
	var clientFirstMessage string

	if len(parts) > 1 {
		// SASL-IR: initial response provided with the AUTHENTICATE command
		clientFirstMessage = parts[1]
	} else {
		// Wait for client-first message
		if err := s.WriteResponse(334, " "); err != nil {
			return err
		}

		reader := bufio.NewReader(s.conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		clientFirstMessage = strings.TrimSpace(line)
	}

	// Decode and parse client-first message
	clientFirstDecoded, err := base64.StdEncoding.DecodeString(clientFirstMessage)
	if err != nil {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid base64 in SCRAM-SHA-256 initial response")
	}

	clientFirst := string(clientFirstDecoded)

	// Parse client-first message
	clientFirstMsg, err := auth.ParseClientFirstMessage(clientFirst)
	if err != nil {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid SCRAM-SHA-256 client-first message")
	}

	username := clientFirstMsg.AuthCID
	if username == "" {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Missing username in SCRAM-SHA-256")
	}

	// Generate server nonce and salt
	serverNonce, err := auth.GenerateNonce()
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Server error")
	}

	salt, err := auth.GenerateSalt()
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Server error")
	}

	iterations := 4096 // SCRAM default iterations

	// Build combined nonce: client nonce + server nonce
	combinedNonce := clientFirstMsg.Nonce + serverNonce

	// Build server-first message
	serverFirst := auth.BuildServerFirstMessage(combinedNonce, salt, iterations)

	// Encode and send server-first message
	serverFirstB64 := base64.StdEncoding.EncodeToString([]byte(serverFirst))
	if err := s.WriteResponse(334, serverFirstB64); err != nil {
		return err
	}

	// Read client-final message
	reader := bufio.NewReader(s.conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	clientFinalDecoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line))
	if err != nil {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid base64 in SCRAM-SHA-256 final response")
	}

	clientFinal := string(clientFinalDecoded)

	// Parse client-final message
	clientFinalMsg, err := auth.ParseClientFinalMessage(clientFinal)
	if err != nil {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid SCRAM-SHA-256 client-final message")
	}

	// Verify the nonce in client-final matches our server nonce
	if clientFinalMsg.Nonce != combinedNonce {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Nonce mismatch in SCRAM-SHA-256")
	}

	// Get user password for SCRAM
	var password string
	if s.server.onGetPassword != nil {
		password, err = s.server.onGetPassword(username)
		if err != nil {
			s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
			return s.WriteResponse(535, "5.5.4 Authentication failed")
		}
	} else if s.server.onAuth != nil {
		// Fallback: use onAuth but we need the password for SCRAM
		// This won't work well for SCRAM since we need the password to derive keys
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Password lookup not available for SCRAM-SHA-256")
	} else {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Authentication not configured")
	}

	// Create SCRAM authenticator with the password
	scram, err := auth.NewSCRAMSHA256(password, salt, iterations)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Server error in SCRAM-SHA-256")
	}

	// Compute expected client proof
	expectedProof := auth.ClientProof(scram.StoredKey(), clientFirst, serverFirst, clientFinal)

	// Verify client proof using constant-time comparison
	if !hmac.Equal(clientFinalMsg.ClientProof, expectedProof) {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		if s.server.onLoginResult != nil {
			s.server.onLoginResult(username, false, getIPFromAddr(s.conn.RemoteAddr().String()), "invalid_credentials")
		}
		return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
	}

	// Verify server signature
	expectedServerSig := auth.ServerSignature(scram.ServerKey(), clientFirst, serverFirst, clientFinal)
	serverSig := auth.ComputeSignatureKey(scram.SaltedPassword(), clientFirst, serverFirst, clientFinal)
	if !hmac.Equal(serverSig, expectedServerSig) {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(535, "5.5.4 Server signature verification failed")
	}

	// Authentication successful
	s.isAuth = true
	s.username = strings.ToLower(username)
	s.server.clearAuthFailures(getIPFromAddr(s.conn.RemoteAddr().String()))
	if s.server.onLoginResult != nil {
		s.server.onLoginResult(s.username, true, getIPFromAddr(s.conn.RemoteAddr().String()), "")
	}

	// Build and send server-final message (server signature)
	serverFinal := auth.BuildServerFinalMessage(serverSig)
	if err := s.WriteResponse(235, serverFinal); err != nil {
		return err
	}

	return nil
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

	// Perform TLS handshake with a bounded timeout
	_ = s.conn.SetDeadline(time.Now().Add(30 * time.Second))
	tlsConn := tls.Server(s.conn, s.server.config.TLSConfig)
	if err := tlsConn.Handshake(); err != nil {
		_ = s.conn.SetDeadline(time.Time{})
		return err
	}
	_ = s.conn.SetDeadline(time.Time{})

	s.conn = tlsConn
	s.isTLS = true

	// Reset the buffered reader to wrap the new TLS connection
	if s.reader != nil {
		s.reader.Reset(tlsConn)
	}

	// Reset state after TLS upgrade (RFC 3207 Section 4.1)
	s.state = StateNew
	s.resetTransaction()
	s.isAuth = false
	s.username = ""

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

// parseMailFromWithRet parses MAIL FROM with DSN RET parameter
// Format: FROM:<address> [RET=<FULL|HDRS>]
func parseMailFromWithRet(arg string) (string, string, error) {
	// Format: FROM:<address> [SIZE=nnnn] [BODY=8BITMIME] [RET=<FULL|HDRS>] etc.
	if !strings.HasPrefix(strings.ToUpper(arg), "FROM:") {
		return "", "", fmt.Errorf("invalid MAIL FROM format")
	}

	arg = arg[5:] // Remove "FROM:"
	arg = strings.TrimSpace(arg)

	// Parse optional RET parameter
	ret := ""
	for _, part := range strings.Fields(arg) {
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "RET=") {
			ret = strings.TrimPrefix(upper, "RET=")
			// Remove the param from arg
			arg = strings.TrimSpace(strings.Replace(arg, part, "", 1))
			break
		}
	}

	// Find end of address (space or end of string)
	idx := strings.IndexAny(arg, " ")
	if idx > 0 {
		arg = arg[:idx]
	}

	// Remove < and >
	arg = strings.Trim(arg, "<>")

	return arg, ret, nil
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

// parseRcptToWithNotify parses RCPT TO with DSN NOTIFY parameter
// Format: TO:<address> [NOTIFY=<notify>]
func parseRcptToWithNotify(arg string) (string, string, error) {
	// Format: TO:<address> [params]
	if !strings.HasPrefix(strings.ToUpper(arg), "TO:") {
		return "", "", fmt.Errorf("invalid RCPT TO format")
	}

	arg = arg[3:] // Remove "TO:"
	arg = strings.TrimSpace(arg)

	// Parse optional parameters (NOTIFY=)
	notify := ""
	for _, part := range strings.Fields(arg) {
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "NOTIFY=") {
			notify = strings.TrimPrefix(upper, "NOTIFY=")
			// Remove the param from arg
			arg = strings.TrimSpace(strings.Replace(arg, part, "", 1))
			break
		}
	}

	// Find end of address (space or end of string)
	idx := strings.IndexAny(arg, " ")
	if idx > 0 {
		arg = arg[:idx]
	}

	// Remove < and >
	arg = strings.Trim(arg, "<>")

	return arg, notify, nil
}

// Ensure io.Reader is implemented
var _ io.Reader = (*Session)(nil)

// Read implements io.Reader
func (s *Session) Read(p []byte) (n int, err error) {
	return s.conn.Read(p)
}
