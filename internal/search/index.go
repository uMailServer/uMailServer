package search

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// Document represents a searchable document
type Document struct {
	ID      string
	Content string
	Fields  map[string]string // e.g., "from", "to", "subject"
}

// Index provides full-text search capabilities
type Index struct {
	mu       sync.RWMutex
	docs     map[string]*Document
	tokens   map[string]map[string]int // token -> docID -> frequency
	docCount int
}

// NewIndex creates a new search index
func NewIndex() *Index {
	return &Index{
		docs:   make(map[string]*Document),
		tokens: make(map[string]map[string]int),
	}
}

// Add adds a document to the index
func (idx *Index) Add(doc *Document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old version if exists
	if _, exists := idx.docs[doc.ID]; exists {
		idx.removeInternal(doc.ID)
	}

	// Store document
	idx.docs[doc.ID] = doc
	idx.docCount++

	// Index all text content
	allText := doc.Content
	for field, value := range doc.Fields {
		// Add field-specific tokens with field prefix
		tokens := tokenize(value)
		for _, token := range tokens {
			fieldToken := field + ":" + token
			idx.addToken(fieldToken, doc.ID)
		}
		allText += " " + value
	}

	// Index general content tokens
	tokens := tokenize(allText)
	for _, token := range tokens {
		idx.addToken(token, doc.ID)
	}
}

// addToken adds a token to the index
func (idx *Index) addToken(token, docID string) {
	if _, exists := idx.tokens[token]; !exists {
		idx.tokens[token] = make(map[string]int)
	}
	idx.tokens[token][docID]++
}

// Remove removes a document from the index
func (idx *Index) Remove(docID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeInternal(docID)
}

func (idx *Index) removeInternal(docID string) {
	doc, exists := idx.docs[docID]
	if !exists {
		return
	}

	// Remove all tokens for this document
	allText := doc.Content
	for field, value := range doc.Fields {
		tokens := tokenize(value)
		for _, token := range tokens {
			// Remove field-prefixed token (as added in Add)
			fieldToken := field + ":" + token
			delete(idx.tokens[fieldToken], docID)
		}
		allText += " " + value
	}

	tokens := tokenize(allText)
	for _, token := range tokens {
		delete(idx.tokens[token], docID)
	}

	delete(idx.docs, docID)
	idx.docCount--
}

// SearchResult represents a search result
type SearchResult struct {
	DocID      string
	Score      float64
	Highlights []string
}

// SearchOptions contains search options
type SearchOptions struct {
	Limit  int
	Offset int
}

// Search performs a full-text search
func (idx *Index) Search(query string, opts SearchOptions) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Parse query
	queryTerms := parseQuery(query)

	// Score documents
	docScores := make(map[string]float64)

	for _, term := range queryTerms {
		if term.Field != "" {
			// Field-specific search
			fieldToken := term.Field + ":" + term.Value
			for docID, freq := range idx.tokens[fieldToken] {
				score := idx.calculateScore(fieldToken, docID, freq)
				docScores[docID] += score * term.Boost
			}
		} else {
			// General search across all fields
			for docID, freq := range idx.tokens[term.Value] {
				score := idx.calculateScore(term.Value, docID, freq)
				docScores[docID] += score * term.Boost
			}
		}
	}

	// Convert to results
	var results []SearchResult
	for docID, score := range docScores {
		results = append(results, SearchResult{
			DocID: docID,
			Score: score,
		})
	}

	// Sort by score (highest first)
	sortResults(results)

	// Apply limit and offset
	start := opts.Offset
	if start > len(results) {
		return []SearchResult{}
	}

	end := start + opts.Limit
	if end > len(results) || opts.Limit == 0 {
		end = len(results)
	}

	return results[start:end]
}

// calculateScore calculates TF-IDF score
func (idx *Index) calculateScore(token, docID string, freq int) float64 {
	// Term Frequency (TF)
	tf := float64(freq)

	// Inverse Document Frequency (IDF)
	docsWithToken := len(idx.tokens[token])
	idf := 1.0
	if docsWithToken > 0 {
		idf = float64(idx.docCount) / float64(docsWithToken)
	}

	return tf * idf
}

// QueryTerm represents a parsed query term
type QueryTerm struct {
	Field string
	Value string
	Boost float64
}

// parseQuery parses a search query into terms
// Supports syntax like: from:john subject:hello world has:attachment
func parseQuery(query string) []QueryTerm {
	var terms []QueryTerm

	// Pattern for field:value pairs
	fieldPattern := regexp.MustCompile(`(\w+):(\S+)`)

	// Find all field:value pairs
	matches := fieldPattern.FindAllStringSubmatch(query, -1)
	for _, match := range matches {
		if len(match) == 3 {
			field := strings.ToLower(match[1])
			value := strings.ToLower(match[2])
			terms = append(terms, QueryTerm{
				Field: field,
				Value: value,
				Boost: 2.0, // Field-specific matches get higher boost
			})
		}
	}

	// Remove field:value pairs from query to get remaining terms
	remaining := fieldPattern.ReplaceAllString(query, "")
	remaining = strings.TrimSpace(remaining)

	// Tokenize remaining text
	if remaining != "" {
		tokens := tokenize(remaining)
		for _, token := range tokens {
			terms = append(terms, QueryTerm{
				Field: "",
				Value: token,
				Boost: 1.0,
			})
		}
	}

	return terms
}

// tokenize breaks text into tokens
func tokenize(text string) []string {
	// Convert to lowercase
	text = strings.ToLower(text)

	// Split by non-letter/number characters
	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				token := current.String()
				if !isStopWord(token) {
					tokens = append(tokens, token)
				}
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		token := current.String()
		if !isStopWord(token) {
			tokens = append(tokens, token)
		}
	}

	return tokens
}

// isStopWord checks if a word is a common stop word
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "need": true,
		"dare": true, "ought": true, "used": true, "to": true, "of": true,
		"in": true, "for": true, "on": true, "with": true, "at": true,
		"from": true, "by": true, "about": true, "as": true, "into": true,
		"through": true, "during": true, "before": true, "after": true,
		"above": true, "below": true, "between": true, "under": true,
		"and": true, "but": true, "or": true, "yet": true, "so": true,
		"if": true, "because": true, "although": true, "though": true,
		"while": true, "where": true, "when": true, "that": true,
		"which": true, "who": true, "whom": true, "whose": true,
		"what": true, "this": true, "these": true, "those": true,
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "me": true, "him": true, "her": true,
		"us": true, "them": true, "my": true, "your": true, "his": true,
		"its": true, "our": true, "their": true,
	}
	return stopWords[word]
}

// sortResults sorts results by score (descending)
func sortResults(results []SearchResult) {
	// Simple bubble sort for now (sufficient for small result sets)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// Clear removes all documents from the index
func (idx *Index) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.docs = make(map[string]*Document)
	idx.tokens = make(map[string]map[string]int)
	idx.docCount = 0
}

// DocCount returns the number of indexed documents
func (idx *Index) DocCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}
