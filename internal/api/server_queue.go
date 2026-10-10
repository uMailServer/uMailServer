package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/db"
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
	// F5691: the detail routes are mounted under both /api/v1/queue/ and
	// /api/v1/admin/queue/; trimming only the first left the full path as id.
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/queue/"), "/api/v1/queue/")

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

// listQueue returns every undelivered entry (pending, sending, failed - the
// same set the stats endpoint counts as queue_size), oldest first. F6061: it
// used to return only pending entries due within 24h, so failed entries that
// need an operator were invisible. Optional ?status=, ?limit= (default 100,
// max 1000) and ?offset=.
func (s *Server) listQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := 100, 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			s.sendError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.sendError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = n
	}
	status := q.Get("status")
	switch status {
	case "", "pending", "sending", "failed":
	default:
		s.sendError(w, http.StatusBadRequest, "invalid status")
		return
	}

	var entries []*db.QueueEntry
	err := s.db.ForEach(db.BucketQueue, func(_ string, value []byte) error {
		var e db.QueueEntry
		if json.Unmarshal(value, &e) != nil {
			return nil
		}
		if e.Status == "delivered" || e.Status == "bounced" || (status != "" && e.Status != status) {
			return nil
		}
		entries = append(entries, &e)
		return nil
	})
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to list queue")
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
	if offset > len(entries) {
		offset = len(entries)
	}
	entries = entries[offset:]
	if len(entries) > limit {
		entries = entries[:limit]
	}

	result := make([]map[string]interface{}, 0, len(entries))
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

	// F6062: an in-flight entry would be delivered twice and a finished one
	// resurrected; only pending/failed entries can be retried.
	if entry.Status == "sending" || entry.Status == "delivered" || entry.Status == "bounced" {
		s.sendError(w, http.StatusConflict, "queue entry cannot be retried in status "+entry.Status)
		return
	}

	// Reset retry count and status
	entry.Status = "pending"
	entry.RetryCount = 0
	entry.LastError = ""
	entry.NextRetry = time.Now()

	if err := s.db.UpdateQueueEntry(entry); err != nil {
		if errors.Is(err, db.ErrQueueEntryNotFound) {
			s.sendError(w, http.StatusNotFound, "queue entry not found")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "failed to retry queue entry")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (s *Server) dropQueueEntry(w http.ResponseWriter, r *http.Request, id string) {
	// F5692: with a queue manager, drop through it so the queued message file
	// is removed too (a bare Dequeue orphaned it on disk).
	var err error
	if s.queueMgr != nil {
		err = s.queueMgr.DropEntry(id)
	} else {
		err = s.db.Dequeue(id)
	}
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "failed to drop queue entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
