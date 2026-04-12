package api

import (
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listQueue(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleQueueDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/queue/")

	switch r.Method {
	case http.MethodGet:
		s.getQueueEntry(w, r, id)
	case http.MethodPost:
		s.retryQueueEntry(w, r, id)
	case http.MethodDelete:
		s.dropQueueEntry(w, r, id)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Queue handlers

func (s *Server) listQueue(w http.ResponseWriter, r *http.Request) {
	// Get pending queue entries from database
	entries, err := s.db.GetPendingQueue(time.Now().Add(24 * time.Hour))
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list queue")
		return
	}

	var result []map[string]interface{}
	for _, e := range entries {
		result = append(result, map[string]interface{}{
			"id":          e.ID,
			"from":        e.From,
			"to":          e.To,
			"status":      e.Status,
			"retry_count": e.RetryCount,
			"last_error":  e.LastError,
			"created_at":  e.CreatedAt,
			"next_retry":  e.NextRetry,
		})
	}

	s.sendJSON(w, http.StatusOK, result)
}

func (s *Server) getQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := s.db.GetQueueEntry(id)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "queue entry not found")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"id":          entry.ID,
		"from":        entry.From,
		"to":          entry.To,
		"status":      entry.Status,
		"retry_count": entry.RetryCount,
		"last_error":  entry.LastError,
		"created_at":  entry.CreatedAt,
		"next_retry":  entry.NextRetry,
	})
}

func (s *Server) retryQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	entry, err := s.db.GetQueueEntry(id)
	if err != nil {
		s.sendError(w, http.StatusNotFound, "queue entry not found")
		return
	}

	// Reset retry count and status
	entry.Status = "pending"
	entry.RetryCount = 0
	entry.LastError = ""
	entry.NextRetry = time.Now()

	if err := s.db.UpdateQueueEntry(entry); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to retry queue entry")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (s *Server) dropQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.db.Dequeue(id); err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to drop queue entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
