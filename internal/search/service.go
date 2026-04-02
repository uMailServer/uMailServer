package search

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/umailserver/umailserver/internal/storage"
)

// Service provides message search functionality
type Service struct {
	index    *Index
	logger   *slog.Logger
	db       *storage.Database
	msgStore *storage.MessageStore
	mu       sync.RWMutex
	indexes  map[string]*Index // user -> index
}

// NewService creates a new search service
func NewService(database *storage.Database, msgStore *storage.MessageStore, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		index:    NewIndex(),
		logger:   logger,
		db:       database,
		msgStore: msgStore,
		indexes:  make(map[string]*Index),
	}
}

// MessageSearchResult represents a message search result
type MessageSearchResult struct {
	UID           uint32  `json:"uid"`
	Folder        string  `json:"folder"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Subject       string  `json:"subject"`
	Preview       string  `json:"preview"`
	Date          string  `json:"date"`
	Score         float64 `json:"score"`
	HasAttachment bool    `json:"has_attachment"`
}

// MessageSearchOptions contains search options
type MessageSearchOptions struct {
	User          string
	Folder        string // empty for all folders
	Query         string
	Limit         int
	Offset        int
	DateFrom      string
	DateTo        string
	HasAttachment bool
}

// Search performs a search across user's messages
func (s *Service) Search(opts MessageSearchOptions) ([]MessageSearchResult, error) {
	s.mu.RLock()
	index, exists := s.indexes[opts.User]
	s.mu.RUnlock()

	if !exists {
		// Index doesn't exist yet, build it
		if err := s.BuildIndex(opts.User); err != nil {
			return nil, fmt.Errorf("failed to build index: %w", err)
		}
		s.mu.RLock()
		index = s.indexes[opts.User]
		s.mu.RUnlock()
	}

	// Perform search
	searchOpts := SearchOptions{}
	if opts.Limit > 0 {
		searchOpts.Limit = opts.Limit
	} else {
		searchOpts.Limit = 20
	}
	searchOpts.Offset = opts.Offset

	results := index.Search(opts.Query, searchOpts)

	// Convert to MessageSearchResult
	var searchResults []MessageSearchResult
	for _, result := range results {
		// Parse docID to get folder and UID
		// DocID format: folder:uid
		folder, uid, err := parseDocID(result.DocID)
		if err != nil {
			continue
		}

		// Filter by folder if specified
		if opts.Folder != "" && folder != opts.Folder {
			continue
		}

		// Get message metadata (optional — use index data if db unavailable)
		searchResult := MessageSearchResult{
			UID:    uid,
			Folder: folder,
			Score:  result.Score,
		}

		if s.db != nil {
			meta, err := s.db.GetMessageMetadata(opts.User, folder, uid)
			if err == nil && meta != nil {
				searchResult.From = meta.From
				searchResult.To = meta.To
				searchResult.Subject = meta.Subject
				searchResult.Preview = generatePreview(meta.Subject, 100)
				searchResult.Date = meta.Date
			}
		}

		searchResults = append(searchResults, searchResult)
	}

	return searchResults, nil
}

// BuildIndex builds the search index for a user
func (s *Service) BuildIndex(user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("Building search index", "user", user)

	index := NewIndex()

	// Get all folders for user
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	folders, err := s.db.ListMailboxes(user)
	if err != nil {
		return fmt.Errorf("failed to list folders: %w", err)
	}

	// Index messages in each folder
	for _, folder := range folders {
		uids, err := s.db.GetMessageUIDs(user, folder)
		if err != nil {
			continue
		}

		for _, uid := range uids {
			meta, err := s.db.GetMessageMetadata(user, folder, uid)
			if err != nil {
				continue
			}

			// Read message content for full-text indexing
			content := ""
			if s.msgStore != nil {
				data, err := s.msgStore.ReadMessage(user, meta.MessageID)
				if err == nil {
					// Extract text content
					content = extractTextContent(data)
				}
			}

			// Create document
			doc := &Document{
				ID:      fmt.Sprintf("%s:%d", folder, uid),
				Content: content,
				Fields: map[string]string{
					"from":    meta.From,
					"to":      meta.To,
					"subject": meta.Subject,
				},
			}

			index.Add(doc)
		}
	}

	s.indexes[user] = index
	s.logger.Info("Search index built", "user", user, "docs", index.DocCount())

	return nil
}

// IndexMessage adds a message to the search index.
// TODO: Wire into message delivery flow (server.go deliverMessage) so new
// messages are indexed automatically on receipt.
func (s *Service) IndexMessage(user, folder string, uid uint32) error {
	s.mu.RLock()
	index, exists := s.indexes[user]
	s.mu.RUnlock()

	if !exists {
		// Build index if it doesn't exist
		return s.BuildIndex(user)
	}

	meta, err := s.db.GetMessageMetadata(user, folder, uid)
	if err != nil {
		return err
	}

	// Read message content
	content := ""
	if s.msgStore != nil {
		data, err := s.msgStore.ReadMessage(user, meta.MessageID)
		if err == nil {
			content = extractTextContent(data)
		}
	}

	doc := &Document{
		ID:      fmt.Sprintf("%s:%d", folder, uid),
		Content: content,
		Fields: map[string]string{
			"from":    meta.From,
			"to":      meta.To,
			"subject": meta.Subject,
		},
	}

	index.Add(doc)
	return nil
}

// RemoveMessage removes a message from the search index.
// TODO: Wire into IMAP EXPUNGE and admin message deletion handlers.
func (s *Service) RemoveMessage(user, folder string, uid uint32) {
	s.mu.RLock()
	index, exists := s.indexes[user]
	s.mu.RUnlock()

	if !exists {
		return
	}

	docID := fmt.Sprintf("%s:%d", folder, uid)
	index.Remove(docID)
}

// ClearIndex clears the search index for a user
func (s *Service) ClearIndex(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index, exists := s.indexes[user]; exists {
		index.Clear()
		delete(s.indexes, user)
	}
}

// parseDocID parses a document ID into folder and UID
func parseDocID(docID string) (string, uint32, error) {
	// Parse format: folder:uid
	parts := strings.SplitN(docID, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid docID format")
	}

	uid, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", 0, err
	}

	return parts[0], uint32(uid), nil
}

// generatePreview generates a preview text from content
func generatePreview(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// extractTextContent extracts text content from message data
func extractTextContent(data []byte) string {
	// Simple extraction - remove headers and extract body
	content := string(data)

	// Find body start
	bodyStart := strings.Index(content, "\r\n\r\n")
	if bodyStart == -1 {
		bodyStart = strings.Index(content, "\n\n")
	}
	if bodyStart != -1 {
		content = content[bodyStart:]
	}

	// Remove HTML tags if present
	content = stripHTML(content)

	// Normalize whitespace
	content = strings.Join(strings.Fields(content), " ")

	return content
}

// stripHTML removes HTML tags from text
func stripHTML(html string) string {
	// Simple HTML tag removal
	result := ""
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result += string(r)
		}
	}
	return result
}
