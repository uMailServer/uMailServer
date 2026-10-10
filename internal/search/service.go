package search

import (
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/storage"
)

// maxSearchLimit bounds the page size of a single search.
const maxSearchLimit = 1000

var attachmentDispositionRE = regexp.MustCompile(`(?i)content-disposition:\s*attachment`)

// parseSearchDate parses YYYY-MM-DD or RFC3339. A date-only upper bound is
// inclusive of the whole day.
func parseSearchDate(v string, upper bool) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", v); err == nil {
		if upper {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		return t, nil
	}
	return time.Parse(time.RFC3339, v)
}

// docDate picks the message date used for date filtering.
func docDate(meta *storage.MessageMetadata) time.Time {
	if !meta.InternalDate.IsZero() {
		return meta.InternalDate
	}
	if t, err := mail.ParseDate(meta.Date); err == nil {
		return t
	}
	return time.Time{}
}

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
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	// F5975: cap so a caller cannot force an unbounded result slice.
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	var from, to time.Time
	var err error
	if opts.DateFrom != "" {
		if from, err = parseSearchDate(opts.DateFrom, false); err != nil {
			return nil, fmt.Errorf("invalid date_from: %w", err)
		}
	}
	if opts.DateTo != "" {
		if to, err = parseSearchDate(opts.DateTo, true); err != nil {
			return nil, fmt.Errorf("invalid date_to: %w", err)
		}
	}

	filtered := opts.Folder != "" || !from.IsZero() || !to.IsZero() || opts.HasAttachment
	indexOpts := SearchOptions{Limit: limit, Offset: offset}
	if filtered {
		// Filtering must precede pagination.
		indexOpts = SearchOptions{}
	}
	results := index.Search(opts.Query, indexOpts)
	if filtered {
		kept := results[:0]
		for _, result := range results {
			if opts.Folder != "" {
				folder, _, err := parseDocID(result.DocID)
				if err != nil || folder != opts.Folder {
					continue
				}
			}
			if !from.IsZero() || !to.IsZero() || opts.HasAttachment {
				doc := index.Get(result.DocID)
				if doc == nil {
					continue
				}
				if opts.HasAttachment && !doc.HasAttachment {
					continue
				}
				if !from.IsZero() && (doc.Date.IsZero() || doc.Date.Before(from)) {
					continue
				}
				if !to.IsZero() && (doc.Date.IsZero() || doc.Date.After(to)) {
					continue
				}
			}
			kept = append(kept, result)
		}
		start := offset
		if start > len(kept) {
			start = len(kept)
		}
		end := len(kept)
		if limit < end-start {
			end = start + limit
		}
		results = kept[start:end]
	}

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

		if doc := index.Get(result.DocID); doc != nil {
			searchResult.HasAttachment = doc.HasAttachment
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
			hasAtt := false
			if s.msgStore != nil {
				data, err := s.msgStore.ReadMessage(user, meta.MessageID)
				if err == nil {
					// Extract text content
					content = extractTextContent(data)
					hasAtt = attachmentDispositionRE.Match(data)
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
				Date:          docDate(meta),
				HasAttachment: hasAtt,
			}

			index.Add(doc)
		}
	}

	s.indexes[user] = index
	s.logger.Info("Search index built", "user", user, "docs", index.DocCount())

	return nil
}

// IndexMessage adds a message to the search index.
// IndexMessage indexes a message for full-text search.
// Called from server.go deliverLocal after storing a new message.
func (s *Service) IndexMessage(user, folder string, uid uint32) error {
	s.mu.RLock()
	index, exists := s.indexes[user]
	s.mu.RUnlock()

	if !exists {
		// Build index if it doesn't exist
		return s.BuildIndex(user)
	}

	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	meta, err := s.db.GetMessageMetadata(user, folder, uid)
	if err != nil {
		return err
	}

	// Read message content
	content := ""
	hasAtt := false
	if s.msgStore != nil {
		data, err := s.msgStore.ReadMessage(user, meta.MessageID)
		if err == nil {
			content = extractTextContent(data)
			hasAtt = attachmentDispositionRE.Match(data)
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
		Date:          docDate(meta),
		HasAttachment: hasAtt,
	}

	index.Add(doc)
	return nil
}

// RemoveMessage removes a message from the search index.
// RemoveMessage removes a message from the search index.
// Called from IMAP EXPUNGE handler (imap commands.go) and admin message deletion.
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
	separator := strings.LastIndexByte(docID, ':')
	if separator < 0 {
		return "", 0, fmt.Errorf("invalid docID format")
	}

	uid, err := strconv.ParseUint(docID[separator+1:], 10, 32)
	if err != nil {
		return "", 0, err
	}

	return docID[:separator], uint32(uid), nil
}

// generatePreview generates a preview text from content
func generatePreview(content string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(content) <= maxLen {
		return content
	}
	// Cut on a rune boundary so a multi-byte character is never split into
	// invalid UTF-8 (F5197).
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut] + "..."
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
	// Simple HTML tag removal. A strings.Builder keeps this linear: the former
	// per-rune string concatenation was quadratic, so one large inbound
	// message stalled an index worker (and BuildIndex's service lock) (F5196).
	var result strings.Builder
	result.Grow(len(html))
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}
