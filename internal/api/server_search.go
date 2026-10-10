package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/umailserver/umailserver/internal/search"
)

// Search handlers

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Get user from context
	user := r.Context().Value("user")
	if user == nil {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		s.sendError(w, http.StatusBadRequest, "missing query parameter 'q'")
		return
	}

	// Validate query length
	if utf8.RuneCountInString(query) > 500 {
		s.sendError(w, http.StatusBadRequest, "query too long (max 500 characters)")
		return
	}

	folder := r.URL.Query().Get("folder")
	// F5696: accept the webmail folder names (inbox, spam, ...) like the mail
	// endpoints do; the index keys documents by the internal mailbox name.
	searchFolder := folder
	if internal := folderMap[strings.ToLower(folder)]; internal != "" {
		searchFolder = internal
	}
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l > 100 {
				l = 100 // Cap at 100 to prevent resource exhaustion
			}
			limit = l
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Perform search
	if s.searchSvc == nil {
		s.sendError(w, http.StatusServiceUnavailable, "search service not available")
		return
	}

	userStr, ok := user.(string)
	if !ok {
		s.sendError(w, http.StatusUnauthorized, "invalid user context")
		return
	}

	results, err := s.searchSvc.Search(search.MessageSearchOptions{
		User:          userStr,
		Folder:        searchFolder,
		Query:         query,
		Limit:         limit,
		Offset:        offset,
		DateFrom:      r.URL.Query().Get("date_from"),
		DateTo:        r.URL.Query().Get("date_to"),
		HasAttachment: r.URL.Query().Get("has_attachment") == "true",
	})
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid date_") {
			s.sendError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.sendError(w, http.StatusInternalServerError, "search failed")
		return
	}

	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"query":  query,
		"folder": folder,
		"emails": results,
		"total":  len(results),
		"limit":  limit,
		"offset": offset,
	})
}
