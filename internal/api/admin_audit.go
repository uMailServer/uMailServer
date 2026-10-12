package api

import (
	"net/http"

	"github.com/umailserver/umailserver/internal/audit"
)

// auditAdmin records a successful administrative mutation (F7370).
func (s *Server) auditAdmin(r *http.Request, action, target string) {
	if s.auditLogger == nil {
		return
	}
	actor, _ := r.Context().Value("user").(string)
	if actor == "" {
		actor = "system"
	}
	s.auditLogger.LogAdminAction(actor, audit.ExtractIP(r), action, target)
}
