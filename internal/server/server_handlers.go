package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/metrics"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/storage"
	"github.com/umailserver/umailserver/internal/tracing"
	"github.com/umailserver/umailserver/internal/webhook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"
)

// authenticate validates user credentials
func (s *Server) authenticate(username, password string) (bool, error) {
	// Create tracing span if tracing is enabled
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		ctx, span := s.tracingProvider.StartSpanWithKind(context.Background(), "authenticate", tracing.SpanKindServer,
			attribute.String("auth.username", username),
		)
		defer span.End()
		_ = ctx // Use ctx if needed for future LDAP tracing
	}

	// Try LDAP authentication first if enabled
	if s.ldapClient != nil {
		ldapUser, err := s.ldapClient.Authenticate(username, password)
		if err == nil {
			s.logger.Debug("LDAP authentication successful",
				"username", username,
				"email", ldapUser.Email,
				"is_admin", ldapUser.IsAdmin,
			)
			return true, nil
		}
		// If LDAP returns "user not found", fall back to local DB
		// Other errors (connection failure, etc.) also fall back to local DB
		s.logger.Debug("LDAP auth failed, falling back to local DB", "username", username, "error", err)
	}

	// Fall back to local database authentication
	user, domain := parseEmail(username)

	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return false, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)); err != nil {
		return false, nil
	}

	if !account.IsActive {
		return false, fmt.Errorf("account is not active")
	}

	return true, nil
}

// getUserSecret returns the password hash for a user, used by CRAM-MD5 authentication
func (s *Server) getUserSecret(username string) (string, error) {
	user, domain := parseEmail(username)
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		return "", err
	}
	if account == nil || !account.IsActive {
		return "", fmt.Errorf("user not found or inactive")
	}
	return account.PasswordHash, nil
}

// loginResult handles SMTP login success/failure events and triggers webhooks
// + audit. It exists for backwards compatibility with the SMTP wiring; new
// callers should use protoLoginHandler which is service-parameterized.
func (s *Server) loginResult(username string, success bool, ip string) {
	s.recordLoginResult("smtp", username, success, ip, "")
}

// recordLoginResult is the unified login-event sink for all auth-bearing
// protocols (smtp, imap, pop3). It writes to the audit log and fires the
// webhook event in one place so consumers stay consistent across protocols.
func (s *Server) recordLoginResult(service, username string, success bool, ip, reason string) {
	if s.apiServer != nil {
		if al := s.apiServer.AuditLogger(); al != nil {
			al.LogProtocolLogin(service, username, ip, success, reason)
		}
	}
	if s.webhookMgr != nil {
		eventType := "auth.login.success"
		if !success {
			eventType = "auth.login.failed"
		}
		payload := map[string]interface{}{
			"service":  service,
			"username": username,
			"ip":       ip,
		}
		if !success && reason != "" {
			payload["reason"] = reason
		}
		s.webhookMgr.Trigger(eventType, payload)
	}
}

// protoLoginHandler returns a SetLoginResultHandler-compatible callback that
// tags every event with the given protocol service ("smtp", "imap", "pop3").
func (s *Server) protoLoginHandler(service string) func(username string, success bool, ip, reason string) {
	return func(username string, success bool, ip, reason string) {
		s.recordLoginResult(service, username, success, ip, reason)
	}
}

// deliverMessage delivers an incoming message
func (s *Server) deliverMessage(from string, to []string, data []byte) error {
	return s.deliverMessageWithSieve(from, to, data, nil)
}

// deliverMessageWithSieve delivers an incoming message with optional Sieve filtering actions
// deliverMessageWithNotify is like deliverMessageWithSieve but also forwards per-recipient
// DSN NOTIFY preferences so that queue entries carry the correct bounce-suppression flags.
func (s *Server) deliverMessageWithNotify(from string, to []string, notify []string, data []byte) error {
	return s.deliverMessageToFolder(from, to, notify, data, "")
}

// deliverMessageToFolder is deliverMessageWithNotify with the local folder
// ("" for INBOX) chosen by the caller.
func (s *Server) deliverMessageToFolder(from string, to []string, notify []string, data []byte, folder string) error {
	if !s.beginDelivery() {
		return errServerStopping
	}
	defer s.deliveries.Done()
	ctx := context.Background()
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		var span trace.Span
		_, span = s.tracingProvider.StartSpanWithKind(ctx, "deliverMessage", tracing.SpanKindServer,
			attribute.String("mail.from", from),
			attribute.Int("mail.recipients", len(to)),
			attribute.Int("mail.size", len(data)),
		)
		defer span.End()
	}

	var failed []rcptFailure
	delivered := 0
	for i, recipient := range to {
		user, domain := parseEmail(recipient)
		rcptNotify := ""
		if i < len(notify) {
			rcptNotify = notify[i]
		}

		domainData, err := s.database.GetDomain(domain)
		if err != nil || domainData == nil || !domainData.IsActive {
			if relayErr := s.relayMessageWithNotify(from, recipient, notify, data); relayErr != nil {
				s.logger.Error("Failed to relay message", "to", recipient, "error", relayErr)
				failed = append(failed, rcptFailure{rcpt: recipient, notify: rcptNotify, err: fmt.Errorf("relay %s: %w", recipient, relayErr)})
			} else {
				delivered++
			}
			continue
		}

		target, aliasErr := s.database.ResolveAlias(domain, user)
		if aliasErr != nil {
			s.logger.Debug("Alias resolution failed, trying direct delivery", "domain", domain, "user", user, "error", aliasErr)
		}
		if target != "" {
			tUser, tDomain := parseEmail(target)
			if tUser != "" && tDomain != "" {
				user = tUser
				domain = tDomain
			}
		}

		// The mailbox owner's Sieve script decides the folder, redirects,
		// discard or reject (F5115); without a script this is deliverLocal.
		if err := s.deliverLocalFiltered(user, domain, recipient, from, rcptNotify, data, folder); err != nil {
			s.logger.Error("Failed to deliver locally", "user", user, "domain", domain, "error", err)
			failed = append(failed, rcptFailure{rcpt: recipient, notify: rcptNotify, err: fmt.Errorf("deliver %s: %w", recipient, err), local: err})
		} else {
			delivered++
		}
	}

	return s.settleDelivery(from, data, delivered, failed)
}

// deliverInboundWithNotify is the delivery handler of the inbound (MX)
// server. Its pipeline marks spam-verdict mail with X-Spam-Status: Yes
// after spamHeaderGuardStage has renamed any such header the sender
// supplied, so the header is the server's own verdict here and the message
// is filed into the recipient's Junk folder (F4975). Submission servers keep
// deliverMessageWithNotify, which never reads the header.
func (s *Server) deliverInboundWithNotify(from string, to []string, notify []string, data []byte) error {
	folder := ""
	if hasSpamVerdict(data) {
		folder = "Junk"
	}
	return s.deliverMessageToFolder(from, to, notify, data, folder)
}

// isSpamHeaderName reports whether a header field name belongs to the
// X-Spam-* family whose values only this server may set.
func isSpamHeaderName(name string) bool {
	return len(name) >= len("X-Spam-") && strings.EqualFold(name[:len("X-Spam-")], "X-Spam-")
}

// forEachHeaderField calls fn for every header field of the message header
// block (up to the first empty line), with the field name and the byte
// offsets of the line in data. Continuation lines are skipped.
func forEachHeaderField(data []byte, fn func(name string, start, end int)) {
	for start := 0; start < len(data); {
		end := start
		for end < len(data) && data[end] != '\n' {
			end++
		}
		line := data[start:end]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			return
		}
		if line[0] != ' ' && line[0] != '\t' {
			if colon := strings.IndexByte(string(line), ':'); colon > 0 {
				fn(strings.TrimRight(string(line[:colon]), " \t"), start, end)
			}
		}
		start = end + 1
	}
}

// hasSpamVerdict reports whether the header block carries
// X-Spam-Status: Yes.
func hasSpamVerdict(data []byte) bool {
	found := false
	forEachHeaderField(data, func(name string, start, end int) {
		if found || !strings.EqualFold(name, "X-Spam-Status") {
			return
		}
		line := string(data[start:end])
		value := strings.TrimSpace(line[strings.IndexByte(line, ':')+1:])
		found = len(value) >= 3 && strings.EqualFold(value[:3], "yes")
	})
	return found
}

// errServerStopping is returned for a delivery attempted while the server is
// stopping; the SMTP session answers it with a transient 451.
var errServerStopping = errors.New("server is shutting down")

// beginDelivery registers an in-flight delivery, or reports false once Stop
// has begun (F4976). A true result must be paired with s.deliveries.Done().
func (s *Server) beginDelivery() bool {
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()
	if s.deliveryClosed {
		return false
	}
	s.deliveries.Add(1)
	return true
}

// rcptFailure records one recipient that could not be delivered or queued.
type rcptFailure struct {
	rcpt   string
	notify string // RFC 3461 NOTIFY value given for this recipient, if any
	err    error
	local  error  // the deliverLocal error, when the failure was local
	diag   string // Diagnostic-Code for the DSN; "" picks one from local
}

// settleDelivery turns per-recipient outcomes into the single result the
// SMTP DATA reply can carry (F4875). When nothing was delivered an error is
// returned and the client's retry cannot duplicate anything. Once some
// recipients have the message, failing the transaction makes the client
// retry every recipient and duplicates the mail to those already served, so
// the message is accepted and each failed recipient is reported to the
// sender with a failure DSN instead. If that report cannot be queued the
// error is returned: a duplicate is preferable to a silently lost recipient.
func (s *Server) settleDelivery(from string, data []byte, delivered int, failed []rcptFailure) error {
	if len(failed) == 0 {
		return nil
	}
	errs := make([]error, 0, len(failed))
	for _, f := range failed {
		errs = append(errs, f.err)
	}
	joined := fmt.Errorf("delivery had %d failure(s): %w", len(errs), errors.Join(errs...))
	if delivered == 0 {
		return joined
	}
	if err := s.bounceFailedRecipients(from, data, failed); err != nil {
		return fmt.Errorf("%w; failure report not queued: %v", joined, err)
	}
	s.logger.Warn("Partial delivery accepted; failed recipients reported to sender",
		"from", from, "delivered", delivered, "failed", len(failed), "error", joined)
	return nil
}

// bounceFailedRecipients queues one failure DSN per failed recipient back to
// the sender. A null-sender message is never bounced (RFC 5321 §4.5.5) and a
// recipient that asked for NOTIFY=NEVER gets no report (RFC 3461).
func (s *Server) bounceFailedRecipients(from string, data []byte, failed []rcptFailure) error {
	if from == "" {
		return nil
	}
	if s.queue == nil {
		return errors.New("queue not available")
	}
	for _, f := range failed {
		if queue.ParseDSNNotify(f.notify).HasNotify(queue.DSNNotifyNever) {
			continue
		}
		// Keep internal error text (paths, keys) out of the report.
		diag := "smtp; 550 5.0.0 local delivery failed"
		if f.diag != "" {
			diag = f.diag
		} else if f.local != nil && strings.HasPrefix(f.local.Error(), "quota exceeded") {
			diag = "smtp; 552 5.2.2 mailbox full"
		}
		dsn := &queue.DSN{
			ReportedDomain: "umailserver",
			ReportedName:   "umailserver",
			ArrivalDate:    time.Now(),
			OriginalFrom:   from,
			OriginalTo:     f.rcpt,
			Recipient: queue.DSNRecipient{
				Original: f.rcpt,
				Notify:   queue.DSNNotifyNever,
				Ret:      queue.DSNRetHeaders,
			},
			RemoteMTA: "umailserver",
			MessageID: queue.GenerateMessageID(),
		}
		msg, err := queue.GenerateFailureDSN(dsn, data, queue.DSNRetHeaders, diag)
		if err != nil {
			return fmt.Errorf("generate DSN for %s: %w", f.rcpt, err)
		}
		if _, err := s.queue.Enqueue("", []string{from}, msg); err != nil {
			return fmt.Errorf("queue DSN for %s: %w", f.rcpt, err)
		}
	}
	return nil
}

// relayMessageWithNotify relays a message with per-recipient DSN notify preferences.
func (s *Server) relayMessageWithNotify(from, to string, notify []string, data []byte) error {
	if s.queue != nil {
		_, err := s.queue.EnqueueWithNotify(from, []string{to}, notify, data)
		if err != nil {
			s.logger.Error("Failed to enqueue relay message with notify", "error", err)
			return fmt.Errorf("failed to queue message: %w", err)
		}
		s.logger.Debug("Message queued for relay with notify", "from", from, "to", to)
		return nil
	}
	return nil
}

func (s *Server) deliverMessageWithSieve(from string, to []string, data []byte, sieveActions []string) error {
	if !s.beginDelivery() {
		return errServerStopping
	}
	defer s.deliveries.Done()
	// Create tracing span if tracing is enabled
	ctx := context.Background()
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		var span trace.Span
		_, span = s.tracingProvider.StartSpanWithKind(ctx, "deliverMessage", tracing.SpanKindServer,
			attribute.String("mail.from", from),
			attribute.Int("mail.recipients", len(to)),
			attribute.Int("mail.size", len(data)),
		)
		defer span.End()
	}

	// Parse sieve actions for fileinto and redirect
	var targetFolder string
	var redirectAddrs []string

	for _, action := range sieveActions {
		if strings.HasPrefix(action, "fileinto:") {
			targetFolder = strings.TrimPrefix(action, "fileinto:")
		} else if strings.HasPrefix(action, "redirect:") {
			redirectAddr := strings.TrimPrefix(action, "redirect:")
			if redirectAddr != "" {
				redirectAddrs = append(redirectAddrs, redirectAddr)
			}
		}
	}

	// Handle redirects - queue copies to redirect addresses
	for _, redirectAddr := range redirectAddrs {
		// Check for forwarding loop
		loopAddrs := getMailLoopHeaders(data)
		for _, loopAddr := range loopAddrs {
			if strings.EqualFold(loopAddr, redirectAddr) {
				s.logger.Warn("Forwarding loop detected, skipping redirect", "loop_addr", loopAddr, "redirect_to", redirectAddr)
				continue
			}
		}
		// Add this sender to the loop tracking header
		dataWithLoop := addMailLoopHeader(data, from)
		if err := s.relayMessage(from, redirectAddr, dataWithLoop); err != nil {
			s.logger.Error("Failed to queue redirect message", "to", redirectAddr, "error", err)
			// Continue with other deliveries even if redirect fails
		} else {
			s.logger.Debug("Message queued for redirect", "from", from, "to", redirectAddr)
		}
	}

	var failed []rcptFailure
	delivered := 0
	for _, recipient := range to {
		user, domain := parseEmail(recipient)

		domainData, err := s.database.GetDomain(domain)
		if err != nil || domainData == nil || !domainData.IsActive {
			if relayErr := s.relayMessage(from, recipient, data); relayErr != nil {
				s.logger.Error("Failed to relay message", "to", recipient, "error", relayErr)
				failed = append(failed, rcptFailure{rcpt: recipient, err: fmt.Errorf("relay %s: %w", recipient, relayErr)})
			} else {
				delivered++
			}
			continue
		}

		// Resolve alias
		target, aliasErr := s.database.ResolveAlias(domain, user)
		if aliasErr != nil {
			s.logger.Debug("Alias resolution failed, trying direct delivery", "domain", domain, "user", user, "error", aliasErr)
		}
		if target != "" {
			tUser, tDomain := parseEmail(target)
			if tUser != "" && tDomain != "" {
				user = tUser
				domain = tDomain
			}
		}

		// Deliver with optional target folder from sieve
		if err := s.deliverLocal(user, domain, from, data, targetFolder); err != nil {
			s.logger.Error("Failed to deliver locally", "user", user, "domain", domain, "error", err)
			failed = append(failed, rcptFailure{rcpt: recipient, err: fmt.Errorf("deliver %s: %w", recipient, err), local: err})
		} else {
			delivered++
		}
	}

	return s.settleDelivery(from, data, delivered, failed)
}

// relayMessage relays a message to a remote server
func (s *Server) relayMessage(from, to string, data []byte) error {
	if s.queue != nil {
		_, err := s.queue.Enqueue(from, []string{to}, data)
		if err != nil {
			s.logger.Error("Failed to enqueue relay message", "error", err)
			return fmt.Errorf("failed to queue message: %w", err)
		}
		s.logger.Debug("Message queued for relay", "from", from, "to", to)
		return nil
	}
	s.logger.Debug("Relaying message (queue not available)", "from", from, "to", to)
	return nil
}

// getMailLoopHeaders returns all addresses in existing X-Mail-Loop headers.
func getMailLoopHeaders(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return nil
	}
	return msg.Header["X-Mail-Loop"]
}

// addMailLoopHeader appends an X-Mail-Loop header with the given address.
func addMailLoopHeader(data []byte, addr string) []byte {
	if len(data) == 0 {
		return data
	}
	idx := strings.Index(string(data), "\r\n\r\n")
	if idx == -1 {
		idx = strings.Index(string(data), "\n\n")
		if idx == -1 {
			return data
		}
		// Insert before blank line (Unix newline)
		headerPart := string(data[:idx+1])
		bodyPart := string(data[idx+1:])
		return []byte(headerPart + "X-Mail-Loop: " + addr + "\n" + bodyPart)
	}
	// Insert before blank line (Windows newline)
	headerPart := string(data[:idx+2])
	bodyPart := string(data[idx+2:])
	return []byte(headerPart + "X-Mail-Loop: " + addr + "\r\n" + bodyPart)
}

// addReturnPath returns data with a Return-Path header for the envelope
// sender prepended and any existing Return-Path header removed.
func addReturnPath(data []byte, from string) []byte {
	from = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '<' || r == '>' {
			return -1
		}
		return r
	}, from)
	hdrEnd := len(data)
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		hdrEnd = i + 2
	}
	if j := bytes.Index(data, []byte("\n\n")); j >= 0 && j+1 < hdrEnd {
		hdrEnd = j + 1
	}
	var out bytes.Buffer
	out.WriteString("Return-Path: <" + from + ">\r\n")
	skipping := false
	pos := 0
	for pos < hdrEnd {
		nl := bytes.IndexByte(data[pos:hdrEnd], '\n')
		end := hdrEnd
		if nl >= 0 {
			end = pos + nl + 1
		}
		line := data[pos:end]
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if !skipping {
				out.Write(line)
			}
		} else {
			skipping = len(line) >= 12 && strings.EqualFold(string(line[:12]), "Return-Path:")
			if !skipping {
				out.Write(line)
			}
		}
		pos = end
	}
	out.Write(data[hdrEnd:])
	return out.Bytes()
}

// deliverLocal delivers a message to a local mailbox
func (s *Server) deliverLocal(user, domain, from string, data []byte, targetFolders ...string) error {
	return s.deliverLocalHop(user, domain, from, data, true, targetFolders...)
}

// deliverLocalHop is deliverLocal with the catch-all redirect allowed at most
// once: the catch-all target itself is delivered with allowCatchAll=false, so
// a target that is missing or inactive fails instead of recursing (F4877).
func (s *Server) deliverLocalHop(user, domain, from string, data []byte, allowCatchAll bool, targetFolders ...string) error {
	email := user + "@" + domain

	// Determine target folder - default to INBOX if not specified
	folder := "INBOX"
	if len(targetFolders) > 0 && targetFolders[0] != "" {
		folder = targetFolders[0]
	}

	// Check if user exists
	account, err := s.database.GetAccount(domain, user)
	if err != nil || account == nil || !account.IsActive {
		// The catch-all covers mailboxes that do not exist as well as
		// inactive ones (F4876).
		if allowCatchAll {
			if domainData, derr := s.database.GetDomain(domain); derr == nil && domainData != nil && domainData.CatchAllTarget != "" {
				tUser, tDomain := parseEmail(domainData.CatchAllTarget)
				if tUser != "" && tDomain != "" {
					return s.deliverLocalHop(tUser, tDomain, from, data, false, targetFolders...)
				}
			}
		}
		if err != nil {
			return fmt.Errorf("user does not exist: %s", email)
		}
		return fmt.Errorf("user does not exist or is not active: %s", email)
	}

	// Handle mail forwarding (before storing, so we skip local store if not keeping copy)
	if account.ForwardTo != "" {
		// Check for forwarding loop. A looped message is not forwarded
		// again; it falls through to the local store so it is neither lost
		// nor left holding the quota reserved above (F4878).
		looped := false
		for _, loopAddr := range getMailLoopHeaders(data) {
			if strings.EqualFold(loopAddr, email) {
				looped = true
				break
			}
		}
		// Add this sender to the loop tracking header
		dataWithLoop := addMailLoopHeader(data, email)
		forwarded, forwardFailed := 0, false
		if looped {
			s.logger.Warn("Forwarding loop detected, skipping forward and keeping local copy", "from", from, "to", email)
		} else {
			for _, fwd := range strings.Split(account.ForwardTo, ",") {
				fwd = strings.TrimSpace(fwd)
				if fwd == "" {
					continue
				}
				if s.queue == nil {
					forwardFailed = true
					continue
				}
				if _, err := s.queue.Enqueue(email, []string{fwd}, dataWithLoop); err != nil {
					s.logger.Error("Failed to enqueue forwarded message", "from", email, "to", fwd, "error", err)
					forwardFailed = true
					continue
				}
				forwarded++
			}
		}
		// Drop the local copy only when every forward was queued; otherwise
		// keep it so the message is not lost (F4879).
		if !account.ForwardKeepCopy && forwarded > 0 && !forwardFailed {
			s.logger.Debug("Message forwarded (no local copy)",
				"to", email,
				"from", from,
			)
			return nil
		}
	}

	// RFC 5321 4.4: the final delivery system records the envelope sender in
	// a Return-Path header, replacing any the message arrived with.
	data = addReturnPath(data, from)

	// Reserve quota atomically before storing. This comes after the
	// forward-only return above: a message that is not stored must not be
	// refused for quota (F5713).
	if err := s.database.IncrementQuota(domain, user, int64(len(data))); err != nil {
		return fmt.Errorf("quota exceeded for user: %s", email)
	}

	// Store message locally
	messageID, err := s.msgStore.StoreMessage(email, data)
	if err != nil {
		// Release the quota we reserved since store failed
		s.database.IncrementQuota(domain, user, -int64(len(data)))
		return fmt.Errorf("failed to store message: %w", err)
	}

	s.logger.Debug("Message delivered",
		"to", email,
		"from", from,
		"message_id", messageID,
	)

	// Store metadata and index message for search
	if s.storageDB != nil {
		// A folder other than INBOX (Junk, a Sieve fileinto target) may not
		// exist yet; create it so it gets a UIDVALIDITY before the first UID
		// is assigned (F4975). CreateMailbox is a no-op for an existing one.
		if folder != "INBOX" {
			if err := s.storageDB.CreateMailbox(email, folder); err != nil {
				s.logger.Error("Failed to create delivery folder", "email", email, "folder", folder, "error", err)
			}
		}
		uid, uidErr := s.storageDB.GetNextUID(email, folder)
		if uidErr == nil {
			subject, fromAddr, toAddr, dateStr := parseBasicHeaders(data)
			inReplyTo, references := parseThreadHeaders(data)
			// Assign thread identity at delivery, mirroring the IMAP APPEND
			// path (internal/imap/mailstore.go) so SMTP-delivered mail is
			// threaded for JMAP Thread/get and the REST threads API.
			threadID, threadErr := s.storageDB.GetOrCreateThreadID(email, folder, subject, inReplyTo, references)
			if threadErr != nil {
				threadID = "" // Continue without threading if it fails
			}
			meta := &storage.MessageMetadata{
				MessageID:    messageID,
				UID:          uid,
				Flags:        []string{"\\Recent"},
				InternalDate: time.Now(),
				Size:         int64(len(data)),
				Subject:      subject,
				Date:         dateStr,
				From:         fromAddr,
				To:           toAddr,
				ThreadID:     threadID,
				IsThreadRoot: inReplyTo == "" && len(references) == 0,
			}
			if err := s.storageDB.StoreMessageMetadata(email, folder, uid, meta); err != nil {
				s.logger.Error("Failed to store message metadata", "email", email, "uid", uid, "folder", folder, "error", err)
			}

			// Refresh the thread aggregate row (best-effort), mirroring the
			// IMAP APPEND path so the REST threads list reflects SMTP mail.
			if threadID != "" {
				s.updateThreadAggregate(email, subject, fromAddr, meta, threadID)
			}

			if s.searchSvc != nil {
				select {
				case s.indexWork <- indexJob{email: email, folder: folder, uid: uid}:
				default:
					s.logger.Warn("Search index queue full, dropping index job", "email", email, "uid", uid)
				}
			}
		}
	}

	// Trigger webhook for mail received
	if s.webhookMgr != nil {
		s.webhookMgr.Trigger(webhook.EventMailReceived, map[string]interface{}{
			"message_id": messageID,
			"to":         email,
			"from":       from,
			"size":       len(data),
		})
	}

	// Send push notification for new mail
	if s.pushSvc != nil {
		select {
		case s.bgSem <- struct{}{}:
			go func() {
				defer func() {
					<-s.bgSem
					if r := recover(); r != nil {
						s.logger.Error("Panic in push notification", "error", r)
					}
				}()
				// Extract subject from message for notification
				subject, _, _, _ := parseBasicHeaders(data)
				if subject == "" {
					subject = "(No subject)"
				}
				// Send push notification (non-blocking)
				if err := s.pushSvc.SendNewMailNotification(email, from, subject, ""); err != nil {
					s.logger.Debug("Failed to send push notification", "to", email, "error", err)
				}
			}()
		default:
			s.logger.Warn("Background task semaphore full, dropping push notification", "email", email)
		}
	}

	// Track delivery metric
	metrics.Get().DeliverySuccess()

	// Send vacation auto-reply if configured
	if account.VacationSettings != "" && s.queue != nil && folder != "Junk" && autoReplyAllowed(from, data) {
		select {
		case s.bgSem <- struct{}{}:
			go func() {
				defer func() {
					<-s.bgSem
					if r := recover(); r != nil {
						s.logger.Error("Panic in vacation reply", "error", r)
					}
				}()
				s.sendVacationReply(email, from, account.VacationSettings)
			}()
		default:
			s.logger.Warn("Background task semaphore full, dropping vacation reply", "email", email)
		}
	}
	return nil
}

// autoReplyAllowed reports whether an automatic reply (vacation) may be sent
// for a message: never to the null sender, nor to mail that is itself
// automatic or bulk (RFC 3834 §2/§4, RFC 5230 §4.6): Auto-Submitted other
// than "no", Precedence bulk/list/junk, or any List-* header (F5710-F5712).
func autoReplyAllowed(from string, data []byte) bool {
	if from == "" {
		return false
	}
	allowed := true
	forEachHeaderField(data, func(name string, start, end int) {
		line := string(data[start:end])
		value := strings.ToLower(strings.TrimSpace(line[strings.IndexByte(line, ':')+1:]))
		switch {
		case strings.EqualFold(name, "Auto-Submitted"):
			if v, _, _ := strings.Cut(value, ";"); strings.TrimSpace(v) != "no" {
				allowed = false
			}
		case strings.EqualFold(name, "Precedence"):
			if value == "bulk" || value == "list" || value == "junk" {
				allowed = false
			}
		case len(name) >= len("List-") && strings.EqualFold(name[:len("List-")], "List-"):
			allowed = false
		}
	})
	return allowed
}

// parseEmail splits an email address into user and domain
func parseEmail(email string) (user, domain string) {
	at := -1
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			at = i
			break
		}
	}
	if at == -1 {
		return email, ""
	}
	return email[:at], email[at+1:]
}

// parseBasicHeaders extracts subject, from, to, date from raw message data.
func parseBasicHeaders(data []byte) (subject, from, to, date string) {
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return "", "", "", ""
	}
	subject = msg.Header.Get("Subject")
	from = msg.Header.Get("From")
	to = msg.Header.Get("To")
	date = msg.Header.Get("Date")
	return
}

// parseThreadHeaders extracts the threading headers from a raw message,
// mirroring the IMAP parser (internal/imap/mailstore.go
// parseMessageHeadersExtended): In-Reply-To as a single id and References
// split into its whitespace-separated message-ids.
func parseThreadHeaders(data []byte) (inReplyTo string, references []string) {
	msg, err := mail.ReadMessage(strings.NewReader(string(data)))
	if err != nil {
		return "", nil
	}
	inReplyTo = strings.TrimSpace(msg.Header.Get("In-Reply-To"))
	for _, raw := range msg.Header["References"] {
		for _, ref := range strings.Fields(raw) {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				references = append(references, ref)
			}
		}
	}
	return inReplyTo, references
}

// updateThreadAggregate refreshes the Thread summary row after a local
// delivery (best-effort), mirroring the IMAP APPEND path's updateThreadInfo.
func (s *Server) updateThreadAggregate(email, subject, from string, meta *storage.MessageMetadata, threadID string) {
	thread, err := s.storageDB.GetThread(email, threadID)
	if err != nil || thread == nil {
		thread = &storage.Thread{
			ThreadID:     threadID,
			Subject:      storage.NormalizeSubject(subject),
			Participants: []string{},
			MessageCount: 0,
			UnreadCount:  0,
			LastActivity: meta.InternalDate,
			CreatedAt:    time.Now(),
		}
	}

	thread.MessageCount++
	thread.LastActivity = meta.InternalDate

	if from != "" {
		found := false
		for _, p := range thread.Participants {
			if p == from {
				found = true
				break
			}
		}
		if !found {
			thread.Participants = append(thread.Participants, from)
		}
	}

	if !storage.HasFlag(meta.Flags, "\\Seen") {
		thread.UnreadCount++
	}

	if err := s.storageDB.UpdateThread(email, thread); err != nil {
		s.logger.Error("Failed to update thread aggregate", "email", email, "thread_id", threadID, "error", err)
	}
}

// generateSecureToken generates a cryptographically random 32-byte hex token.
func generateSecureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// authenticateClientCert authenticates a user based on client certificate
// Returns the email address extracted from the certificate and true if valid
func (s *Server) authenticateClientCert(cert *x509.Certificate) (string, bool) {
	if cert == nil {
		return "", false
	}

	// Extract email from certificate
	var email string
	if len(cert.EmailAddresses) > 0 {
		email = cert.EmailAddresses[0]
	} else if cert.Subject.CommonName != "" {
		// Try to use CommonName as email if it looks like one
		if strings.Contains(cert.Subject.CommonName, "@") {
			email = cert.Subject.CommonName
		}
	}

	if email == "" {
		s.logger.Debug("Client certificate has no email address")
		return "", false
	}

	// Verify the account exists
	user, domain := parseEmail(email)
	account, err := s.database.GetAccount(domain, user)
	if err != nil {
		s.logger.Debug("Account not found for client certificate", "email", email)
		return "", false
	}

	if !account.IsActive {
		s.logger.Debug("Account is not active", "email", email)
		return "", false
	}

	// Log successful client cert auth
	s.logger.Info("Client certificate authentication successful", "email", email)

	return email, true
}
