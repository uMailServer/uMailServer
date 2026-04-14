package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// ThreadResponse represents a thread in API responses
type ThreadResponse struct {
	ThreadID     string    `json:"thread_id"`
	Subject      string    `json:"subject"`
	Participants []string  `json:"participants"`
	MessageCount int       `json:"message_count"`
	UnreadCount  int       `json:"unread_count"`
	LastActivity time.Time `json:"last_activity"`
	CreatedAt    time.Time `json:"created_at"`
}

// ThreadMessageResponse represents a message in a thread
type ThreadMessageResponse struct {
	MessageID string    `json:"message_id"`
	UID       uint32    `json:"uid"`
	Mailbox   string    `json:"mailbox"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Date      time.Time `json:"date"`
	IsRead    bool      `json:"is_read"`
	Flags     []string  `json:"flags"`
}

// ThreadListResponse represents the response for listing threads
type ThreadListResponse struct {
	Threads []ThreadResponse `json:"threads"`
	Total   int              `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
}

// handleThreads handles GET /api/v1/threads
func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	limit := 20
	offset := 0
	mailbox := "INBOX"

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	if m := r.URL.Query().Get("mailbox"); m != "" {
		mailbox = m
	}

	// Get threads from database
	threads, err := s.getThreadsForMailbox(user, mailbox, limit, offset)
	if err != nil {
		s.logger.Error("failed to get threads", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to get threads")
		return
	}

	// Convert to response format
	response := ThreadListResponse{
		Threads: make([]ThreadResponse, 0, len(threads)),
		Total:   len(threads),
		Limit:   limit,
		Offset:  offset,
	}

	for _, t := range threads {
		response.Threads = append(response.Threads, ThreadResponse{
			ThreadID:     t.ThreadID,
			Subject:      t.Subject,
			Participants: t.Participants,
			MessageCount: t.MessageCount,
			UnreadCount:  t.UnreadCount,
			LastActivity: t.LastActivity,
			CreatedAt:    t.CreatedAt,
		})
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handleThreadDetail handles GET /api/v1/threads/{id}
func (s *Server) handleThreadDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract thread ID from URL
	threadID := r.URL.Path[len("/api/v1/threads/"):]
	if threadID == "" {
		s.sendError(w, http.StatusBadRequest, "thread ID required")
		return
	}

	// Get mailbox from query (default to INBOX)
	mailbox := r.URL.Query().Get("mailbox")
	if mailbox == "" {
		mailbox = "INBOX"
	}

	// Get thread messages
	messages, err := s.getThreadMessages(user, mailbox, threadID)
	if err != nil {
		s.logger.Error("failed to get thread messages", "error", err, "user", user, "thread", threadID)
		s.sendError(w, http.StatusInternalServerError, "failed to get thread messages")
		return
	}

	if len(messages) == 0 {
		s.sendError(w, http.StatusNotFound, "thread not found")
		return
	}

	// Convert to response format
	response := make([]ThreadMessageResponse, 0, len(messages))
	for _, m := range messages {
		response = append(response, ThreadMessageResponse{
			MessageID: m.MessageID,
			UID:       m.UID,
			Mailbox:   m.Mailbox,
			From:      m.From,
			To:        m.To,
			Subject:   m.Subject,
			Date:      m.Date,
			IsRead:    m.IsRead,
			Flags:     m.Flags,
		})
	}

	s.sendJSON(w, http.StatusOK, response)
}

// handleThreadSearch handles GET /api/v1/threads/search
func (s *Server) handleThreadSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get search query
	query := r.URL.Query().Get("q")
	if query == "" {
		s.sendError(w, http.StatusBadRequest, "query parameter 'q' required")
		return
	}

	// Search threads
	threads, err := s.searchThreads(user, query)
	if err != nil {
		s.logger.Error("failed to search threads", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to search threads")
		return
	}

	// Convert to response format
	response := make([]ThreadResponse, 0, len(threads))
	for _, t := range threads {
		response = append(response, ThreadResponse{
			ThreadID:     t.ThreadID,
			Subject:      t.Subject,
			Participants: t.Participants,
			MessageCount: t.MessageCount,
			UnreadCount:  t.UnreadCount,
			LastActivity: t.LastActivity,
			CreatedAt:    t.CreatedAt,
		})
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"threads": response,
		"query":   query,
	})
}

// handleThreadMarkRead handles POST /api/v1/threads/{id}/read
func (s *Server) handleThreadMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract thread ID from URL
	// Path format: /api/v1/threads/{id}/read
	path := r.URL.Path
	path = path[len("/api/v1/threads/"):]
	path = path[:len(path)-len("/read")] // Remove "/read" suffix

	if path == "" {
		s.sendError(w, http.StatusBadRequest, "thread ID required")
		return
	}

	// Get mailbox from query (default to INBOX)
	mailbox := r.URL.Query().Get("mailbox")
	if mailbox == "" {
		mailbox = "INBOX"
	}

	// Mark all messages in thread as read
	err := s.markThreadAsRead(user, mailbox, path)
	if err != nil {
		s.logger.Error("failed to mark thread as read", "error", err, "user", user, "thread", path)
		s.sendError(w, http.StatusInternalServerError, "failed to mark thread as read")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "success",
	})
}

// handleThreadPath routes to the appropriate thread handler based on path
func (s *Server) handleThreadPath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/v1/threads/"):]

	// Check for sub-paths
	if strings.HasSuffix(path, "/read") {
		s.handleThreadMarkRead(w, r)
		return
	}

	// Default to detail handler for GET/DELETE
	switch r.Method {
	case http.MethodGet:
		s.handleThreadDetail(w, r)
	case http.MethodDelete:
		s.handleThreadDelete(w, r)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleThreadDelete handles DELETE /api/v1/threads/{id}
func (s *Server) handleThreadDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Extract thread ID from URL
	threadID := r.URL.Path[len("/api/v1/threads/"):]
	if threadID == "" {
		s.sendError(w, http.StatusBadRequest, "thread ID required")
		return
	}

	// Get mailbox from query (default to INBOX)
	mailbox := r.URL.Query().Get("mailbox")
	if mailbox == "" {
		mailbox = "INBOX"
	}

	// Delete all messages in thread
	err := s.deleteThread(user, mailbox, threadID)
	if err != nil {
		s.logger.Error("failed to delete thread", "error", err, "user", user, "thread", threadID)
		s.sendError(w, http.StatusInternalServerError, "failed to delete thread")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
	})
}

// getThreadsForMailbox retrieves threads for a user's mailbox
func (s *Server) getThreadsForMailbox(user, mailbox string, limit, offset int) ([]*storage.Thread, error) {
	// This is a placeholder - in a real implementation, we would query the database
	// For now, return an empty list
	return []*storage.Thread{}, nil
}

// getThreadMessages retrieves messages for a specific thread
func (s *Server) getThreadMessages(user, mailbox, threadID string) ([]*storage.ThreadMessage, error) {
	// This is a placeholder - in a real implementation, we would query the database
	// For now, return an empty list
	return []*storage.ThreadMessage{}, nil
}

// searchThreads searches for threads matching a query
func (s *Server) searchThreads(user, query string) ([]*storage.Thread, error) {
	// This is a placeholder - in a real implementation, we would search the database
	// For now, return an empty list
	return []*storage.Thread{}, nil
}

// markThreadAsRead marks all messages in a thread as read
func (s *Server) markThreadAsRead(user, mailbox, threadID string) error {
	// This is a placeholder - in a real implementation, we would update the database
	return nil
}

// deleteThread deletes all messages in a thread
func (s *Server) deleteThread(user, mailbox, threadID string) error {
	// This is a placeholder - in a real implementation, we would delete from the database
	return nil
}
