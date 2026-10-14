package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/mail"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/mimeutil"
	"github.com/umailserver/umailserver/internal/queue"
	"github.com/umailserver/umailserver/internal/storage"
)

var errMailBodyRead = errors.New("failed to read message body")

// Mail represents an email message
type Mail struct {
	ID             string   `json:"id"`
	From           string   `json:"from"`
	FromName       string   `json:"fromName"`
	To             []string `json:"to"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	Preview        string   `json:"preview"`
	Date           string   `json:"date"`
	Read           bool     `json:"read"`
	Starred        bool     `json:"starred"`
	Folder         string   `json:"folder"`
	HasAttachments bool     `json:"hasAttachments"`
	MessageIDHdr   string   `json:"messageId,omitempty"` // RFC 5322 Message-ID header, for reply threading
	Size           int64    `json:"size"`
}

// SendMailRequest represents a request to send an email
type SendMailRequest struct {
	To      []string `json:"to"`
	CC      []string `json:"cc"`
	BCC     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	// InReplyTo / References carry RFC 5322 threading headers for replies.
	InReplyTo  string   `json:"inReplyTo,omitempty"`
	References []string `json:"references,omitempty"`
	// Attachments are base64 files assembled server-side into multipart/mixed.
	Attachments []SendAttachment `json:"attachments,omitempty"`
}

// MailHandler handles mail-related API requests
type MailHandler struct {
	msgStore *storage.MessageStore
	mailDB   *storage.Database
	queueMgr *queue.Manager
	moveMu   sync.Mutex // serialises folder moves (F6253)

	// quotaAdjust mirrors blob bytes into the account quota counter (F6680).
	quotaAdjust func(user string, delta int64) error

	// deliver routes a sent message: mailboxes on this server are delivered
	// locally, other recipients are queued for relay (F7640). When nil,
	// handleMailSend queues every recipient on queueMgr.
	deliver func(from string, to []string, data []byte) error
}

// SetDeliverFunc wires the server's delivery routing (local mailboxes versus
// outbound relay) so sends to a mailbox on this server do not depend on a DNS
// round trip to this server's own MX (F7640).
func (h *MailHandler) SetDeliverFunc(fn func(from string, to []string, data []byte) error) {
	h.deliver = fn
}

// displayRecipients turns a stored To header value into the recipient strings
// the web client shows: parsed as an address list (so a quoted name such as
// "Smoke, Admin" is not cut at its comma) with RFC 2047 names decoded (F7721).
// The result is never nil, so it serializes as a JSON array.
func displayRecipients(raw string) []string {
	if l := mimeutil.DisplayList(raw); l != nil {
		return l
	}
	return []string{}
}

// SetQueueManager wires the outbound delivery queue so sent messages are
// actually delivered. When unset, handleMailSend keeps the legacy
// store-to-Sent-only behavior.
func (h *MailHandler) SetQueueManager(qm *queue.Manager) {
	h.queueMgr = qm
}

// NewMailHandler creates a new mail handler
func NewMailHandler() *MailHandler {
	return &MailHandler{}
}

// SetStorage sets the storage backends for mail operations
func (h *MailHandler) SetStorage(msgStore *storage.MessageStore, mailDB *storage.Database) {
	h.msgStore = msgStore
	h.mailDB = mailDB
}

// sendError sends a JSON error response
func (h *MailHandler) sendError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		// Best-effort - headers already sent, log error
		fmt.Printf("ERROR: failed to encode error response: %v\n", err)
	}
}

// folderMap maps webmail folder names to internal mailbox names
var folderMap = map[string]string{
	"inbox":  "INBOX",
	"sent":   "Sent",
	"drafts": "Drafts",
	"trash":  "Trash",
	"spam":   "Junk",
}

// reverseFolderMap maps internal mailbox names to webmail folder names
var reverseFolderMap = map[string]string{
	"INBOX":  "Inbox",
	"Sent":   "Sent",
	"Drafts": "Drafts",
	"Trash":  "Trash",
	"Junk":   "Spam",
}

// handleMailList lists emails in a folder
// handleMailList returns emails in a mailbox folder
//
//	@Summary List emails in folder
//	@Description Returns a list of emails from the specified mailbox folder
//	@Tags Mail
//	@Produce json
//	@Security BearerAuth
//	@Param folder query string false "Folder name (default INBOX)"
//	@Success 200 {array} Mail "List of emails"
//	@Failure 401 {object} map[string]interface{} "Unauthorized"
//	@Router /api/v1/mail [get]
func (h *MailHandler) handleMailList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get user from context (set by auth middleware)
	user := r.Context().Value("user")
	userEmail, ok := user.(string)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Parse folder from query
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		// F5690: the webmail lists a folder via /api/v1/mail/<folder>
		// (inbox, sent, drafts, trash, spam); without this every folder
		// route returned the INBOX.
		if seg := strings.ToLower(path.Base(r.URL.Path)); folderMap[seg] != "" {
			folder = seg
		}
	}
	if folder == "" {
		folder = "INBOX"
	}

	// Map folder name to internal folder
	internalFolder := folderMap[strings.ToLower(folder)]
	if internalFolder == "" {
		internalFolder = folder
	}

	// Pagination is counted from the newest message; each page is returned
	// in ascending UID order. Bounds keep one request from reading every body.
	limit, offset := defMailListLimit, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxMailListLimit {
			h.sendError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxMailListLimit))
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			h.sendError(w, http.StatusBadRequest, "offset must be a non-negative integer")
			return
		}
		offset = n
	}

	// F6761: server-side filters so Starred/Unread views page the whole
	// folder instead of only the already-loaded pages. total then counts the
	// matching messages; unreadTotal/starredTotal are always folder-wide.
	q := r.URL.Query()
	filter := mailListFilter{}
	switch strings.ToLower(q.Get("flag")) {
	case "":
	case "flagged", "starred":
		filter.flagged = true
	default:
		h.sendError(w, http.StatusBadRequest, "flag must be flagged")
		return
	}
	switch strings.ToLower(q.Get("unread")) {
	case "", "0", "false":
	case "1", "true":
		filter.unread = true
	default:
		h.sendError(w, http.StatusBadRequest, "unread must be 1 or 0")
		return
	}

	emails, total, counts, err := h.getEmailsPageFiltered(userEmail, internalFolder, limit, offset, filter)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to list emails")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"emails": emails,
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"folder": folder,

		"unreadTotal":  counts.unread,
		"starredTotal": counts.starred,
	}); err != nil {
		fmt.Printf("ERROR: failed to encode mail list response: %v\n", err)
	}
}

// getEmailsFromStorage retrieves emails from real storage
func (h *MailHandler) getEmailsFromStorage(userEmail, mailbox string) ([]Mail, error) {
	emails, _, err := h.getEmailsPage(userEmail, mailbox, 0, 0)
	return emails, err
}

// getEmailsPage returns up to limit emails (0 = all), skipping the offset
// newest messages, plus the mailbox's total message count.
func (h *MailHandler) getEmailsPage(userEmail, mailbox string, limit, offset int) ([]Mail, int, error) {
	emails, total, _, err := h.getEmailsPageFiltered(userEmail, mailbox, limit, offset, mailListFilter{})
	return emails, total, err
}

// mailListFilter restricts a listing to flagged and/or unread messages.
type mailListFilter struct{ flagged, unread bool }

func (f mailListFilter) active() bool { return f.flagged || f.unread }

type mailListCounts struct{ unread, starred int }

func (h *MailHandler) getEmailsPageFiltered(userEmail, mailbox string, limit, offset int, filter mailListFilter) ([]Mail, int, mailListCounts, error) {
	var counts mailListCounts
	if h.mailDB == nil || h.msgStore == nil {
		return []Mail{}, 0, counts, nil
	}

	// Ensure mailbox exists - try to get it
	_, err := h.mailDB.GetMailbox(userEmail, mailbox)
	if err != nil {
		return nil, 0, counts, err
	}

	// Get message UIDs
	uids, err := h.mailDB.GetMessageUIDs(userEmail, mailbox)
	if err != nil {
		return nil, 0, counts, err
	}
	// Metadata-only pass: folder-wide counts and, when filtering, the
	// matching uid set (order preserved, so pagination stays newest-first).
	matched := uids[:0:0]
	for _, uid := range uids {
		meta, err := h.mailDB.GetMessageMetadata(userEmail, mailbox, uid)
		if err != nil || meta == nil {
			continue
		}
		seen, flagged := hasFlag(meta.Flags, "\\Seen"), hasFlag(meta.Flags, "\\Flagged")
		if !seen {
			counts.unread++
		}
		if flagged {
			counts.starred++
		}
		if (filter.flagged && !flagged) || (filter.unread && seen) {
			continue
		}
		matched = append(matched, uid)
	}
	if filter.active() {
		uids = matched
	}
	total := len(uids)
	if limit > 0 {
		end := total - offset
		if end < 0 {
			end = 0
		}
		start := end - limit
		if start < 0 {
			start = 0
		}
		uids = uids[start:end]
	}

	// F6645: copies of identical bytes get copy-qualified ids.
	pageMeta := make(map[uint32]*storage.MessageMetadata, len(uids))
	wantHashes := make(map[string]bool, len(uids))
	for _, uid := range uids {
		if meta, err := h.mailDB.GetMessageMetadata(userEmail, mailbox, uid); err == nil && meta != nil {
			pageMeta[uid] = meta
			wantHashes[meta.MessageID] = true
		}
	}
	dupHashes := h.duplicatedHashes(userEmail, wantHashes)

	emails := make([]Mail, 0, len(uids))
	for _, uid := range uids {
		meta := pageMeta[uid]
		if meta == nil {
			continue
		}
		mailID := meta.MessageID
		if dupHashes[meta.MessageID] {
			mailID = qualifiedMailID(meta.MessageID, mailbox, uid)
		}

		// Read message body
		var body string
		var rawData []byte
		if h.msgStore != nil {
			data, err := h.msgStore.ReadMessage(userEmail, meta.MessageID)
			if err == nil {
				rawData = data
				body = h.extractBody(string(data))
			}
		}

		hasAtt := false
		if h.msgStore != nil {
			hasAtt = rawHasAttachment(rawData)
		}

		// Determine preview
		preview := body
		if len(preview) > 100 {
			end := 100
			for end > 0 && !utf8.RuneStart(preview[end]) {
				end--
			}
			preview = preview[:end] + "..."
		}

		// Map folder name for response
		folderName := reverseFolderMap[mailbox]
		if folderName == "" {
			folderName = mailbox
		}

		email := Mail{
			ID:      mailID,
			From:    mimeutil.DecodeHeader(meta.From),
			To:      displayRecipients(meta.To),
			Subject: mimeutil.DecodeHeader(meta.Subject),
			Body:    body,
			Preview: preview,
			Date:    meta.InternalDate.Format(time.RFC1123Z),
			Read:    hasFlag(meta.Flags, "\\Seen"),
			Starred: hasFlag(meta.Flags, "\\Flagged"),
			Folder:  folderName,
			Size:    meta.Size,

			HasAttachments: hasAtt,
		}
		emails = append(emails, email)
	}

	return emails, total, counts, nil
}

// extractBody extracts the body from a raw email message
func (h *MailHandler) extractBody(raw string) string {
	// Find the header/body separator
	sep := "\r\n\r\n"
	idx := strings.Index(raw, sep)
	if idx == -1 {
		sep = "\n\n"
		idx = strings.Index(raw, sep)
	}
	if idx == -1 {
		return raw
	}
	return strings.TrimSpace(raw[idx+len(sep):])
}

// hasFlag checks if a flag is present
func hasFlag(flags []string, flag string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, flag) {
			return true
		}
	}
	return false
}

// handleMailGet gets a single email
func (h *MailHandler) handleMailGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	user := r.Context().Value("user")
	userEmail, ok := user.(string)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	emailID := r.URL.Query().Get("id")
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}

	internalFolder := folderMap[strings.ToLower(folder)]
	if internalFolder == "" {
		internalFolder = folder
	}

	email, err := h.getEmailFromStorage(userEmail, internalFolder, emailID)
	if errors.Is(err, errMailBodyRead) {
		h.sendError(w, http.StatusInternalServerError, "Failed to read email")
		return
	}
	if err != nil || email == nil {
		h.sendError(w, http.StatusNotFound, "Email not found")
		return
	}

	// Mark as read
	h.markAsRead(userEmail, internalFolder, emailID)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(email); err != nil {
		fmt.Printf("ERROR: failed to encode email response: %v\n", err)
	}
}

// getEmailFromStorage retrieves a single email from storage
func (h *MailHandler) getEmailFromStorage(userEmail, mailbox, messageID string) (*Mail, error) {
	if h.mailDB == nil || h.msgStore == nil {
		return nil, fmt.Errorf("storage not available")
	}

	_, meta, found := h.findUIDByMessageID(userEmail, mailbox, messageID)
	if !found {
		return nil, fmt.Errorf("email not found")
	}
	// Read message body
	data, err := h.msgStore.ReadMessage(userEmail, meta.MessageID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMailBodyRead, err)
	}
	body := h.extractBody(string(data))

	folderName := reverseFolderMap[mailbox]
	if folderName == "" {
		folderName = mailbox
	}

	return &Mail{
		ID:      messageID, // F6645: echo the (possibly copy-qualified) id asked for
		From:    mimeutil.DecodeHeader(meta.From),
		To:      displayRecipients(meta.To),
		Subject: mimeutil.DecodeHeader(meta.Subject),
		Body:    body,
		Preview: body,
		Date:    meta.InternalDate.Format(time.RFC1123Z),
		Read:    hasFlag(meta.Flags, "\\Seen"),
		Starred: hasFlag(meta.Flags, "\\Flagged"),
		Folder:  folderName,
		Size:    meta.Size,

		HasAttachments: rawHasAttachment(data),
		MessageIDHdr:   rawMessageIDHeader(data),
	}, nil
}

// markAsRead marks a message as read
func (h *MailHandler) markAsRead(userEmail, mailbox, messageID string) {
	if h.mailDB == nil {
		return
	}

	uid, _, found := h.findUIDByMessageID(userEmail, mailbox, messageID)
	if !found {
		return
	}
	// Atomic read-modify-write: the flag check and write happen in
	// one storage transaction, so a concurrent flag change (an IMAP
	// keyword, another session) can never be clobbered by a stale copy.
	_ = h.mailDB.UpdateMessageMetadataFunc(userEmail, mailbox, uid, func(m *storage.MessageMetadata) error {
		if !hasFlag(m.Flags, "\\Seen") {
			m.Flags = append(m.Flags, "\\Seen")
		}
		return nil
	})
}

// isBareAddress reports whether s is exactly one RFC 5322 addr-spec with no
// display name, angle brackets or surrounding text (F5137).
func isBareAddress(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Name == "" && addr.Address == s
}

// sanitizeHeaderValue removes CR/LF characters to prevent SMTP header injection.
func sanitizeHeaderValue(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", "")
}

// sanitizeHeaderValues applies sanitizeHeaderValue to each element in a slice.
func sanitizeHeaderValues(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = sanitizeHeaderValue(v)
	}
	return out
}

// handleMailSend sends an email and stores it in Sent folder
//
//	@Summary Send email
//	@Description Sends an email and stores it in the Sent folder
//	@Tags Mail
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Param email body map[string]interface{} true "Email data"
//	@Success 200 {object} map[string]interface{} "Email sent"
//	@Failure 401 {object} map[string]interface{} "Unauthorized"
//	@Router /api/v1/mail/send [post]
func (h *MailHandler) handleMailSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	user := r.Context().Value("user")
	userEmail, ok := user.(string)
	if !ok {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req SendMailRequest
	if err := decodeJSON(r, &req); err != nil {
		h.sendError(w, jsonErrStatus(err), jsonErrMsg(err, "Invalid request"))
		return
	}

	if len(req.To) == 0 {
		h.sendError(w, http.StatusBadRequest, "Recipient required")
		return
	}

	// Validate recipient count. F5136: the cap applies to every envelope
	// recipient (To + Cc + Bcc); counting only To let Bcc carry any number.
	if len(req.To)+len(req.CC)+len(req.BCC) > 100 {
		h.sendError(w, http.StatusBadRequest, "Too many recipients (max 100)")
		return
	}

	// F5137: every recipient becomes an SMTP envelope address (RCPT TO), so
	// it must be a bare addr-spec. Anything else (empty, display-name form,
	// trailing "> NOTIFY=..." ESMTP parameters) was queued verbatim.
	for _, list := range [][]string{req.To, req.CC, req.BCC} {
		for _, rcpt := range list {
			if !isBareAddress(rcpt) {
				h.sendError(w, http.StatusBadRequest, "Invalid recipient address")
				return
			}
		}
	}

	// Validate subject length
	if len(req.Subject) > 998 {
		h.sendError(w, http.StatusBadRequest, "Subject too long (max 998 characters)")
		return
	}

	// Validate body length (prevent memory issues)
	if len(req.Body) > 25*1024*1024 {
		h.sendError(w, http.StatusBadRequest, "Message body too large (max 25MB)")
		return
	}

	atts, err := validateAttachments(req.Attachments)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Build RFC 2822 email
	now := time.Now()
	dateStr := now.Format("Mon, 02 Jan 2006 15:04:05 -0700")

	// Sanitize header values to prevent CRLF injection
	safeSubject := sanitizeHeaderValue(req.Subject)
	safeTo := sanitizeHeaderValues(req.To)
	safeCC := sanitizeHeaderValues(req.CC)
	safeBCC := sanitizeHeaderValues(req.BCC)

	// Build headers
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", userEmail))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(safeTo, ", ")))
	if len(safeCC) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(safeCC, ", ")))
	}
	// F5990: non-ASCII subjects must be RFC 2047 encoded-words; raw 8-bit
	// header bytes are invalid and get mangled by relays.
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", safeSubject)))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", dateStr))
	if v := threadingHeaderValue(req.InReplyTo); v != "" {
		sb.WriteString("In-Reply-To: " + v + "\r\n")
	}
	if refs := threadingHeaderValues(req.References, req.InReplyTo); len(refs) > 0 {
		sb.WriteString("References: " + strings.Join(refs, " ") + "\r\n")
	}
	sb.WriteString("MIME-Version: 1.0\r\n")
	if len(atts) > 0 {
		ct, mixed, err := buildMixedBody(req.Body, atts)
		if err != nil {
			h.sendError(w, http.StatusBadRequest, err.Error())
			return
		}
		sb.WriteString("Content-Type: " + ct + "\r\n\r\n")
		sb.Write(mixed)
	} else {
		sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(req.Body)
	}

	rawEmail := sb.String()
	if len(rawEmail) > 25*1024*1024 {
		h.sendError(w, http.StatusRequestEntityTooLarge, "Message too large (max 25MB)")
		return
	}

	// Queue the message for outbound delivery to all envelope recipients
	// (To, Cc, Bcc). Bcc addresses must not appear in the message headers
	// but still receive the mail. Without a wired queue (direct-handler
	// tests), keep the legacy store-to-Sent-only behavior.
	recipients := append(append(append([]string{}, safeTo...), safeCC...), safeBCC...)
	if h.deliver != nil {
		if err := h.deliver(userEmail, recipients, []byte(rawEmail)); err != nil {
			h.sendError(w, http.StatusInternalServerError, "Failed to deliver message")
			return
		}
	} else if h.queueMgr != nil {
		if _, err := h.queueMgr.Enqueue(userEmail, recipients, []byte(rawEmail)); err != nil {
			h.sendError(w, http.StatusInternalServerError, "Failed to queue message for delivery")
			return
		}
	}

	// Store the message
	var msgID string
	if h.msgStore != nil && h.mailDB != nil {
		// Ensure Sent mailbox exists
		_ = h.mailDB.CreateMailbox(userEmail, "Sent")

		// Store message file - msgID is the hash-based ID returned by StoreMessage
		storedMsgID, err := h.msgStore.StoreMessage(userEmail, []byte(rawEmail))
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, "Failed to store message")
			return
		}
		if storedMsgID == "" {
			h.sendError(w, http.StatusInternalServerError, "Failed to store message: no ID returned")
			return
		}
		msgID = storedMsgID
		if err := h.chargeNewBlob(userEmail, storedMsgID, int64(len(rawEmail))); err != nil {
			h.sendError(w, http.StatusInsufficientStorage, "Quota exceeded")
			return
		}

		// Get next UID for Sent mailbox
		uid, err := h.mailDB.GetNextUID(userEmail, "Sent")
		if err != nil {
			h.sendError(w, http.StatusInternalServerError, "Failed to get next UID")
			return
		}

		// Parse headers for metadata
		subject := safeSubject
		from := userEmail
		to := strings.Join(safeTo, ", ")

		// Store metadata with the hash-based message ID
		meta := &storage.MessageMetadata{
			MessageID:    msgID,
			UID:          uid,
			Flags:        []string{"\\Seen"},
			InternalDate: now,
			Size:         int64(len(rawEmail)),
			Subject:      subject,
			Date:         dateStr,
			From:         from,
			To:           to,
			InReplyTo:    threadingHeaderValue(req.InReplyTo),
			References:   threadingHeaderValues(req.References, req.InReplyTo),
		}
		if err := h.mailDB.StoreMessageMetadata(userEmail, "Sent", uid, meta); err != nil {
			fmt.Printf("ERROR: failed to store message metadata: %v\n", err)
			h.sendError(w, http.StatusInternalServerError, "Failed to store message metadata")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"message": "Email sent successfully",
		"id":      msgID,
	}); err != nil {
		fmt.Printf("ERROR: failed to encode send confirmation: %v\n", err)
	}
}

// handleMailDelete deletes an email (moves to trash)
//
//	@Summary Delete email
//	@Description Moves an email to the Trash folder
//	@Tags Mail
//	@Produce json
//	@Security BearerAuth
//	@Param id query string true "Message ID"
//	@Success 200 {object} map[string]string "Email deleted"
//	@Failure 401 {object} map[string]interface{} "Unauthorized"
//	@Router /api/v1/mail/delete [delete]
func (h *MailHandler) handleMailDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userEmail, ok := r.Context().Value("user").(string)
	if !ok || userEmail == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Extract message ID from query string or request body
	var messageID string

	// Try query string first (DELETE /api/v1/mail/delete?id=xxx)
	messageID = r.URL.Query().Get("id")

	// If not in query, try form/body
	if messageID == "" {
		if r.FormValue("id") != "" {
			messageID = r.FormValue("id")
		}
	}

	if messageID == "" {
		h.sendError(w, http.StatusBadRequest, "Message ID required")
		return
	}

	// F6645: a copy-qualified id removes exactly that (mailbox, uid) entry;
	// the blob goes only once no other entry uses it.
	if hash, mb, uid, ok := parseQualifiedMailID(messageID); ok && h.mailDB != nil {
		if meta, err := h.mailDB.GetMessageMetadata(userEmail, mb, uid); err == nil && meta != nil && meta.MessageID == hash {
			_ = h.mailDB.DeleteMessage(userEmail, mb, uid)
			if h.msgStore != nil && !h.blobStillReferenced(userEmail, hash) {
				h.removeBlob(userEmail, hash)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Email deleted", "id": messageID})
		return
	}

	// Drop the metadata rows first: the blob delete is reference-aware and
	// only removes the file once no row (or pending upload) still uses it.
	if h.mailDB != nil {
		// Search through all mailboxes to find the message
		h.deleteMessageMetadata(userEmail, messageID)
	}

	// Delete from message store (actual file)
	if h.msgStore != nil {
		h.removeBlob(userEmail, messageID)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"message": "Email deleted",
		"id":      messageID,
	}); err != nil {
		fmt.Printf("ERROR: failed to encode delete confirmation: %v\n", err)
	}
}

// deleteMessageMetadata finds and deletes message metadata by messageID
func (h *MailHandler) deleteMessageMetadata(userEmail, messageID string) {
	mailboxes, err := h.mailDB.ListMailboxes(userEmail)
	if err != nil {
		return
	}
	for _, mailbox := range mailboxes {
		// Get all UIDs in this mailbox
		uids, err := h.mailDB.GetMessageUIDs(userEmail, mailbox)
		if err != nil {
			continue
		}
		// Find the message with matching ID and delete it
		for _, uid := range uids {
			meta, err := h.mailDB.GetMessageMetadata(userEmail, mailbox, uid)
			if err != nil {
				continue
			}
			if meta.MessageID == messageID {
				_ = h.mailDB.DeleteMessage(userEmail, mailbox, uid)
			}
		}
	}
}

// InitDemoEmails is a no-op - demo emails are now stored in real storage
// This function is kept for backwards compatibility
func InitDemoEmails(userEmail string) {
	// No-op: demo emails are now in real storage
	// Users receive demo emails when accounts are first created via setup scripts
}
