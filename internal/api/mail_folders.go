package api

import (
	"net/http"
	"sort"
	"strings"
)

// MailFolder is one mailbox in GET /api/v1/mail/folders. Role is the
// webmail name of a standard folder (inbox, sent, drafts, trash, spam) and
// empty for the user's own folders.
type MailFolder struct {
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Total  int    `json:"total"`
	Unread int    `json:"unread"`
}

// standardFolderOrder lists the standard folders in display order.
var standardFolderOrder = []string{"inbox", "sent", "drafts", "trash", "spam"}

// handleMailFolders lists the signed-in user's mailboxes with message and
// unread counts. The webmail showed fixed placeholder folders ("Work",
// "Personal") and could not reach folders created over IMAP or by filters.
func (h *MailHandler) handleMailFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		h.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		h.sendError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.mailDB == nil {
		h.sendError(w, http.StatusServiceUnavailable, "mail storage not available")
		return
	}
	names, err := h.mailDB.ListMailboxes(user)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, "Failed to list folders")
		return
	}
	roleOf := map[string]string{}
	for role, internal := range folderMap {
		roleOf[internal] = role
	}
	rank := map[string]int{}
	for i, role := range standardFolderOrder {
		rank[role] = i
	}
	folders := make([]MailFolder, 0, len(names))
	for _, name := range names {
		total, _, unread, err := h.mailDB.GetMailboxCounts(user, name)
		if err != nil {
			continue
		}
		folders = append(folders, MailFolder{Name: name, Role: roleOf[name], Total: total, Unread: unread})
	}
	sort.SliceStable(folders, func(i, j int) bool {
		a, b := folders[i], folders[j]
		if (a.Role != "") != (b.Role != "") {
			return a.Role != ""
		}
		if a.Role != "" {
			return rank[a.Role] < rank[b.Role]
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	h.writeJSON(w, map[string]interface{}{"folders": folders})
}
