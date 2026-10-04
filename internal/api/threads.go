package api

import (
	"fmt"
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
	threads, err := s.getThreadsForMailbox(user, mailbox, 0, 0)
	if err != nil {
		s.logger.Error("failed to get threads", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to get threads")
		return
	}

	total := len(threads)
	start := min(offset, total)
	end := total
	if limit < total-start {
		end = start + limit
	}
	threads = threads[start:end]

	// Convert to response format
	response := ThreadListResponse{
		Threads: make([]ThreadResponse, 0, len(threads)),
		Total:   total,
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
	if s.mailDB == nil {
		return nil, fmt.Errorf("mail storage unavailable")
	}
	// Threads are keyed per user in storage, so the mailbox filter does not
	// participate in this query.
	return s.mailDB.GetThreads(user, limit, offset)
}

// getThreadMessages retrieves messages for a specific thread
func (s *Server) getThreadMessages(user, mailbox, threadID string) ([]*storage.ThreadMessage, error) {
	if s.mailDB == nil {
		return nil, fmt.Errorf("mail storage unavailable")
	}
	return s.mailDB.GetThreadMessages(user, mailbox, threadID)
}

// searchThreads searches for threads matching a query
func (s *Server) searchThreads(user, query string) ([]*storage.Thread, error) {
	if s.mailDB == nil {
		return nil, fmt.Errorf("mail storage unavailable")
	}
	return s.mailDB.SearchThreads(user, query)
}

// markThreadAsRead marks all messages in a thread as read
func (s *Server) markThreadAsRead(user, mailbox, threadID string) error {
	if s.mailDB == nil {
		return fmt.Errorf("mail storage unavailable")
	}
	msgs, err := s.mailDB.GetThreadMessages(user, mailbox, threadID)
	if err != nil {
		return err
	}
	// Same read convention as mail.go: a message is read when the \Seen
	// flag is set. UpdateMessageMetadataFunc applies the flag atomically
	// per message (no read-modify-write race).
	for _, m := range msgs {
		err := s.mailDB.UpdateMessageMetadataFunc(user, mailbox, m.UID, func(meta *storage.MessageMetadata) error {
			if !storage.HasFlag(meta.Flags, "\\Seen") {
				meta.Flags = append(meta.Flags, "\\Seen")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	// Best-effort aggregate refresh: the Thread row exists only when the
	// IMAP delivery path has written one; a missing row is not an error.
	if thread, err := s.mailDB.GetThread(user, threadID); err == nil {
		count, unread, err := s.threadMessageCounts(user, threadID)
		if err != nil {
			return err
		}
		thread.MessageCount, thread.UnreadCount = count, unread
		if err := s.mailDB.UpdateThread(user, thread); err != nil {
			return err
		}
	}
	return nil
}

// deleteThread deletes all messages in a thread
func (s *Server) deleteThread(user, mailbox, threadID string) error {
	if s.mailDB == nil {
		return fmt.Errorf("mail storage unavailable")
	}
	msgs, err := s.mailDB.GetThreadMessages(user, mailbox, threadID)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if err := s.mailDB.DeleteMessage(user, mailbox, m.UID); err != nil {
			return err
		}
	}
	count, unread, err := s.threadMessageCounts(user, threadID)
	if err != nil {
		return err
	}
	if count == 0 {
		return s.mailDB.DeleteThread(user, threadID)
	}
	thread, err := s.mailDB.GetThread(user, threadID)
	if err != nil {
		return err
	}
	thread.MessageCount, thread.UnreadCount = count, unread
	return s.mailDB.UpdateThread(user, thread)
}

// threadMessageCounts counts the user-wide summary across mailboxes.
func (s *Server) threadMessageCounts(user, threadID string) (count, unread int, err error) {
	mailboxes, err := s.mailDB.ListMailboxes(user)
	if err != nil {
		return 0, 0, err
	}
	for _, mailbox := range mailboxes {
		messages, err := s.mailDB.GetThreadMessages(user, mailbox, threadID)
		if err != nil {
			return 0, 0, err
		}
		count += len(messages)
		for _, message := range messages {
			if !message.IsRead {
				unread++
			}
		}
	}
	return count, unread, nil
}
