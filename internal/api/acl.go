package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/umailserver/umailserver/internal/storage"
)

// handleACLHandler handles GET and POST for /api/v1/mailboxes/{owner}/{mailbox}/acl
func (s *Server) handleACLHandler(w http.ResponseWriter, r *http.Request) {
	// Parse owner and mailbox from URL path
	// Path: /api/v1/mailboxes/{owner}/{mailbox}/acl
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/mailboxes/"), "/", 3)
	if len(pathParts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	owner := pathParts[0]
	mailbox := pathParts[1]

	switch r.Method {
	case http.MethodGet:
		s.handleACLGet(w, r, owner, mailbox)
	case http.MethodPost:
		s.handleACLCreate(w, r, owner, mailbox)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleACLDeleteHandler handles DELETE for /api/v1/mailboxes/{owner}/{mailbox}/acl/{grantee}
func (s *Server) handleACLDeleteHandler(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/mailboxes/{owner}/{mailbox}/acl/{grantee}
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/mailboxes/"), "/", 4)
	if len(pathParts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	owner := pathParts[0]
	mailbox := pathParts[1]
	grantee := pathParts[3]

	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isAdmin, _ := r.Context().Value("isAdmin").(bool)
	if user != owner && !isAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	err := s.mailDB.DeleteACL(owner, mailbox, grantee)
	if err != nil {
		http.Error(w, "Failed to delete ACL", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "ACL entry deleted",
	})
}

// handleACLGet retrieves ACL entries for a mailbox
func (s *Server) handleACLGet(w http.ResponseWriter, r *http.Request, owner, mailbox string) {
	entries, err := s.mailDB.ListACL(owner, mailbox)
	if err != nil {
		http.Error(w, "Failed to list ACL", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"owner":   owner,
		"mailbox": mailbox,
		"acl":     entries,
	})
}

// handleACLCreate creates or updates an ACL entry
func (s *Server) handleACLCreate(w http.ResponseWriter, r *http.Request, owner, mailbox string) {
	var req struct {
		Grantee string `json:"grantee"`
		Rights  uint8  `json:"rights"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Grantee == "" {
		http.Error(w, "Grantee required", http.StatusBadRequest)
		return
	}

	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	isAdmin, _ := r.Context().Value("isAdmin").(bool)
	if user != owner && !isAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	err := s.mailDB.SetACL(owner, mailbox, req.Grantee, storage.ACLRights(req.Rights), user)
	if err != nil {
		http.Error(w, "Failed to set ACL", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "ACL entry created",
	})
}

// handleMyRightsHandler handles GET for /api/v1/mailboxes/{owner}/{mailbox}/myrights
func (s *Server) handleMyRightsHandler(w http.ResponseWriter, r *http.Request) {
	// Parse owner and mailbox from URL path
	// Path: /api/v1/mailboxes/{owner}/{mailbox}/myrights
	pathParts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/mailboxes/"), "/", 3)
	if len(pathParts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	owner := pathParts[0]
	mailbox := pathParts[1]
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var rights storage.ACLRights

	if owner == user {
		rights = storage.ACLAll
	} else {
		aclRights, err := s.mailDB.GetACL(owner, mailbox, user)
		rights = storage.ACLRights(aclRights)
		if err != nil {
			http.Error(w, "Failed to get rights", http.StatusInternalServerError)
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"owner":   owner,
		"mailbox": mailbox,
		"rights":  rights.String(),
	})
}

// handleSharedMailboxesList handles GET for /api/v1/mailboxes/shared
func (s *Server) handleSharedMailboxesList(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mailboxes, err := s.mailDB.ListMailboxesSharedWith(user)
	if err != nil {
		http.Error(w, "Failed to list shared mailboxes", http.StatusInternalServerError)
		return
	}

	// Parse owner:mailbox format
	result := make([]map[string]string, 0, len(mailboxes))
	for _, m := range mailboxes {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) == 2 {
			result = append(result, map[string]string{
				"owner":   parts[0],
				"mailbox": parts[1],
			})
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"shared_mailboxes": result,
	})
}

// handleGranteesMailboxesList handles GET for /api/v1/mailboxes/shared-as-owner
func (s *Server) handleGranteesMailboxesList(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mailboxes, err := s.mailDB.ListGranteesMailboxes(user)
	if err != nil {
		http.Error(w, "Failed to list shared mailboxes", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"shared_as_owner": mailboxes,
	})
}
