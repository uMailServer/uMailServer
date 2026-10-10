package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

	// Abuse limits (read by the server's command loop)
	cmdCount atomic.Int64
	errCount atomic.Int64
	bytesIn  atomic.Int64 // octets read from the connection, all phases

	// Per-transaction state from MAIL FROM parameters and address policy.
	rcptRaw    []string // Received "for" form: local part as received, domain lower-cased
	mailBinary bool     // BODY=BINARYMIME: only BDAT may carry the message
	smtputf8   bool     // SMTPUTF8 requested on MAIL FROM
}

// errSessionBytes marks a session that has read more than its byte budget.
var errSessionBytes = errors.New("session byte limit exceeded")

// normalizeAddr returns the canonical lower-case form of an address, the form
// handlers, policy checks and rate-limit keys see (addresses are
// case-insensitive here, RFC 5321 §2.4 permits it).
func normalizeAddr(a string) string { return strings.ToLower(a) }

// receivedForm keeps the local part as the client sent it and lower-cases the
// domain, which is case-insensitive by definition.
func receivedForm(a string) string {
	if at := strings.LastIndexByte(a, '@'); at >= 0 {
		return a[:at+1] + strings.ToLower(a[at+1:])
	}
	return a
}

// isASCII reports whether s is pure ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
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
	if code >= 500 {
		s.errCount.Add(1)
	}

	if s.server.config.WriteTimeout > 0 {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	// Strip any CRLF in the message to prevent response splitting (RFC 5321 §4.1.1.1
	// requires response text to not contain bare CR or LF; user input can reach here
	// via Sieve reject actions and other dynamic messages).
	safe := strings.ReplaceAll(strings.ReplaceAll(message, "\r", ""), "\n", "")

	_, err := fmt.Fprintf(s.conn, "%d %s\r\n", code, safe)
	return err
}

// WriteMultiLineResponse writes a multi-line SMTP response
func (s *Session) WriteMultiLineResponse(code int, lines []string) error {
	if s.server.config.WriteTimeout > 0 {
		_ = s.conn.SetWriteDeadline(time.Now().Add(s.server.config.WriteTimeout))
	}

	var firstErr error
	for i, line := range lines {
		// Strip CRLF from each line to prevent response splitting.
		safe := strings.ReplaceAll(strings.ReplaceAll(line, "\r", ""), "\n", "")
		var err error
		if i < len(lines)-1 {
			_, err = fmt.Fprintf(s.conn, "%d-%s\r\n", code, safe)
		} else {
			_, err = fmt.Fprintf(s.conn, "%d %s\r\n", code, safe)
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

// validHelloArg reports whether arg is a single token free of the characters
// that would let it forge structure in the Received trace field it is copied
// into (whitespace, control characters, ';', parentheses, angle brackets;
// F5677). Hostnames and address literals pass.
func validHelloArg(arg string) bool {
	for _, r := range arg {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(";()<>", r) {
			return false
		}
	}
	return true
}

// handleEHLO handles the EHLO command
func (s *Session) handleEHLO(arg string) error {
	if arg == "" || !validHelloArg(arg) {
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
		"SIZE " + fmt.Sprintf("%d", s.server.config.maxMessageSize()),
		"8BITMIME",
		"BINARYMIME",
		"PIPELINING",
		"ENHANCEDSTATUSCODES",
		"SMTPUTF8",
		"CHUNKING",
		"DSN",
		"DELIVERYSTATUS",
	}

	if s.server.tlsAvailable() && !s.isTLS {
		capabilities = append(capabilities, "STARTTLS")
	}

	// Only advertise AUTH after TLS or if insecure auth is allowed on submission
	if s.isTLS || (s.server.config.IsSubmission && s.server.config.AllowInsecure) {
		authMechs := []string{"PLAIN LOGIN"}
		// SCRAM needs the plaintext password; advertise it only when a
		// password source is wired (F5321).
		if s.server.onGetPassword != nil {
			authMechs = append(authMechs, "SCRAM-SHA-256")
		}
		// CRAM-MD5 disabled: HMAC-MD5 is cryptographically broken
		// if s.server.onGetUserSecret != nil {
		// 	authMechs = append(authMechs, "CRAM-MD5")
		// }
		capabilities = append(capabilities, "AUTH "+strings.Join(authMechs, " "))
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
	if arg == "" || !validHelloArg(arg) {
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

	mp, code, msg := s.checkMailParams(arg)
	if code != 0 {
		return s.WriteResponse(code, msg)
	}
	// A UTF-8 address needs the SMTPUTF8 parameter (RFC 6531 §3.4).
	if !isASCII(from) && !mp.utf8 {
		return s.WriteResponse(553, "5.6.7 Non-ASCII address requires SMTPUTF8")
	}
	from = normalizeAddr(from)

	// An authenticated user may only send as themselves or their aliases
	// (F6041).
	if s.isAuth && s.server.onSenderAllowed != nil && !s.server.onSenderAllowed(s.username, from) {
		return s.WriteResponse(553, "5.7.1 Sender address rejected: not owned by user")
	}

	s.resetTransaction()
	s.mailFrom = from
	s.mailFromRet = ret
	s.mailBinary = mp.binary
	s.smtputf8 = mp.utf8
	s.state = StateMailFrom

	return s.WriteResponse(250, "OK")
}

// mailParamFields returns the ESMTP parameters after the path of a MAIL FROM /
// RCPT TO argument (everything after the first space-separated token).
func mailParamFields(arg string) []string {
	if i := strings.IndexByte(arg, ':'); i >= 0 {
		arg = arg[i+1:]
	}
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return nil
	}
	return fields[1:]
}

// checkMailParams validates the MAIL FROM parameters this server advertises
// (RFC 1870 SIZE, RFC 6152 BODY, RFC 6531 SMTPUTF8, RFC 3461 RET/ENVID,
// RFC 4954 AUTH). It returns a reply code and text, or 0 when acceptable.
// A declared SIZE over the limit is refused here, before the client sends
// the message (RFC 1870 §6; F5672); invalid or unknown parameters are
// refused instead of silently ignored (F5673).
// mailParams holds the MAIL FROM parameters that change later handling.
type mailParams struct {
	binary bool // BODY=BINARYMIME
	utf8   bool // SMTPUTF8
}

func (s *Session) checkMailParams(arg string) (mp mailParams, code int, msg string) {
	for _, field := range mailParamFields(arg) {
		key, val, _ := strings.Cut(field, "=")
		switch strings.ToUpper(key) {
		case "SIZE":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil || n < 0 {
				return mp, 501, "5.5.4 Invalid SIZE parameter"
			}
			if max := s.server.config.maxMessageSize(); n > max {
				return mp, 552, "5.3.4 Message size exceeds fixed maximum message size"
			}
		case "BODY":
			// 8BITMIME and BINARYMIME are advertised in EHLO (BINARYMIME rides
			// on CHUNKING and is only usable with BDAT).
			switch strings.ToUpper(val) {
			case "7BIT", "8BITMIME":
			case "BINARYMIME":
				mp.binary = true
			default:
				return mp, 501, "5.5.4 Invalid BODY parameter"
			}
		case "RET":
			if v := strings.ToUpper(val); v != "FULL" && v != "HDRS" {
				return mp, 501, "5.5.4 Invalid RET parameter"
			}
		case "ENVID", "AUTH":
			if val == "" {
				return mp, 501, "5.5.4 Invalid " + strings.ToUpper(key) + " parameter"
			}
		case "SMTPUTF8":
			if val != "" {
				return mp, 501, "5.5.4 Invalid SMTPUTF8 parameter"
			}
			mp.utf8 = true
		default:
			// Includes REQUIRETLS and MT-PRIORITY, which are not advertised.
			return mp, 555, "5.5.4 Unsupported MAIL FROM parameter"
		}
	}
	return mp, 0, ""
}

// checkRcptParams validates the RCPT TO parameters of RFC 3461: NOTIFY
// (NEVER, or a list of SUCCESS/FAILURE/DELAY; NEVER excludes the others) and
// ORCPT (addr-type;address). It returns a reply code and text, or 0 (F5673).
func checkRcptParams(arg string) (int, string) {
	for _, field := range mailParamFields(arg) {
		key, val, _ := strings.Cut(field, "=")
		switch strings.ToUpper(key) {
		case "NOTIFY":
			items := strings.Split(strings.ToUpper(val), ",")
			for _, it := range items {
				if it != "NEVER" && it != "SUCCESS" && it != "FAILURE" && it != "DELAY" {
					return 501, "5.5.4 Invalid NOTIFY parameter"
				}
				if it == "NEVER" && len(items) > 1 {
					return 501, "5.5.4 NOTIFY=NEVER cannot be combined with other values"
				}
			}
		case "ORCPT":
			if t, a, ok := strings.Cut(val, ";"); !ok || t == "" || a == "" {
				return 501, "5.5.4 Invalid ORCPT parameter"
			}
		default:
			return 555, "5.5.4 Unsupported RCPT TO parameter"
		}
	}
	return 0, ""
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

	if code, msg := checkRcptParams(arg); code != 0 {
		return s.WriteResponse(code, msg)
	}
	if !isASCII(validated) && !s.smtputf8 {
		return s.WriteResponse(553, "5.6.7 Non-ASCII address requires SMTPUTF8")
	}
	rawFor := receivedForm(validated)
	validated = normalizeAddr(validated)

	// Check max recipients
	if len(s.rcptTo) >= s.server.config.maxRecipients() {
		return s.WriteResponse(452, "4.5.3 Too many recipients")
	}

	// An unauthenticated client may only send to local domains (F6042).
	if !s.isAuth && s.server.onLocalDomain != nil {
		domain := strings.ToLower(strings.TrimSuffix(validated[strings.LastIndexByte(validated, '@')+1:], "."))
		if domain == "" || !s.server.onLocalDomain(domain) {
			return s.WriteResponse(554, "5.7.1 Relay access denied")
		}
	}

	// Apply the envelope policy installed with SetValidateHandler. (F4907)
	if s.server.onValidate != nil {
		if err := s.server.onValidate(s.mailFrom, []string{validated}); err != nil {
			return s.WriteResponse(550, "5.7.1 Recipient rejected")
		}
	}

	if s.isAuth && !s.server.allowUserRecipient(s.username) {
		return s.WriteResponse(452, "4.5.3 Hourly recipient limit exceeded")
	}

	s.rcptTo = append(s.rcptTo, validated)
	s.rcptRaw = append(s.rcptRaw, rawFor)
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
		_, span = s.server.tracingProvider.StartSpanWithKind(ctx, "smtp.data", tracing.SpanKindServer,
			attribute.String("session.id", s.id),
			attribute.String("mail.from", s.mailFrom),
			attribute.Int("mail.recipients", len(s.rcptTo)),
			attribute.Bool("session.tls", s.isTLS),
		)
		defer span.End()
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have RCPT TO first; DATA may not follow a BDAT chunk of the same
	// transaction (RFC 3030 §3, F5676).
	if s.state != StateRcptTo || s.bdatBuffer != nil || s.mailBinary {
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
		s.resetTransaction()
		if errors.Is(err, errSessionBytes) {
			_ = s.WriteResponse(421, "4.7.0 Session data limit exceeded, closing connection")
			return ErrSessionQuit
		}
		if errors.Is(err, errMessageTooLarge) {
			return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			_ = s.WriteResponse(421, "4.4.2 Timeout waiting for message data")
			return ErrSessionQuit
		}
		if errors.Is(err, errBareCR) {
			return s.WriteResponse(554, "5.6.0 Message contains bare CR")
		}
		return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
	}

	// Check message size
	if int64(len(data)) > s.server.config.maxMessageSize() {
		s.resetTransaction()
		return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
	}

	s.data = data

	// Run message through pipeline if configured
	var pctx *MessageContext
	if s.server.pipeline != nil {
		ctx := NewMessageContext(s.clientIP(), s.mailFrom, s.rcptTo, data)
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

		result, err := s.server.pipeline.ProcessWithTimeout(ctx, s.server.config.StageTimeout)
		if errors.Is(err, errStageTimeout) {
			// The stuck stage may still be writing ctx: do not read it.
			s.resetTransaction()
			return s.WriteResponse(451, "4.4.7 Message processing timed out, try again later")
		}
		// Process reports a stage rejection as ResultReject with a non-nil
		// error; answer it with the stage's code below, not 451. (F4945)
		if err != nil && result != ResultReject {
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
		data = s.applyJunkVerdict(ctx, result, data)
		pctx = ctx
	}

	// Trace and result headers are added the same way for DATA and BDAT,
	// with or without a pipeline. (F5055)
	data = s.addTraceHeaders(pctx, data)
	s.data = data

	// Deliver message via deliver
	if err := s.deliver(pctx); err != nil {
		s.resetTransaction()
		code, msg := deliveryReply(err)
		return s.WriteResponse(code, msg)
	}

	s.resetTransaction()
	s.errCount.Store(0)
	return s.WriteResponse(250, "OK")
}

// deliver hands the accepted message in s.data to the most capable delivery
// handler that is set: WithAuth, WithNotify, WithSieve, then the plain one.
// pctx is the pipeline context, nil when no pipeline ran.
func (s *Session) deliver(pctx *MessageContext) error {
	srv := s.server
	switch {
	case srv.onDeliverWithAuth != nil:
		info := &DeliveryAuth{SieveActions: s.sieveActions}
		if pctx != nil {
			info.AuthResults = s.authResultsValue(pctx)
			info.SPF, info.DKIM, info.DMARC, info.ARC = pctx.SPFResult, pctx.DKIMResult, pctx.DMARCResult, pctx.ARCResult
		}
		return srv.onDeliverWithAuth(s.mailFrom, s.rcptTo, s.rcptToNotify, s.data, info)
	case srv.onDeliverWithNotify != nil:
		return srv.onDeliverWithNotify(s.mailFrom, s.rcptTo, s.rcptToNotify, s.data)
	case srv.onDeliverWithSieve != nil:
		return srv.onDeliverWithSieve(s.mailFrom, s.rcptTo, s.data, s.sieveActions)
	case srv.onDeliver != nil:
		return srv.onDeliver(s.mailFrom, s.rcptTo, s.data)
	}
	return nil
}

// applyJunkVerdict passes a ScoreStage "junk" verdict on to the delivery
// handler, which otherwise cannot tell junk from inbox mail (F4946): the
// message is flagged with X-Spam-Status (unless quarantine already added it)
// and, when no sieve rule chose a folder, a fileinto:Junk action is added.
func (s *Session) applyJunkVerdict(ctx *MessageContext, result PipelineResult, data []byte) []byte {
	if ctx.SpamResult.Verdict != "junk" {
		return data
	}
	if result != ResultQuarantine {
		data = append([]byte(fmt.Sprintf("X-Spam-Status: Yes, score=%.1f\r\n", ctx.SpamScore)), data...)
	}
	for _, a := range s.sieveActions {
		if strings.HasPrefix(a, "fileinto:") {
			return data
		}
	}
	s.sieveActions = append(append([]string(nil), s.sieveActions...), "fileinto:Junk")
	return data
}

// clientIP returns the peer IP of the session, or 0.0.0.0 for Unix sockets
// and unparsable addresses.
func (s *Session) clientIP() net.IP {
	var remoteIP net.IP
	if host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String()); err == nil {
		remoteIP = net.ParseIP(host)
	}
	if remoteIP == nil {
		remoteIP = net.IPv4zero
	}
	return remoteIP
}

// addTraceHeaders prepends the headers this server adds to an accepted
// message, whichever of DATA or BDAT carried it (F5055): when a pipeline ran
// (ctx non-nil) Authentication-Results, X-Spam-Score and the RFC 5321 §4.4
// Received trace header, and in every case a Message-ID if the header block
// has none. Authentication-Results fields that already claim this
// server's authserv-id are forged and are removed first (RFC 8601 §5, F5059).
func (s *Session) addTraceHeaders(ctx *MessageContext, data []byte) []byte {
	hostname := s.server.config.Hostname
	if hostname == "" {
		hostname = "localhost"
	}
	data = removeOwnAuthResults(data, hostname)

	// A submitting client's own Received fields would forge the origin of the
	// trace; the first hop is this server and it records the true origin
	// (RFC 6409 §8.1, F6153).
	if s.isAuth || s.server.config.IsSubmission {
		data = stripHeaderFields(data, "received")
	}

	// Message-ID and Date are inserted first so that the trace fields added
	// below end up above them: Received must be the topmost field (RFC 5321 §4.4, F6153).
	if s.isAuth || s.server.config.IsSubmission {
		data = sanitizeSubmissionHeaders(data)
	}

	// Add Message-ID if not present. The search must be scoped to the header
	// block: a "message-id:" occurring in the body is quoted text (ordinary
	// in forwards and replies), not this message's identifier, and must not
	// suppress the header. RFC 5322 §3.6.4.
	headerScope := data
	if idx := bytes.Index(data, []byte("\r\n\r\n")); idx >= 0 {
		headerScope = data[:idx]
	}
	if !bytes.Contains(bytes.ToLower(headerScope), []byte("message-id:")) {
		msgID := fmt.Sprintf("Message-ID: <%s@%s>\r\n", uuid.New().String(), hostname)
		data = append([]byte(msgID), data...)
	}
	if ctx != nil {
		// Add Authentication-Results header with SPF/DKIM/DMARC/ARC results
		if ar := s.authResultsValue(ctx); ar != "" {
			arHeader := fmt.Sprintf("Authentication-Results: %s;\r\n\t%s\r\n", hostname, ar)
			data = append([]byte(arHeader), data...)
		}

		// Add X-Spam headers for all messages processed by pipeline
		if ctx.SpamResult.Score > 0 {
			spamScoreHeader := fmt.Sprintf("X-Spam-Score: %.1f\r\n", ctx.SpamResult.Score)
			data = append([]byte(spamScoreHeader), data...)
		}

		// Add Received trace header
		// RFC 3848: an authenticated session is ESMTPA / ESMTPSA (F6155).
		proto := "ESMTP"
		switch {
		case s.isTLS && s.isAuth:
			proto = "ESMTPSA"
		case s.isTLS:
			proto = "ESMTPS"
		case s.isAuth:
			proto = "ESMTPA"
		}
		// The "for" clause names a recipient only when there is exactly one:
		// with several it would disclose one (possibly Bcc) recipient to all
		// the others (RFC 5321 §4.4, F5674). An IPv6 peer is written as an
		// address literal (RFC 5321 §4.1.3, F5678).
		ipLit := s.clientIP().String()
		if s.clientIP().To4() == nil {
			ipLit = "IPv6:" + ipLit
		}
		var received string
		forAddr := ""
		if len(s.rcptTo) == 1 {
			forAddr = s.rcptTo[0]
			if len(s.rcptRaw) == 1 {
				forAddr = s.rcptRaw[0]
			}
		}
		if len(s.rcptTo) == 1 {
			received = fmt.Sprintf("Received: from %s ([%s]) by %s with %s for <%s>; %s\r\n",
				s.helloDomain, ipLit, hostname, proto, forAddr,
				time.Now().Format(time.RFC1123Z))
		} else {
			received = fmt.Sprintf("Received: from %s ([%s]) by %s with %s; %s\r\n",
				s.helloDomain, ipLit, hostname, proto,
				time.Now().Format(time.RFC1123Z))
		}
		data = append([]byte(received), data...)
	}

	return data
}

// stripHeaderFields removes every header field named name (lower case),
// continuation lines included, from the header block of data.
func stripHeaderFields(data []byte, name string) []byte {
	hdrEnd := len(data)
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		hdrEnd = i + 2
	}
	var out []byte
	dropping := false
	for _, line := range bytes.SplitAfter(data[:hdrEnd], []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			dropping = false
			if c := bytes.IndexByte(line, ':'); c > 0 &&
				strings.ToLower(strings.TrimRight(string(line[:c]), " \t")) == name {
				dropping = true
			}
		}
		if !dropping {
			out = append(out, line...)
		}
	}
	return append(out, data[hdrEnd:]...)
}

// sanitizeSubmissionHeaders prepares a client-submitted message: Bcc header
// fields (with continuation lines) are removed so that no recipient sees the
// blind-copy list (RFC 5322 §3.6.3, F6043), and a Date header is added when
// missing (RFC 5322 §3.6.1).
func sanitizeSubmissionHeaders(data []byte) []byte {
	hdrEnd := len(data)
	body := []byte(nil)
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		hdrEnd, body = i+2, data[i+2:]
	} else if bytes.HasSuffix(data, []byte("\r\n")) {
		hdrEnd = len(data)
	}
	var out []byte
	hasDate, dropping, sawHeader := false, false, false
	for _, line := range bytes.SplitAfter(data[:hdrEnd], []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			dropping = false
			if c := bytes.IndexByte(line, ':'); c > 0 {
				sawHeader = true
				switch strings.ToLower(strings.TrimRight(string(line[:c]), " \t")) {
				case "bcc":
					dropping = true
				case "date":
					hasDate = true
				}
			}
		}
		if !dropping {
			out = append(out, line...)
		}
	}
	if sawHeader && !hasDate {
		out = append([]byte("Date: "+time.Now().Format(time.RFC1123Z)+"\r\n"), out...)
	}
	return append(out, body...)
}

// arcSafe makes a result string safe for a quoted-string in an
// Authentication-Results field: control characters (CR/LF would inject
// headers) become spaces and quote/backslash are escaped.
func arcSafe(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteByte(' ')
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// authorDomain returns the RFC 5322 From domain of the message, which DMARC
// aligns against and which Authentication-Results header.from must name,
// falling back to the envelope sender's domain.
func authorDomain(ctx *MessageContext, envFrom string) string {
	for name, values := range ctx.Headers {
		if strings.EqualFold(name, "From") && len(values) > 0 {
			if a, err := mail.ParseAddress(values[0]); err == nil {
				if i := strings.LastIndex(a.Address, "@"); i > 0 {
					return strings.ToLower(a.Address[i+1:])
				}
			}
			return ""
		}
	}
	return strings.ToLower(extractDomain(envFrom))
}

// authResultsValue renders the resinfo part of the Authentication-Results
// header (everything after "authserv-id;") for the pipeline verdicts, or ""
// when there are none. The ARC verdict is included as the RFC 8617 "arc"
// method so that it survives in the stored message and a later forwarder can
// recover the cv it must seal with (F5942).
func (s *Session) authResultsValue(ctx *MessageContext) string {
	var parts []string
	if ctx.SPFResult.Result != "" {
		parts = append(parts, fmt.Sprintf("spf=%s smtp.mailfrom=%s", ctx.SPFResult.Result, ctx.SPFResult.Domain))
	}
	if ctx.DKIMResult.Domain != "" {
		if ctx.DKIMResult.Valid {
			parts = append(parts, fmt.Sprintf("dkim=pass header.d=%s", ctx.DKIMResult.Domain))
		} else {
			reason := ctx.DKIMResult.Error
			if reason == "" {
				reason = "verification failed"
			}
			parts = append(parts, fmt.Sprintf("dkim=fail reason=\"%s\" header.d=%s", arcSafe(reason), ctx.DKIMResult.Domain))
		}
	}
	if ctx.DMARCResult.Result != "" {
		p := "dmarc=" + ctx.DMARCResult.Result
		if d := authorDomain(ctx, s.mailFrom); d != "" {
			p += " header.from=" + d
		}
		parts = append(parts, p)
	}
	if ctx.ARCResult.Result != "" {
		parts = append(parts, "arc="+ctx.ARCResult.Result)
	}
	return strings.Join(parts, ";\r\n\t")
}

// removeOwnAuthResults returns data without the Authentication-Results header
// fields (with their continuation lines) whose authserv-id is hostname. Only
// this server may issue results under its own id; a sender-supplied one is a
// forgery (RFC 8601 §5, F5059). data itself is not modified.
func removeOwnAuthResults(data []byte, hostname string) []byte {
	var out []byte
	changed, dropping := false, false
	for start := 0; start < len(data); {
		end := bytes.IndexByte(data[start:], '\n')
		if end < 0 {
			end = len(data)
		} else {
			end += start + 1
		}
		line := data[start:end]
		content := bytes.TrimRight(line, "\r\n")
		if len(content) == 0 {
			// End of the header block: the body is kept as is.
			if !changed {
				return data
			}
			return append(out, data[start:]...)
		}
		if content[0] != ' ' && content[0] != '\t' {
			dropping = false
			if colon := bytes.IndexByte(content, ':'); colon > 0 &&
				strings.EqualFold(strings.TrimRight(string(content[:colon]), " \t"), "Authentication-Results") {
				dropping = authServID(data[start+colon+1:]) == strings.ToLower(hostname)
			}
		}
		if dropping {
			if !changed {
				out = append(make([]byte, 0, len(data)), data[:start]...)
				changed = true
			}
		} else if changed {
			out = append(out, line...)
		}
		start = end
	}
	if !changed {
		return data
	}
	return out
}

// authServID returns the lower-cased authserv-id of an Authentication-Results
// field value: the first token before ';', which may be folded across lines.
func authServID(value []byte) string {
	if semi := bytes.IndexByte(value, ';'); semi >= 0 {
		value = value[:semi]
	}
	fields := strings.Fields(string(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// setReadDeadline sets the connection read deadline; a session without a
// connection (unit-test construction) has none to set.
func (s *Session) setReadDeadline(t time.Time) {
	if s.conn != nil {
		_ = s.conn.SetReadDeadline(t)
	}
}

// errBareCR is returned by readData for a message containing a bare CR.
var errBareCR = errors.New("message contains bare CR")

// hasBareCR reports whether line (one CRLF/LF-terminated line) has a CR that
// is not immediately followed by LF.
func hasBareCR(line []byte) bool {
	for i, b := range line {
		if b == '\r' && (i+1 >= len(line) || line[i+1] != '\n') {
			return true
		}
	}
	return false
}

// errMessageTooLarge is returned by readData when the message exceeds the size limit
var errMessageTooLarge = errors.New("message too large")

// readData reads the email message data from the connection
func (s *Session) readData() ([]byte, error) {
	// Read through the session's buffered reader so message bytes already
	// consumed by its read-ahead (PIPELINING: the client may send the whole
	// transaction, message included, in one TCP segment) are visible here.
	// A fresh bufio.Reader over the raw conn cannot see them and stalls
	// until ReadTimeout aborts the message.
	reader := s.reader
	if reader == nil {
		reader = bufio.NewReader(s.conn)
	}
	var data []byte
	const maxLineLength = 1000 // RFC 5322: max 1000 bytes per line including CRLF

	// A content error (overlong line, NUL, size limit) must not abandon the
	// message mid-stream: the unread remainder would then be executed as
	// SMTP commands. Record the first error, keep consuming (and discarding)
	// lines up to the end-of-data indicator, and report it there. (F4905)
	var contentErr error
	// Dot handling applies only at the start of a line, i.e. after <CRLF>:
	// <LF>.<CRLF> is not the end-of-data indicator (RFC 5321 §4.1.1.4), and
	// treating it as one enables SMTP smuggling. (F4906)
	atLineStart := true

	// Absolute DATA deadline: the per-line deadline below is renewed by every
	// line, so a client dripping one byte per interval would otherwise hold
	// the session forever (F6150).
	dataDeadline := time.Now().Add(s.server.config.dataTimeout())
	defer s.setReadDeadline(time.Time{})

	for {
		lineDeadline := dataDeadline
		if rt := s.server.config.ReadTimeout; rt > 0 {
			if d := time.Now().Add(rt); d.Before(lineDeadline) {
				lineDeadline = d
			}
		}
		s.setReadDeadline(lineDeadline)

		// Read through a bounded reader: ReadBytes would buffer an
		// unterminated line without limit, defeating the size and line
		// limits below (F5671).
		line, total, endsCRLF, err := readBoundedLine(reader, maxLineLength+1)
		if err != nil {
			return nil, err
		}
		if s.bytesIn.Load() > s.server.config.maxSessionBytes() {
			return nil, errSessionBytes
		}
		dotLine := atLineStart && len(line) > 0 && line[0] == '.'
		atLineStart = endsCRLF

		// Check for end of data marker
		if dotLine && total == 3 && line[1] == '\r' && line[2] == '\n' {
			break
		}
		if contentErr != nil {
			continue
		}

		// RFC 5322 line length limit check
		lineLength := total
		if dotLine {
			// The extra transparency dot does not count toward the line limit.
			lineLength--
		}
		if lineLength > maxLineLength {
			contentErr = fmt.Errorf("line exceeds maximum length of %d bytes", maxLineLength)
			continue
		}

		// Check for null bytes (security: prevent header injection)
		if bytes.Contains(line, []byte{0}) {
			contentErr = fmt.Errorf("message contains null bytes")
			continue
		}

		// A CR that is not half of a CRLF is a line break to some parsers
		// and not to others: the SMTP smuggling family. Refuse it (F6151).
		if hasBareCR(line) {
			contentErr = errBareCR
			continue
		}

		// Remove dot-stuffing (leading dot is doubled)
		if dotLine {
			line = line[1:]
		}

		// A bare LF ends the line like CRLF for every consumer except the
		// header parsers, which split on CRLF: normalise it so stages see
		// the headers the recipient will (F5670).
		if n := len(line); n > 0 && line[n-1] == '\n' && (n == 1 || line[n-2] != '\r') {
			data = append(data, line[:n-1]...)
			data = append(data, '\r', '\n')
		} else {
			data = append(data, line...)
		}

		// Check accumulated size during read to prevent memory exhaustion
		if int64(len(data)) > s.server.config.maxMessageSize() {
			contentErr = fmt.Errorf("%w: message exceeds maximum size of %d bytes", errMessageTooLarge, s.server.config.maxMessageSize())
			data = nil
		}
	}

	if contentErr != nil {
		return nil, contentErr
	}
	return data, nil
}

// readBoundedLine reads one line terminated by '\n' from r without buffering
// more than keep bytes of it: the rest is consumed and dropped. It returns the
// retained prefix, the full length of the line and whether it ended in CRLF.
// An unterminated line of any length therefore costs O(keep) memory (F5671).
func readBoundedLine(r *bufio.Reader, keep int) (line []byte, total int, endsCRLF bool, err error) {
	var prev byte
	for {
		chunk, e := r.ReadSlice('\n')
		if room := keep - len(line); room > 0 {
			if room > len(chunk) {
				room = len(chunk)
			}
			line = append(line, chunk[:room]...)
		}
		total += len(chunk)
		switch {
		case e == nil:
			n := len(chunk)
			endsCRLF = (n >= 2 && chunk[n-2] == '\r') || (n == 1 && prev == '\r')
			return line, total, endsCRLF, nil
		case errors.Is(e, bufio.ErrBufferFull):
			prev = chunk[len(chunk)-1]
		default:
			return nil, total, false, e
		}
	}
}

// normalizeBareLF returns data with every LF not preceded by CR turned into CRLF.
func normalizeBareLF(data []byte) []byte {
	var out []byte
	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			if out == nil {
				out = append(make([]byte, 0, len(data)+16), data[:i]...)
			}
			out = append(out, '\r', '\n')
		} else if out != nil {
			out = append(out, b)
		}
	}
	if out == nil {
		return data
	}
	return out
}

// handleBDAT handles the BDAT command (RFC 3030)
func (s *Session) handleBDAT(arg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Must have RCPT TO first
	if s.state != StateRcptTo {
		werr := s.WriteResponse(503, "5.5.1 Bad sequence of commands")
		if parts := strings.Fields(arg); len(parts) > 0 {
			if size, err := strconv.Atoi(parts[0]); err == nil {
				if derr := s.discardBDATChunk(size); derr != nil && werr == nil {
					werr = derr
				}
			}
		}
		return werr
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

	// Check cumulative size against limit. Compare by subtraction rather than by
	// adding bufLen+size: a large declared chunk size would overflow that sum
	// (in int or in int64) to a negative value and defeat the guard, letting the
	// make() below run with an attacker-chosen length. bdatBuffer.Len() is always
	// <= MaxMessageSize because every previous chunk passed this same check, so
	// the subtraction cannot underflow.
	if int64(size) > s.server.config.maxMessageSize()-int64(s.bdatBuffer.Len()) {
		s.bdatBuffer = nil
		s.resetTransaction()
		if err := s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size"); err != nil {
			return err
		}
		return s.discardBDATChunk(size)
	}

	// Read chunk data
	if size > 0 {
		// Read through the session's buffered reader so chunk octets already
		// consumed by its read-ahead (CHUNKING + PIPELINING: RFC 3030 allows
		// BDAT pipelined with MAIL/RCPT, so the client may send the chunk
		// size line, payload, and next command in one TCP segment) are
		// visible here. A fresh io.ReadFull over the raw conn cannot see
		// them and stalls until ReadTimeout aborts the session.
		reader := s.reader
		if reader == nil {
			reader = bufio.NewReader(s.conn)
		}
		// Grow the buffer as octets arrive instead of allocating the
		// declared size up front (F5675).
		s.setReadDeadline(time.Now().Add(s.server.config.dataTimeout()))
		if _, err := io.CopyN(s.bdatBuffer, reader, int64(size)); err != nil {
			return fmt.Errorf("failed to read BDAT chunk: %w", err)
		}
		if s.bytesIn.Load() > s.server.config.maxSessionBytes() {
			s.resetTransaction()
			_ = s.WriteResponse(421, "4.7.0 Session data limit exceeded, closing connection")
			return ErrSessionQuit
		}
	}

	if isLast {
		// Final chunk — process the complete message
		raw := s.bdatBuffer.Bytes()
		s.bdatBuffer = nil
		data := raw
		// BODY=BINARYMIME carries arbitrary octets: bare CR/LF and NUL are
		// content there (RFC 3030 §3), not line-ending ambiguity.
		if !s.mailBinary {
			if hasBareCR(raw) { // F6151
				s.resetTransaction()
				return s.WriteResponse(554, "5.6.0 Message contains bare CR")
			}
			data = normalizeBareLF(raw) // F5670
		}

		// Check total message size
		if int64(len(data)) > s.server.config.maxMessageSize() {
			s.resetTransaction()
			return s.WriteResponse(552, "5.2.3 Message exceeds fixed maximum message size")
		}

		s.data = data

		// Run through pipeline if configured
		var pctx *MessageContext
		if s.server.pipeline != nil {
			ctx := NewMessageContext(s.clientIP(), s.mailFrom, s.rcptTo, data)
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

			result, err := s.server.pipeline.ProcessWithTimeout(ctx, s.server.config.StageTimeout)
			if errors.Is(err, errStageTimeout) {
				s.resetTransaction()
				return s.WriteResponse(451, "4.4.7 Message processing timed out, try again later")
			}
			if err != nil && result != ResultReject { // F4945
				s.resetTransaction()
				return s.WriteResponse(451, "4.4.0 Requested action aborted: local error in processing")
			}

			s.sieveActions = ctx.SpamResult.Reasons

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
			data = s.applyJunkVerdict(ctx, result, data)
			pctx = ctx
		}

		// BDAT gets the same trace and result headers as DATA. (F5055)
		data = s.addTraceHeaders(pctx, data)
		s.data = data

		// Deliver message via deliver
		if err := s.deliver(pctx); err != nil {
			s.resetTransaction()
			code, msg := deliveryReply(err)
			return s.WriteResponse(code, msg)
		}

		s.resetTransaction()
		s.errCount.Store(0)
		return s.WriteResponse(250, "2.0.0 OK")
	}

	// Non-last chunk — acknowledge and wait for more
	return s.WriteResponse(250, "2.0.0 OK")
}

// discardBDATChunk consumes, without storing, the size octets that follow a
// BDAT command the server refused. They are message content, never commands:
// leaving them unread would make the command loop execute the chunk as SMTP
// commands (RFC 3030 §2; F5058, the BDAT counterpart of F4905).
func (s *Session) discardBDATChunk(size int) error {
	if size <= 0 {
		return nil
	}
	reader := s.reader
	if reader == nil {
		reader = bufio.NewReader(s.conn)
	}
	if s.server.config.ReadTimeout > 0 {
		_ = s.conn.SetReadDeadline(time.Now().Add(s.server.config.ReadTimeout))
	}
	if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
		return fmt.Errorf("failed to discard refused BDAT chunk: %w", err)
	}
	return nil
}

// handleRSET handles the RSET command
func (s *Session) handleRSET() error {
	s.mutex.Lock()
	s.resetTransaction()
	s.mutex.Unlock()

	return s.WriteResponse(250, "OK")
}

// handleVRFY handles the VRFY command. The answer never depends on whether the
// argument names an account, so it cannot be used to harvest addresses
// (RFC 5321 §3.5.3, §7.3): a syntactically valid request is 252, an empty one
// 501.
func (s *Session) handleVRFY(arg string) error {
	if strings.TrimSpace(arg) == "" {
		return s.WriteResponse(501, "5.5.4 Syntax error in parameters or arguments")
	}
	return s.WriteResponse(252, "2.5.0 Cannot VRFY user, but will accept message and attempt delivery")
}

// handleEXPN handles the EXPN command: list expansion is not provided, and
// the reply is the same whatever the argument.
func (s *Session) handleEXPN(arg string) error {
	return s.WriteResponse(502, "5.5.1 EXPN not implemented")
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
		_, span = s.server.tracingProvider.StartSpanWithKind(ctx, "smtp.auth", tracing.SpanKindServer,
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

	// RFC 4954 §4: AUTH is only valid before a mail transaction starts.
	// Reject it when no EHLO/HELO has been seen yet (StateNew) and while a
	// transaction is in progress (after MAIL). After a completed
	// transaction resetTransaction returns the session to StateGreeted, so
	// AUTH remains valid there.
	if s.state != StateGreeted {
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
		if s.server.onGetPassword == nil {
			// Not advertised without a password source (F5321): refuse up
			// front as unsupported, without charging the lockout counter.
			if span != nil {
				tracing.SetStatus(span, tracing.StatusError, "SCRAM-SHA-256 unavailable")
			}
			return s.WriteResponse(504, "5.5.4 Unrecognized authentication type")
		}
		return s.handleAuthSCRAMSHA256(parts)
	default:
		if span != nil {
			tracing.SetStatus(span, tracing.StatusError, "unrecognized mechanism")
		}
		return s.WriteResponse(504, "Unrecognized authentication type")
	}
}

// maxAuthLine bounds an AUTH continuation response (RFC 4954 §4: 12288).
const maxAuthLine = 12288

// errAuthAborted is returned once a reply to a failed continuation read has
// already been written.
var errAuthAborted = errors.New("authentication exchange aborted")

// readAuthLine reads one SASL continuation line without buffering more than
// maxAuthLine bytes (an unbounded ReadString let a pre-auth client grow server
// memory, F6157) and honours the "*" cancel response (RFC 4954 §4, F6158).
func (s *Session) readAuthLine(r *bufio.Reader) (string, error) {
	line, total, _, err := readBoundedLine(r, maxAuthLine+1)
	if err != nil {
		return "", err
	}
	if total > maxAuthLine {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		_ = s.WriteResponse(501, "5.5.4 Authentication response too long")
		return "", errAuthAborted
	}
	text := strings.TrimSpace(string(line))
	if text == "*" {
		_ = s.WriteResponse(501, "5.0.0 Authentication cancelled")
		return "", errAuthAborted
	}
	return text, nil
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

		reader := s.reader
		if reader == nil {
			reader = bufio.NewReader(s.conn)
		}
		line, err := s.readAuthLine(reader)
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

	// An authzid other than the authenticating identity asks to act as
	// someone else; no proxy authorization is implemented, so refuse it
	// rather than silently dropping it (RFC 4616 §2, F6158).
	if authzid := credParts[0]; authzid != "" && strings.ToLower(authzid) != usernameNormalized {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(535, "5.7.8 Authorization identity not permitted")
	}

	// Authenticate
	// Fail closed: with no auth handler wired no credential is valid (F5946).
	{
		var ok bool
		var err error
		if s.server.onAuth != nil {
			ok, err = s.server.onAuth(usernameNormalized, password)
		}
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
		s.server.onLoginResult(usernameNormalized, true, getIPFromAddr(s.conn.RemoteAddr().String()), "")
	}

	return s.WriteResponse(235, "Authentication successful")
}

// handleAuthLOGIN handles LOGIN authentication
func (s *Session) handleAuthLOGIN(parts []string) error {
	reader := s.reader
	if reader == nil {
		reader = bufio.NewReader(s.conn)
	}

	// The user name may arrive as the initial response ("AUTH LOGIN <b64>");
	// only prompt for it when it did not (F5322).
	var usernameEnc string
	if len(parts) > 1 {
		usernameEnc = strings.TrimSpace(parts[1])
	} else {
		if err := s.WriteResponse(334, "VXNlcm5hbWU6"); err != nil { // base64("Username:")
			return err
		}
		line, err := s.readAuthLine(reader)
		if err != nil {
			return err
		}
		usernameEnc = strings.TrimSpace(line)
	}

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
	line, err := s.readAuthLine(reader)
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
	// Fail closed: with no auth handler wired no credential is valid (F5946).
	{
		var ok bool
		var err error
		if s.server.onAuth != nil {
			ok, err = s.server.onAuth(usernameNormalized, password)
		}
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
		s.server.onLoginResult(usernameNormalized, true, getIPFromAddr(s.conn.RemoteAddr().String()), "")
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

		reader := s.reader
		if reader == nil {
			reader = bufio.NewReader(s.conn)
		}
		line, err := s.readAuthLine(reader)
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
	// client-first-message-bare follows the GS2 header "<flag>,[a=authzid],"
	// (RFC 5802 §7); it, not the whole message, enters AuthMessage (F5320).
	gs2 := strings.SplitN(clientFirst, ",", 3)
	if len(gs2) != 3 {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid SCRAM-SHA-256 client-first message")
	}
	clientFirstBare := gs2[2]

	username := clientFirstMsg.AuthCID
	if username == "" {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Missing username in SCRAM-SHA-256")
	}
	// Canonical identity for lookups, session state, and audit callbacks —
	// the same UsernameCaseMapped normalization PLAIN and LOGIN apply.
	usernameNormalized := strings.ToLower(username)

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
	reader := s.reader
	if reader == nil {
		reader = bufio.NewReader(s.conn)
	}
	line, err := s.readAuthLine(reader)
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
	// handleAUTH only dispatches here when onGetPassword is set (F5321).
	password, err := s.server.onGetPassword(usernameNormalized)
	if err != nil {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(535, "5.5.4 Authentication failed")
	}

	// Create SCRAM authenticator with the password
	scram, err := auth.NewSCRAMSHA256(password, salt, iterations)
	if err != nil {
		return s.WriteResponse(501, "5.5.4 Server error in SCRAM-SHA-256")
	}

	// RFC 5802 §3 (F5320): AuthMessage = client-first-message-bare ","
	// server-first-message "," client-final-message-without-proof, and the
	// proof is ClientKey XOR ClientSignature. The server recovers ClientKey
	// and checks H(ClientKey) == StoredKey.
	proofAt := strings.LastIndex(clientFinal, ",p=")
	if proofAt < 0 {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		return s.WriteResponse(501, "5.5.4 Invalid SCRAM-SHA-256 client-final message")
	}
	clientFinalWithoutProof := clientFinal[:proofAt]
	clientSig := auth.ClientProof(scram.StoredKey(), clientFirstBare, serverFirst, clientFinalWithoutProof)
	proofOK := len(clientFinalMsg.ClientProof) == len(clientSig)
	if proofOK {
		clientKey := sha256.Sum256(auth.XORBytes(clientFinalMsg.ClientProof, clientSig))
		proofOK = hmac.Equal(clientKey[:], scram.StoredKey())
	}
	if !proofOK {
		s.server.recordAuthFailure(getIPFromAddr(s.conn.RemoteAddr().String()))
		if s.server.onLoginResult != nil {
			s.server.onLoginResult(usernameNormalized, false, getIPFromAddr(s.conn.RemoteAddr().String()), "invalid_credentials")
		}
		return s.WriteResponse(535, "5.5.4 Authentication credentials invalid")
	}

	serverSig := auth.ServerSignature(scram.ServerKey(), clientFirstBare, serverFirst, clientFinalWithoutProof)

	// Authentication successful
	s.isAuth = true
	s.username = usernameNormalized
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

	if !s.server.tlsAvailable() {
		return s.WriteResponse(454, "4.7.0 TLS not available")
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
		s.reader.Reset(&countingReader{r: tlsConn, n: &s.bytesIn})
	}

	// Reset state after TLS upgrade (RFC 3207 Section 4.1)
	s.state = StateNew
	s.resetTransaction()
	s.isAuth = false
	s.username = ""
	s.helloDomain = "" // the client must greet again; stale name must not reach Received (F6159)

	return nil
}

// resetTransaction resets the transaction state
func (s *Session) resetTransaction() {
	s.mailFrom = ""
	s.rcptTo = make([]string, 0)
	s.rcptRaw = nil
	s.mailBinary = false
	s.smtputf8 = false
	s.rcptToNotify = make([]string, 0) // Clear per-recipient DSN NOTIFY preferences
	s.data = nil
	s.bdatBuffer = nil
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
	for i, part := range strings.Fields(arg) {
		if i == 0 {
			continue
		}
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "RET=") {
			ret = strings.TrimPrefix(upper, "RET=")
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
	for i, part := range strings.Fields(arg) {
		if i == 0 {
			continue
		}
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "NOTIFY=") {
			notify = strings.TrimPrefix(upper, "NOTIFY=")
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
