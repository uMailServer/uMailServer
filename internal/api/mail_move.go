package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/umailserver/umailserver/internal/storage"
)

const (
	maxMoveIDs      = 1000
	maxMoveBodySize = 1 << 20
)

// MoveMailRequest is the body of POST /api/v1/mail/move. Either id or ids
// (batch) names the messages; from and to are webmail folder names
// (inbox, sent, drafts, trash, spam) or custom mailbox names.
type MoveMailRequest struct {
	ID   string   `json:"id"`
	IDs  []string `json:"ids"`
	From string   `json:"from"`
	To   string   `json:"to"`
}

// resolveFolderName maps a webmail folder name to the internal mailbox name
// and rejects names that cannot be a mailbox.
func resolveFolderName(folder string) (string, bool) {
	folder = strings.TrimSpace(folder)
	if folder == "" || len(folder) > 255 {
		return "", false
	}
	for _, r := range folder {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	if internal := folderMap[strings.ToLower(folder)]; internal != "" {
		return internal, true
	}
	if strings.EqualFold(folder, "INBOX") {
		return "INBOX", true
	}
	return folder, true
}

func validMessageID(id string) bool {
	if id == "" || len(id) > 512 {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// mailboxExists reports whether the user has the mailbox. The standard
// folders the webmail offers exist implicitly (created on first use).
func (h *MailHandler) mailboxExists(user, internal string) bool {
	for _, std := range folderMap {
		if std == internal {
			return true
		}
	}
	names, err := h.mailDB.ListMailboxes(user)
	if err != nil {
		return false
	}
	for _, n := range names {
		if n == internal {
			return true
		}
	}
	return false
}

// findUIDByMessageID returns the UID of messageID in mailbox.
func (h *MailHandler) findUIDByMessageID(user, mailbox, messageID string) (uint32, *storage.MessageMetadata, bool) {
	uids, err := h.mailDB.GetMessageUIDs(user, mailbox)
	if err != nil {
		return 0, nil, false
	}
	for _, uid := range uids {
		meta, err := h.mailDB.GetMessageMetadata(user, mailbox, uid)
		if err != nil || meta == nil {
			continue
		}
		if meta.MessageID == messageID {
			return uid, meta, true
		}
	}
	return 0, nil, false
}

// handleMailMove moves messages between folders of the caller's mailbox
// (F6253: the webmail "Restore" from Trash had no move endpoint and fell back
// to the permanent delete).
//
//	@Summary Move emails between folders
//	@Tags Mail
//	@Accept json
//	@Produce json
//	@Security BearerAuth
//	@Param body body MoveMailRequest true "id or ids, from, to"
//	@Success 200 {object} map[string]interface{} "Moved"
//	@Router /api/v1/mail/move [post]
//
// Like IMAP MOVE (RFC 6851) each message gets a new UID in the destination
// and its flags are kept; the message body is content-addressed and is not
// touched, so no bytes are written and quota use is unchanged. All messages
// are validated before any is moved, so a 404/409 leaves everything in place.
func (h *MailHandler) handleMailMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	userEmail, ok := r.Context().Value("user").(string)
	if !ok || userEmail == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil {
		h.sendError(w, http.StatusServiceUnavailable, "Mail storage not available")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMoveBodySize)
	var req MoveMailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if len(ids) > maxMoveIDs {
		h.sendError(w, http.StatusBadRequest, fmt.Sprintf("Too many messages (max %d)", maxMoveIDs))
		return
	}
	from, okFrom := resolveFolderName(req.From)
	to, okTo := resolveFolderName(req.To)
	if !okFrom || !okTo {
		h.sendError(w, http.StatusBadRequest, "Valid from and to folders required")
		return
	}
	if from == to {
		h.sendError(w, http.StatusBadRequest, "Source and destination folders are the same")
		return
	}

	h.moveMu.Lock()
	defer h.moveMu.Unlock()

	if !h.mailboxExists(userEmail, from) || !h.mailboxExists(userEmail, to) {
		h.sendError(w, http.StatusNotFound, "Folder not found")
		return
	}

	type pending struct {
		id   string
		uid  uint32
		meta *storage.MessageMetadata
	}
	plan := make([]pending, 0, len(ids))
	for _, id := range ids {
		uid, meta, found := h.findUIDByMessageID(userEmail, from, id)
		if !found {
			h.sendError(w, http.StatusNotFound, "Email not found")
			return
		}
		if _, _, dup := h.findUIDByMessageID(userEmail, to, id); dup {
			h.sendError(w, http.StatusConflict, "Email already exists in destination folder")
			return
		}
		plan = append(plan, pending{id: id, uid: uid, meta: meta})
	}

	if err := h.mailDB.CreateMailbox(userEmail, to); err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to move emails")
		return
	}
	moved := make([]string, 0, len(plan))
	newUIDs := make(map[string]uint32, len(plan))
	var moveErr error
	for _, p := range plan {
		if moveErr = h.moveOne(userEmail, from, to, p.uid, p.meta); moveErr != nil {
			break
		}
		moved = append(moved, p.id)
		newUIDs[p.id] = p.meta.UID
	}
	if moveErr != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "Failed to move all emails",
			"moved": moved,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"moved": moved,
		"from":  req.From,
		"to":    req.To,
		"uids":  newUIDs,
	})
}

// moveOne copies the metadata into the destination under a fresh UID and
// removes the source entry, destination first so a crash in between leaves a
// duplicate (recoverable) rather than a lost message. meta.UID is updated to
// the new UID.
func (h *MailHandler) moveOne(user, from, to string, srcUID uint32, meta *storage.MessageMetadata) error {
	newUID, err := h.mailDB.GetNextUID(user, to)
	if err != nil {
		return err
	}
	modSeq, err := h.mailDB.GetNextModSeq(user, to)
	if err != nil {
		return err
	}
	moved := *meta
	moved.Flags = append([]string(nil), meta.Flags...)
	moved.UID = newUID
	moved.ModSeq = modSeq
	if err := h.mailDB.StoreMessageMetadata(user, to, newUID, &moved); err != nil {
		return err
	}
	if err := h.mailDB.DeleteMessage(user, from, srcUID); err != nil {
		return err
	}
	// The create/destroy pair above is recorded in that order; end the
	// journal with an update so JMAP clients still see the email as alive.
	_ = h.mailDB.RecordChange(user, storage.ChangeTypeEmail, storage.ChangeKindUpdated, meta.MessageID, to)
	meta.UID = newUID
	return nil
}
