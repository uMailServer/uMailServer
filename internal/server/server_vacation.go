package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/sieve"
	"github.com/umailserver/umailserver/internal/vacation"
)

// defaultVacationInterval is the reply-once window when none (or a
// non-positive one) is configured; it matches vacation.Manager (F6442).
const defaultVacationInterval = 7 * 24 * time.Hour

// vacationDedupKey is the case-insensitive, pipe-safe key for a
// (mailbox, sender) pair; angle brackets are ignored.
func vacationDedupKey(recipient, sender string) string {
	norm := func(a string) string {
		a = strings.TrimSpace(a)
		a = strings.TrimSuffix(strings.TrimPrefix(a, "<"), ">")
		return strings.ToLower(strings.ReplaceAll(a, "|", "__"))
	}
	return norm(recipient) + "|" + norm(sender)
}

// sieveVacationHandle is the RFC 5230 §4.6 suppression identity of a vacation
// action: the script's :handle, or a hash of :subject, the reason and :mime.
func sieveVacationHandle(va sieve.VacationAction) string {
	if va.Handle != "" {
		return "h:" + va.Handle
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q\x00%q\x00%t", va.Subject, va.Body, va.Mime)))
	return "d:" + hex.EncodeToString(sum[:16])
}

// sieveVacationKey is the reply-suppression key: mailbox, sender and handle.
func sieveVacationKey(recipient, sender string, va sieve.VacationAction) string {
	return vacationDedupKey(recipient, sender) + "|" + sieveVacationHandle(va)
}

// vacationSuppressedByHeaders applies the message-level rules of RFC 3834 §2
// and RFC 5230 §4.6: no reply to automatic, bulk/list/junk or mailing-list mail.
func vacationSuppressedByHeaders(h mail.Header) bool {
	if as := strings.TrimSpace(h.Get("Auto-Submitted")); as != "" && !strings.EqualFold(as, "no") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "bulk", "list", "junk":
		return true
	}
	for _, name := range []string{"List-Id", "List-Post", "List-Unsubscribe", "X-Auto-Response-Suppress"} {
		if strings.TrimSpace(h.Get(name)) != "" {
			return true
		}
	}
	return false
}

// vacationMatchedAddress implements the RFC 5230 §4.5 check: it returns the
// address from To/Cc/Bcc/Resent-* that is one of the user's own (the
// mailbox, the delivered-to address, a :addresses entry or an alias that
// routes to the mailbox), or "" when the message is not addressed to the user.
func (s *Server) vacationMatchedAddress(h mail.Header, mailbox, rcpt string, extra []string) string {
	own := func(a string) bool {
		if strings.EqualFold(a, mailbox) || strings.EqualFold(a, rcpt) {
			return true
		}
		for _, e := range extra {
			if ea, err := mail.ParseAddress(strings.TrimSpace(e)); err == nil && strings.EqualFold(ea.Address, a) {
				return true
			}
		}
		return s.routesToMailbox(a, mailbox)
	}
	for _, name := range []string{"To", "Cc", "Bcc", "Resent-To", "Resent-Cc", "Resent-Bcc"} {
		for _, v := range h[textproto.CanonicalMIMEHeaderKey(name)] {
			list, err := mail.ParseAddressList(v)
			if err != nil {
				continue
			}
			for _, a := range list {
				if own(a.Address) {
					return a.Address
				}
			}
		}
	}
	return ""
}

// vacationStorePath is the persisted dedup log, or "" when no data dir is set.
func (s *Server) vacationStorePath() string {
	if s.config == nil || s.config.Server.DataDir == "" {
		return ""
	}
	return filepath.Join(s.config.Server.DataDir, "vacation_replies.jsonl")
}

type vacationRecord struct {
	Key   string    `json:"key"`
	Until time.Time `json:"until"`
}

// loadVacationRepliesLocked restores unexpired suppression deadlines so a
// restart does not re-answer every sender (F6443). Caller holds the mutex.
func (s *Server) loadVacationRepliesLocked() {
	s.vacationReplies = make(map[string]time.Time)
	path := s.vacationStorePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path) // #nosec G304 -- server-owned path
	if err != nil {
		return
	}
	now := time.Now()
	for _, ln := range strings.Split(string(data), "\n") {
		var rec vacationRecord
		if ln == "" || json.Unmarshal([]byte(ln), &rec) != nil || !now.Before(rec.Until) {
			continue
		}
		if rec.Until.After(s.vacationReplies[rec.Key]) {
			s.vacationReplies[rec.Key] = rec.Until
		}
	}
}

// persistVacationReplyLocked appends one deadline to the log. Caller holds the mutex.
func (s *Server) persistVacationReplyLocked(key string, until time.Time) {
	path := s.vacationStorePath()
	if path == "" {
		return
	}
	line, err := json.Marshal(vacationRecord{Key: key, Until: until})
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}

// rewriteVacationRepliesLocked compacts the log to the live map. Caller holds the mutex.
func (s *Server) rewriteVacationRepliesLocked() {
	path := s.vacationStorePath()
	if path == "" {
		return
	}
	var buf []byte
	for k, u := range s.vacationReplies {
		if line, err := json.Marshal(vacationRecord{Key: k, Until: u}); err == nil {
			buf = append(append(buf, line...), '\n')
		}
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, buf, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

// claimVacationReply atomically checks and records the reply-once window.
// It reports whether a reply may be sent now.
func (s *Server) claimVacationReply(key string, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		interval = defaultVacationInterval
	}
	s.vacationRepliesMu.Lock()
	defer s.vacationRepliesMu.Unlock()
	if s.vacationReplies == nil {
		s.loadVacationRepliesLocked()
	}
	if until, ok := s.vacationReplies[key]; ok && now.Before(until) {
		return false
	}
	until := now.Add(interval) // F4826: suppression deadline
	s.vacationReplies[key] = until
	s.persistVacationReplyLocked(key, until)
	if len(s.vacationReplies) > 100 {
		s.cleanupVacationRepliesLocked()
	}
	return true
}

// parseVacationDate accepts a plain date or RFC 3339. dateOnly reports the
// plain form, whose end date covers the whole day.
func parseVacationDate(v string) (t time.Time, dateOnly, ok bool) {
	if d, err := time.Parse("2006-01-02", v); err == nil {
		return d, true, true
	}
	if d, err := time.Parse(time.RFC3339, v); err == nil {
		return d, false, true
	}
	return time.Time{}, false, false
}

// autoReplyHeaders builds the common auto-reply header block. Envelope sender
// is null; the From header is shown to the recipient only (RFC 5230 §4.?).
func autoReplyHeaders(from, to, subject string, now time.Time, host, transferEncoding string) string {
	return "From: " + sanitizeHeaderValue(from) + "\r\n" +
		"To: " + sanitizeHeaderValue(to) + "\r\n" +
		// F7780: non-ASCII text is an RFC 2047 encoded-word, not raw 8-bit
		// header bytes (a subject that is already encoded passes through).
		"Subject: " + mime.QEncoding.Encode("utf-8", sanitizeHeaderValue(subject)) + "\r\n" +
		"Date: " + now.Format(time.RFC1123Z) + "\r\n" +
		fmt.Sprintf("Message-ID: <vacation.%d@%s>\r\n", now.UnixNano(), host) +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: " + transferEncoding + "\r\n" +
		"Auto-Submitted: auto-replied\r\n" +
		"Precedence: bulk\r\n" +
		"X-Auto-Response-Suppress: All\r\n"
}

func mailHost(addr string) string {
	if at := strings.LastIndex(addr, "@"); at >= 0 && at+1 < len(addr) {
		return sanitizeHeaderValue(strings.Trim(addr[at+1:], "<> "))
	}
	return "localhost"
}

// sanitizeHeaderValue removes CR/LF characters to prevent SMTP header injection.
func sanitizeHeaderValue(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// vacationBody returns a reply body with every line break as CRLF. Unlike
// header values, the body keeps its line breaks: stripping them (as
// sanitizeHeaderValue does) collapsed a multi-line vacation text into one
// line (F5117). A bare CR or LF is normalised so the message stays RFC 5322.
func vacationBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// vacationBodyEncoded returns the body for a reply and its
// Content-Transfer-Encoding: as-is (7bit) when it is ASCII with lines within
// 998 octets, quoted-printable otherwise. The reply used to declare every
// body "8bit", which relays may not accept, and long lines broke RFC 5322
// §2.1.1 (F7780).
func vacationBodyEncoded(s string) (encoding, body string) {
	body = vacationBody(s)
	plain := true
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 998 {
			plain = false
			break
		}
		for i := 0; i < len(line); i++ {
			if line[i] >= 0x80 {
				plain = false
				break
			}
		}
	}
	if plain {
		return "7bit", body
	}
	var qp strings.Builder
	w := quotedprintable.NewWriter(&qp)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return "quoted-printable", qp.String()
}

// handleSieveVacation handles Sieve vacation action by sending a vacation auto-reply
func (s *Server) handleSieveVacation(sender, recipient string, va sieve.VacationAction) {
	s.handleSieveVacationAs(sender, recipient, recipient, va)
}

// handleSieveVacationAs is handleSieveVacation with matched, the user's own
// address the message was sent to; it is the default From of the reply
// (RFC 5230 §5, :from default).
func (s *Server) handleSieveVacationAs(sender, recipient, matched string, va sieve.VacationAction) {
	if s.queue == nil {
		return
	}

	// Never answer the null path, mailer daemons, list/no-reply addresses
	// or ourselves (RFC 3834 §2).
	if vacation.IsAutomatedSender(sender) || strings.EqualFold(strings.Trim(sender, "<> "), recipient) {
		return
	}

	// RFC 5230 §4.1: minimum interval is 1 second; zero means every message
	// (explicit :seconds 0), handled by the caller's window.
	sendInterval := time.Duration(va.Seconds) * time.Second
	now := time.Now()
	if sendInterval > 0 && !s.claimVacationReply(sieveVacationKey(recipient, sender, va), now, sendInterval) {
		return
	}

	// Build vacation message content
	subject := va.Subject
	if subject == "" {
		subject = "Automated reply"
	}
	body := va.Body
	if body == "" {
		body = "I'm currently on vacation and will reply when I return."
	}

	// Create vacation message - From is the recipient (who's on vacation)
	fromAddr := recipient
	if matched != "" {
		fromAddr = matched
	}
	if va.From != "" {
		fromAddr = va.From
	}
	cte, encodedBody := vacationBodyEncoded(body)
	vacationMsg := autoReplyHeaders(fromAddr, sender, subject, now, mailHost(recipient), cte) +
		"X-Mail-Loop: <" + sanitizeHeaderValue(recipient) + ">\r\n\r\n" + encodedBody

	// RFC 5230 §4.? / RFC 3834 §4: the envelope sender of an auto-reply is
	// null; :from only changes the visible header (F6444).
	if _, err := s.queue.Enqueue("", []string{sender}, []byte(vacationMsg)); err != nil {
		s.logger.Error("Failed to enqueue vacation reply", "to", sender, "from", fromAddr, "error", err)
	} else {
		s.logger.Debug("Vacation reply enqueued", "to", sender, "from", fromAddr)
	}
}

// sendVacationReply generates and enqueues an auto-reply message.
func (s *Server) sendVacationReply(recipientEmail, senderEmail, settingsJSON string) {
	s.sendVacationReplyCtx(recipientEmail, senderEmail, "", vacationMsgInfo{}, settingsJSON)
}

// vacationMsgInfo is what a reply needs from the message it answers.
type vacationMsgInfo struct {
	MessageID  string
	Subject    string
	References []string
	// Headers holds the original message's header fields (first value per
	// name) for the RFC 3834 suppression rules in vacation.Eligible.
	Headers map[string]string
}

// vacationInfoFromMessage extracts the Message-ID, Subject and References of
// the original message for threading the reply (RFC 5322 §3.6.4).
func vacationInfoFromMessage(data []byte) vacationMsgInfo {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return vacationMsgInfo{}
	}
	_, refs := parseThreadHeaders(data)
	headers := make(map[string]string, len(msg.Header))
	for k, v := range msg.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return vacationMsgInfo{
		Headers:    headers,
		MessageID:  sanitizeHeaderValue(strings.TrimSpace(msg.Header.Get("Message-ID"))),
		Subject:    sanitizeHeaderValue(strings.TrimSpace(msg.Header.Get("Subject"))),
		References: refs,
	}
}

// sendVacationReplyCtx is sendVacationReply with the address the sender wrote
// to (origRcpt, possibly an alias or plus address) and the original message's
// identity. The reply is From origRcpt when it is a valid address, and carries
// In-Reply-To/References (F6510).
func (s *Server) sendVacationReplyCtx(recipientEmail, senderEmail, origRcpt string, info vacationMsgInfo, settingsJSON string) {
	var settings struct {
		Enabled          bool          `json:"enabled"`
		Subject          string        `json:"subject"`
		Message          string        `json:"message"`
		StartDate        string        `json:"start_date"`
		EndDate          string        `json:"end_date"`
		SendInterval     time.Duration `json:"send_interval"`
		ExcludeAddresses []string      `json:"exclude_addresses"`
		IgnoreLists      bool          `json:"ignore_lists"`
		IgnoreBulk       bool          `json:"ignore_bulk"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil || !settings.Enabled {
		return
	}

	// Share the Manager's rules (null/automated sender, own address,
	// excludes, window, default interval) instead of duplicating them.
	cfg := &vacation.Config{
		Enabled:          true,
		Subject:          settings.Subject,
		Message:          settings.Message,
		SendInterval:     settings.SendInterval,
		ExcludeAddresses: settings.ExcludeAddresses,
		IgnoreLists:      settings.IgnoreLists,
		IgnoreBulk:       settings.IgnoreBulk,
	}
	if settings.StartDate != "" {
		if t, _, ok := parseVacationDate(settings.StartDate); ok {
			cfg.StartDate = t
		}
	}
	if settings.EndDate != "" {
		if t, dateOnly, ok := parseVacationDate(settings.EndDate); ok {
			if dateOnly {
				t = t.Add(24*time.Hour - time.Nanosecond) // the whole end day
			}
			cfg.EndDate = t
		}
	}
	now := time.Now()
	if vacation.IsAutomatedSender(senderEmail) {
		return
	}
	eff, ok := vacation.Eligible(cfg, recipientEmail, senderEmail, info.Headers, now)
	if !ok {
		return
	}
	// Never answer ourselves through an alias, on either side.
	if origRcpt != "" && strings.EqualFold(strings.Trim(senderEmail, "<> "), strings.Trim(origRcpt, "<> ")) {
		return
	}
	if s.routesToMailbox(senderEmail, recipientEmail) {
		return
	}

	// F4825: record dedup state only once a reply is eligible.
	if !s.claimVacationReply(vacationDedupKey(recipientEmail, senderEmail), now, eff.SendInterval) {
		return
	}
	if s.queue == nil {
		return
	}

	subject := strings.TrimSpace(sanitizeHeaderValue(settings.Subject))
	if subject == "" {
		subject = "Auto: Out of Office"
	}
	if strings.TrimSpace(settings.Subject) == "" && info.Subject != "" {
		subject = "Auto: " + info.Subject
	}
	fromAddr := recipientEmail
	if a, perr := mail.ParseAddress(strings.TrimSpace(origRcpt)); perr == nil && origRcpt != "" && s.routesToMailbox(a.Address, recipientEmail) {
		fromAddr = a.Address
	}
	extra := ""
	if info.MessageID != "" {
		extra = "In-Reply-To: " + info.MessageID + "\r\n"
		refs := append(append([]string{}, info.References...), info.MessageID)
		if len(refs) > 20 {
			refs = append(refs[:1:1], refs[len(refs)-19:]...)
		}
		extra += "References: " + sanitizeHeaderValue(strings.Join(refs, " ")) + "\r\n"
	}
	cte, encodedBody := vacationBodyEncoded(settings.Message)
	autoReply := autoReplyHeaders(fromAddr, senderEmail, subject, now, mailHost(recipientEmail), cte) + extra +
		"\r\n" + encodedBody

	// RFC 5230 / RFC 3834 §4: null envelope sender.
	if _, err := s.queue.Enqueue("", []string{senderEmail}, []byte(autoReply)); err != nil {
		s.logger.Error("Failed to enqueue vacation reply", "error", err)
	}
}

// cleanupVacationReplies removes entries whose suppression deadline passed more
// than 48 hours ago (F4826: values are deadlines, not send times).
// Single-phase scan+delete under lock: the two-phase pattern (snapshot then delete outside
// the lock) introduced a correctness flaw where a fresh entry added between phases 1 and 2
// with the same key would be silently deleted, breaking deduplication and causing a second
// vacation reply to be sent to the sender.
func (s *Server) cleanupVacationReplies() {
	cutoff := time.Now().Add(-48 * time.Hour)

	s.vacationRepliesMu.Lock()
	defer s.vacationRepliesMu.Unlock()
	for key, lastSent := range s.vacationReplies {
		if lastSent.Before(cutoff) {
			delete(s.vacationReplies, key)
		}
	}
	s.rewriteVacationRepliesLocked()
}

// cleanupVacationRepliesLocked removes entries whose suppression deadline passed
// more than 48 hours ago.
// Caller must hold s.vacationRepliesMu.
func (s *Server) cleanupVacationRepliesLocked() {
	cutoff := time.Now().Add(-48 * time.Hour)
	for key, lastSent := range s.vacationReplies {
		if lastSent.Before(cutoff) {
			delete(s.vacationReplies, key)
		}
	}
	s.rewriteVacationRepliesLocked()
}

// startVacationCleanup runs a hourly goroutine that removes vacation reply entries
// older than 48 hours, preventing unbounded map growth independent of the
// threshold-based cleanup in trackVacationReply.
func (s *Server) startVacationCleanup() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.cleanupVacationReplies()
			}
		}
	}()
}

// routesToMailbox reports whether addr (the mailbox itself, an alias or a
// plus address) delivers to the local mailbox.
func (s *Server) routesToMailbox(addr, mailbox string) bool {
	addr, mailbox = strings.Trim(addr, "<> "), strings.Trim(mailbox, "<> ")
	if strings.EqualFold(addr, mailbox) {
		return true
	}
	if s.database == nil || s.logger == nil {
		return false
	}
	u, d, ext, err := s.routeRecipient(addr)
	return err == nil && ext == "" && strings.EqualFold(u+"@"+d, mailbox)
}

// effectiveVacationSettings returns the settings JSON that drives the
// auto-reply for a mailbox: the admin-managed account setting when present,
// otherwise the user's own /api/v1/vacation configuration (F7770).
func (s *Server) effectiveVacationSettings(email, accountSettings string) string {
	if accountSettings != "" || s.vacationMgr == nil {
		return accountSettings
	}
	cfg, err := s.vacationMgr.GetConfig(email)
	if err != nil || cfg == nil || !cfg.Enabled {
		return ""
	}
	m := map[string]any{
		"enabled":           true,
		"subject":           cfg.Subject,
		"message":           cfg.Message,
		"send_interval":     cfg.SendInterval,
		"exclude_addresses": cfg.ExcludeAddresses,
		"ignore_lists":      cfg.IgnoreLists,
		"ignore_bulk":       cfg.IgnoreBulk,
	}
	if !cfg.StartDate.IsZero() {
		m["start_date"] = cfg.StartDate.Format(time.RFC3339)
	}
	if !cfg.EndDate.IsZero() {
		m["end_date"] = cfg.EndDate.Format(time.RFC3339)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}
