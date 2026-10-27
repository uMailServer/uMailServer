package api

import (
	"net/http"
	"time"
)

// storedMessageCacheTTL bounds how often the dashboard's message count walks
// every mailbox.
const storedMessageCacheTTL = 30 * time.Second

// storedMessageCount returns the number of messages stored for emails across
// all their mailboxes, cached for storedMessageCacheTTL (the dashboard polls).
// It is 0 when no mail database is attached.
func (s *Server) storedMessageCount(emails []string) int {
	if s.mailDB == nil {
		return 0
	}
	s.msgCountMu.Lock()
	defer s.msgCountMu.Unlock()
	if !s.msgCountAt.IsZero() && time.Since(s.msgCountAt) < storedMessageCacheTTL {
		return s.msgCount
	}
	total := 0
	for _, email := range emails {
		boxes, err := s.mailDB.ListMailboxes(email)
		if err != nil {
			continue
		}
		for _, box := range boxes {
			if n, err := s.mailDB.CountMessages(email, box); err == nil {
				total += n
			}
		}
	}
	s.msgCount, s.msgCountAt = total, time.Now()
	return total
}

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
	var emails []string
	for _, d := range domains {
		accts, aerr := s.db.ListAccountsByDomain(d.Name)
		if aerr != nil { // F6656: a failure undercounted silently
			s.sendError(w, http.StatusInternalServerError, "failed to get stats")
			return
		}
		accounts += len(accts)
		for _, a := range accts {
			emails = append(emails, a.Email)
		}
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
		"messages":   s.storedMessageCount(emails), // F7787
		"queue_size": queueSize,
	})
}
