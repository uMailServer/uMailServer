package api

import (
	"net/http"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, "GET")
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
		accts, aerr := s.db.ListAccountsByDomain(d.Name)
		if aerr != nil { // F6656: a failure undercounted silently
			s.sendError(w, http.StatusInternalServerError, "failed to get stats")
			return
		}
		accounts += len(accts)
	}

	queueSize := 0
	if s.queueMgr != nil {
		stats, err := s.queueMgr.GetStats()
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to get queue stats")
			return
		}
		queueSize = stats.Pending + stats.Sending + stats.Failed
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"domains":    len(domains),
		"accounts":   accounts,
		"messages":   0, // Would need to scan maildirs
		"queue_size": queueSize,
	})
}
