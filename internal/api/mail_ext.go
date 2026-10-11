package api

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

const (
	maxFlagIDs        = 1000
	maxFlagBodySize   = 1 << 20
	maxDraftBodySize  = 26 << 20
	maxDraftRecipient = 100
	maxMailListLimit  = 500
	defMailListLimit  = 100
)

// allowedFlags is the closed set of system flags the webmail may toggle.
// Lookup key is the lower-cased flag with an optional leading backslash removed.
var allowedFlags = map[string]string{
	"seen":     "\\Seen",
	"flagged":  "\\Flagged",
	"answered": "\\Answered",
	"draft":    "\\Draft",
}

func canonicalFlag(f string) (string, bool) {
	c, ok := allowedFlags[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "\\"))]
	return c, ok
}

// FlagsRequest is the body of POST /api/v1/mail/flags.
type FlagsRequest struct {
	ID     string   `json:"id"`
	IDs    []string `json:"ids"`
	Folder string   `json:"folder"`
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// DraftRequest is the body of POST/PUT /api/v1/mail/draft.
type DraftRequest struct {
	ID      string   `json:"id"`
	To      []string `json:"to"`
	CC      []string `json:"cc"`
	BCC     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

func (h *MailHandler) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// requireJSON rejects non-JSON bodies on the mail write endpoints.
func (h *MailHandler) requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		h.sendError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}

// searchMailboxes lists the mailboxes a message lookup covers: the named one,
// or every mailbox of the caller when folder is empty.
func (h *MailHandler) lookupMailboxes(user, folder string) ([]string, bool) {
	if strings.TrimSpace(folder) != "" {
		internal, ok := resolveFolderName(folder)
		if !ok {
			return nil, false
		}
		return []string{internal}, true
	}
	names, err := h.mailDB.ListMailboxes(user)
	if err != nil {
		return nil, true
	}
	return names, true
}

func (h *MailHandler) findAnywhere(user, folder, id string) (string, uint32, *storage.MessageMetadata, bool) {
	boxes, _ := h.lookupMailboxes(user, folder)
	for _, mb := range boxes {
		if uid, meta, ok := h.findUIDByMessageID(user, mb, id); ok {
			return mb, uid, meta, true
		}
	}
	return "", 0, nil, false
}

// handleMailMessage returns one message of the caller.
//
//	GET /api/v1/mail/message?id=<id>[&folder=<folder>]
//
// Without folder every mailbox of the caller is searched. The caller's own
// mailboxes are the only scope, so another user's id is a 404. The request
// is read-only; use POST /api/v1/mail/flags to mark it read.
func (h *MailHandler) handleMailMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil || h.msgStore == nil {
		h.sendError(w, http.StatusServiceUnavailable, "Mail storage not available")
		return
	}
	id := r.URL.Query().Get("id")
	if !validMessageID(id) {
		h.sendError(w, http.StatusBadRequest, "Valid message id required")
		return
	}
	folder := r.URL.Query().Get("folder")
	if _, ok := h.lookupMailboxes(user, folder); !ok {
		h.sendError(w, http.StatusBadRequest, "Invalid folder")
		return
	}
	mb, _, _, found := h.findAnywhere(user, folder, id)
	if !found {
		h.sendError(w, http.StatusNotFound, "Email not found")
		return
	}
	email, err := h.getEmailFromStorage(user, mb, id)
	if err != nil || email == nil {
		if err != nil && strings.Contains(err.Error(), errMailBodyRead.Error()) {
			h.sendError(w, http.StatusInternalServerError, "Failed to read email")
			return
		}
		h.sendError(w, http.StatusNotFound, "Email not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.writeJSON(w, email)
}

// handleMailFlags sets or clears system flags on messages of the caller.
//
//	POST /api/v1/mail/flags {"id"|"ids":[...], "folder":"", "add":["\\Seen"], "remove":["\\Flagged"]}
func (h *MailHandler) handleMailFlags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil {
		h.sendError(w, http.StatusServiceUnavailable, "Mail storage not available")
		return
	}
	if !h.requireJSON(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFlagBodySize)
	var req FlagsRequest
	if err := decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	ids := make([]string, 0, len(req.IDs)+1)
	seen := map[string]bool{}
	for _, id := range append([]string{req.ID}, req.IDs...) {
		if id == "" {
			continue
		}
		if !validMessageID(id) {
			h.sendError(w, http.StatusBadRequest, "Invalid message ID")
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		h.sendError(w, http.StatusBadRequest, "Message ID required")
		return
	}
	if len(ids) > maxFlagIDs {
		h.sendError(w, http.StatusBadRequest, fmt.Sprintf("Too many messages (max %d)", maxFlagIDs))
		return
	}
	if len(req.Add)+len(req.Remove) == 0 || len(req.Add)+len(req.Remove) > 16 {
		h.sendError(w, http.StatusBadRequest, "add or remove flags required")
		return
	}
	add, remove := map[string]bool{}, map[string]bool{}
	for _, f := range req.Add {
		c, ok := canonicalFlag(f)
		if !ok {
			h.sendError(w, http.StatusBadRequest, "Unsupported flag (allowed: \\Seen, \\Flagged, \\Answered, \\Draft)")
			return
		}
		add[c] = true
	}
	for _, f := range req.Remove {
		c, ok := canonicalFlag(f)
		if !ok {
			h.sendError(w, http.StatusBadRequest, "Unsupported flag (allowed: \\Seen, \\Flagged, \\Answered, \\Draft)")
			return
		}
		if add[c] {
			h.sendError(w, http.StatusBadRequest, "Flag cannot be both added and removed")
			return
		}
		remove[c] = true
	}
	if _, ok := h.lookupMailboxes(user, req.Folder); !ok {
		h.sendError(w, http.StatusBadRequest, "Invalid folder")
		return
	}

	h.moveMu.Lock() // serialise with folder moves so a UID is not stale
	defer h.moveMu.Unlock()

	type target struct {
		mb  string
		uid uint32
		id  string
	}
	plan := make([]target, 0, len(ids))
	for _, id := range ids {
		mb, uid, _, found := h.findAnywhere(user, req.Folder, id)
		if !found {
			h.sendError(w, http.StatusNotFound, "Email not found")
			return
		}
		plan = append(plan, target{mb, uid, id})
	}
	updated := make([]string, 0, len(plan))
	for _, t := range plan {
		err := h.mailDB.UpdateMessageMetadataFunc(user, t.mb, t.uid, func(m *storage.MessageMetadata) error {
			if m.MessageID != t.id {
				return errMailNotFound // row vanished; never create a ghost
			}
			out := m.Flags[:0:0]
			for _, f := range m.Flags {
				if c, ok := canonicalFlag(f); ok && (remove[c] || add[c]) {
					continue // re-added below, or dropped
				}
				out = append(out, f)
			}
			for _, f := range []string{"\\Seen", "\\Flagged", "\\Answered", "\\Draft"} {
				if add[f] {
					out = append(out, f)
				}
			}
			m.Flags = out
			return nil
		})
		if err != nil {
			status, msg := http.StatusInternalServerError, "Failed to update flags"
			if err == errMailNotFound {
				status, msg = http.StatusNotFound, "Email not found"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "updated": updated})
			return
		}
		_ = h.mailDB.RecordChange(user, storage.ChangeTypeEmail, storage.ChangeKindUpdated, t.id, t.mb)
		updated = append(updated, t.id)
	}
	h.writeJSON(w, map[string]interface{}{"updated": updated})
}

var errMailNotFound = fmt.Errorf("email not found")

// handleMailDraft saves a draft to the caller's Drafts folder.
//
//	POST /api/v1/mail/draft  creates a draft (an "id" in the body replaces that draft)
//	PUT  /api/v1/mail/draft  updates the draft named by "id" (404 when absent)
//
// Message bodies are content-addressed, so an edited draft gets a new id; the
// response carries the current id and the superseded draft is removed.
func (h *MailHandler) handleMailDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil || h.msgStore == nil {
		h.sendError(w, http.StatusServiceUnavailable, "Mail storage not available")
		return
	}
	if !h.requireJSON(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDraftBodySize)
	var req DraftRequest
	if err := decodeJSON(r, &req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if r.Method == http.MethodPut && req.ID == "" {
		h.sendError(w, http.StatusBadRequest, "Draft id required")
		return
	}
	if req.ID != "" && !validMessageID(req.ID) {
		h.sendError(w, http.StatusBadRequest, "Invalid message ID")
		return
	}
	if len(req.To)+len(req.CC)+len(req.BCC) > maxDraftRecipient {
		h.sendError(w, http.StatusBadRequest, "Too many recipients (max 100)")
		return
	}
	for _, list := range [][]string{req.To, req.CC, req.BCC} {
		for _, rcpt := range list {
			if !isBareAddress(rcpt) {
				h.sendError(w, http.StatusBadRequest, "Invalid recipient address")
				return
			}
		}
	}
	if len(req.Subject) > 998 {
		h.sendError(w, http.StatusBadRequest, "Subject too long (max 998 characters)")
		return
	}
	if len(req.Body) > 25*1024*1024 {
		h.sendError(w, http.StatusBadRequest, "Message body too large (max 25MB)")
		return
	}

	h.moveMu.Lock()
	defer h.moveMu.Unlock()

	var oldUID uint32
	var oldFlags []string
	if req.ID != "" {
		uid, meta, found := h.findUIDByMessageID(user, "Drafts", req.ID)
		if !found {
			h.sendError(w, http.StatusNotFound, "Draft not found")
			return
		}
		oldUID, oldFlags = uid, meta.Flags
	}

	now := time.Now()
	dateStr := now.Format("Mon, 02 Jan 2006 15:04:05 -0700")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", user))
	if len(req.To) > 0 {
		sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(sanitizeHeaderValues(req.To), ", ")))
	}
	if len(req.CC) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(sanitizeHeaderValues(req.CC), ", ")))
	}
	// Bcc is kept in the draft only so the composer can restore it.
	if len(req.BCC) > 0 {
		sb.WriteString(fmt.Sprintf("Bcc: %s\r\n", strings.Join(sanitizeHeaderValues(req.BCC), ", ")))
	}
	safeSubject := sanitizeHeaderValue(req.Subject)
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", safeSubject)))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", dateStr))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	sb.WriteString(req.Body)
	raw := sb.String()

	if err := h.mailDB.CreateMailbox(user, "Drafts"); err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}
	newID, err := h.msgStore.StoreMessage(user, []byte(raw))
	if err != nil || newID == "" {
		h.sendError(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}

	if newID == req.ID {
		// Identical content: nothing to replace.
		h.writeJSON(w, map[string]interface{}{"id": newID, "folder": "Drafts", "saved": true})
		return
	}

	uid, err := h.mailDB.GetNextUID(user, "Drafts")
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}
	flags := []string{"\\Draft", "\\Seen"}
	for _, f := range oldFlags {
		if c, ok := canonicalFlag(f); ok && c == "\\Flagged" {
			flags = append(flags, c)
		}
	}
	meta := &storage.MessageMetadata{
		MessageID: newID, UID: uid, Flags: flags, InternalDate: now,
		Size: int64(len(raw)), Subject: safeSubject, Date: dateStr,
		From: user, To: strings.Join(sanitizeHeaderValues(req.To), ", "),
	}
	if err := h.mailDB.StoreMessageMetadata(user, "Drafts", uid, meta); err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}
	_ = h.mailDB.RecordChange(user, storage.ChangeTypeEmail, storage.ChangeKindCreated, newID, "Drafts")

	if req.ID != "" {
		// New row is durable first; removing the superseded one last means a
		// crash leaves a duplicate draft rather than losing it.
		if err := h.mailDB.DeleteMessage(user, "Drafts", oldUID); err == nil {
			_ = h.msgStore.DeleteMessage(user, req.ID) // reference-aware
		}
	}
	h.writeJSON(w, map[string]interface{}{"id": newID, "folder": "Drafts", "saved": true})
}
