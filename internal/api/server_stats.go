package api

import (
	"net/http"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	domains, err := s.db.ListDomains()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	// Count accounts across all domains
	accounts := 0
	for _, d := range domains {
		accts, _ := s.db.ListAccountsByDomain(d.Name)
		accounts += len(accts)
	}

	queueSize := 0
	if s.queueMgr != nil {
		if stats, err := s.queueMgr.GetStats(); err == nil {
			queueSize = stats.Pending + stats.Sending + stats.Failed
		}
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"domains":    len(domains),
		"accounts":   accounts,
		"messages":   0, // Would need to scan maildirs
		"queue_size": queueSize,
	})
}
