package imap

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser handles IMAP protocol parsing
type Parser struct {
	input string
	pos   int
}

// NewParser creates a new IMAP parser
func NewParser(input string) *Parser {
	return &Parser{
		input: input,
		pos:   0,
	}
}

// ParseSequenceSet parses an IMAP sequence set (e.g., "1:5,7,9:11,*")
func ParseSequenceSet(set string) ([]SeqRange, error) {
	var ranges []SeqRange
	parts := strings.Split(set, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, ":") {
			// Range like "1:5" or "5:*"
			rangeParts := strings.Split(part, ":")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range: %s", part)
			}

			start, err := parseSeqNumber(rangeParts[0])
			if err != nil {
				return nil, err
			}

			end, err := parseSeqNumber(rangeParts[1])
			if err != nil {
				return nil, err
			}

			ranges = append(ranges, SeqRange{Start: start, End: end})
		} else {
			// Single number or *
			num, err := parseSeqNumber(part)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, SeqRange{Start: num, End: num})
		}
	}

	return ranges, nil
}

// parseSeqNumber parses a sequence number (uint32 or *)
func parseSeqNumber(s string) (uint32, error) {
	if s == "*" {
		return 0, nil // 0 represents * (last message)
	}

	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid sequence number: %s", s)
	}

	return uint32(n), nil
}

// SeqRange represents a sequence range
type SeqRange struct {
	Start uint32 // 0 means *
	End   uint32 // 0 means *
}

// Contains checks if a sequence number is in the range
func (r SeqRange) Contains(seqNum uint32, maxSeq uint32) bool {
	start := r.Start
	end := r.End

	// Resolve *
	if start == 0 {
		start = maxSeq
	}
	if end == 0 {
		end = maxSeq
	}

	// Ensure start <= end
	if start > end {
		start, end = end, start
	}

	return seqNum >= start && seqNum <= end
}

// ParseFetchItems parses a fetch items list
func ParseFetchItems(items string) ([]string, error) {
	items = strings.TrimSpace(items)

	// Check for parenthesized list
	if strings.HasPrefix(items, "(") && strings.HasSuffix(items, ")") {
		items = items[1 : len(items)-1]
	}

	// Handle macro names
	switch strings.ToUpper(items) {
	case "ALL":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE"}, nil
	case "FAST":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE"}, nil
	case "FULL":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY"}, nil
	}

	// Parse individual items
	var result []string
	var current strings.Builder
	depth := 0

	for _, ch := range items {
		switch ch {
		case '(':
			depth++
			current.WriteRune(ch)
		case ')':
			depth--
			current.WriteRune(ch)
		case ' ':
			if depth == 0 {
				item := strings.TrimSpace(current.String())
				if item != "" {
					result = append(result, normalizeFetchItem(item))
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Add last item
	item := strings.TrimSpace(current.String())
	if item != "" {
		result = append(result, normalizeFetchItem(item))
	}

	return result, nil
}

// normalizeFetchItem normalizes a fetch item name
func normalizeFetchItem(item string) string {
	item = strings.ToUpper(strings.TrimSpace(item))

	// Handle BODY[...] format
	if strings.HasPrefix(item, "BODY[") {
		return item
	}
	if strings.HasPrefix(item, "BODY.PEEK[") {
		return item
	}
	if strings.HasPrefix(item, "RFC822") {
		return item
	}

	return item
}

// ParseSearchCriteria parses a SEARCH command criteria
func ParseSearchCriteria(args string) (*SearchCriteria, error) {
	criteria := &SearchCriteria{
		Header: make(map[string]string),
	}

	parser := NewParser(args)
	return parser.parseSearchCriteria(criteria)
}

// parseSearchCriteria recursively parses search criteria
func (p *Parser) parseSearchCriteria(criteria *SearchCriteria) (*SearchCriteria, error) {
	for p.pos < len(p.input) {
		// Skip whitespace
		for p.pos < len(p.input) && p.input[p.pos] == ' ' {
			p.pos++
		}

		if p.pos >= len(p.input) {
			break
		}

		// Read next token
		token := p.readToken()
		tokenUpper := strings.ToUpper(token)

		switch tokenUpper {
		case "ALL":
			criteria.All = true
		case "ANSWERED":
			criteria.Answered = true
		case "DELETED":
			criteria.Deleted = true
		case "FLAGGED":
			criteria.Flagged = true
		case "NEW":
			criteria.New = true
		case "OLD":
			criteria.Old = true
		case "RECENT":
			criteria.Recent = true
		case "SEEN":
			criteria.Seen = true
		case "UNANSWERED":
			criteria.Unanswered = true
		case "UNDELETED":
			criteria.Undeleted = true
		case "UNFLAGGED":
			criteria.Unflagged = true
		case "UNSEEN":
			criteria.Unseen = true
		case "DRAFT":
			criteria.Draft = true
		case "UNDRAFT":
			criteria.Undraft = true
		case "FROM":
			criteria.From = p.readToken()
		case "TO":
			criteria.To = p.readToken()
		case "CC":
			criteria.Cc = p.readToken()
		case "BCC":
			criteria.Bcc = p.readToken()
		case "SUBJECT":
			criteria.Subject = p.readToken()
		case "BODY":
			criteria.Body = p.readToken()
		case "TEXT":
			criteria.Text = p.readToken()
		case "UID":
			criteria.UIDSet = p.readToken()
		case "NOT":
			notCriteria := &SearchCriteria{Header: make(map[string]string)}
			criteria.Not = notCriteria
			p.parseSearchCriteria(notCriteria)
		case "OR":
			// OR requires two criteria
			criteria.Or[0] = &SearchCriteria{Header: make(map[string]string)}
			p.parseSearchCriteria(criteria.Or[0])
			criteria.Or[1] = &SearchCriteria{Header: make(map[string]string)}
			p.parseSearchCriteria(criteria.Or[1])
		case "LARGER":
			size, _ := strconv.ParseInt(p.readToken(), 10, 64)
			criteria.Larger = size
		case "SMALLER":
			size, _ := strconv.ParseInt(p.readToken(), 10, 64)
			criteria.Smaller = size
		case "HEADER":
			headerName := p.readToken()
			headerValue := p.readToken()
			criteria.Header[headerName] = headerValue
		default:
			// Could be a sequence set
			if criteria.SeqSet == "" {
				criteria.SeqSet = token
			}
		}
	}

	return criteria, nil
}

// readToken reads the next token from input
func (p *Parser) readToken() string {
	// Skip leading whitespace
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}

	if p.pos >= len(p.input) {
		return ""
	}

	// Check for quoted string
	if p.input[p.pos] == '"' {
		return p.readQuotedString()
	}

	// Read until whitespace or end
	start := p.pos
	for p.pos < len(p.input) && p.input[p.pos] != ' ' {
		p.pos++
	}

	return p.input[start:p.pos]
}

// readQuotedString reads a quoted string
func (p *Parser) readQuotedString() string {
	if p.input[p.pos] != '"' {
		return ""
	}

	p.pos++ // Skip opening quote
	start := p.pos

	for p.pos < len(p.input) && p.input[p.pos] != '"' {
		if p.input[p.pos] == '\\' && p.pos+1 < len(p.input) {
			p.pos += 2 // Skip escaped character
		} else {
			p.pos++
		}
	}

	result := p.input[start:p.pos]
	if p.pos < len(p.input) && p.input[p.pos] == '"' {
		p.pos++ // Skip closing quote
	}

	return result
}

// ParseStatusItems parses a STATUS command items list
func ParseStatusItems(items string) ([]StatusItem, error) {
	items = strings.TrimSpace(items)

	// Check for parenthesized list
	if strings.HasPrefix(items, "(") && strings.HasSuffix(items, ")") {
		items = items[1 : len(items)-1]
	}

	var result []StatusItem
	for _, item := range strings.Fields(items) {
		item = strings.ToUpper(strings.TrimSpace(item))
		if item == "" {
			continue
		}

		switch item {
		case "MESSAGES":
			result = append(result, StatusMessages)
		case "RECENT":
			result = append(result, StatusRecent)
		case "UIDNEXT":
			result = append(result, StatusUIDNext)
		case "UIDVALIDITY":
			result = append(result, StatusUIDValidity)
		case "UNSEEN":
			result = append(result, StatusUnseen)
		default:
			return nil, fmt.Errorf("unknown status item: %s", item)
		}
	}

	return result, nil
}

// ParseFlags parses a parenthesized list of flags
func ParseFlags(flags string) ([]string, error) {
	flags = strings.TrimSpace(flags)

	// Check for parenthesized list
	if !strings.HasPrefix(flags, "(") || !strings.HasSuffix(flags, ")") {
		return nil, fmt.Errorf("flags must be parenthesized: %s", flags)
	}

	flags = flags[1 : len(flags)-1]

	var result []string
	var current strings.Builder
	depth := 0

	for _, ch := range flags {
		switch ch {
		case '\\':
			current.WriteRune(ch)
		case ' ':
			if depth == 0 {
				item := strings.TrimSpace(current.String())
				if item != "" {
					result = append(result, item)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	// Add last item
	item := strings.TrimSpace(current.String())
	if item != "" {
		result = append(result, item)
	}

	return result, nil
}

// FormatFlags formats flags for IMAP response
func FormatFlags(flags []string) string {
	if len(flags) == 0 {
		return "()"
	}

	return "(" + strings.Join(flags, " ") + ")"
}
