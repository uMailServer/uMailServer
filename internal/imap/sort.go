package imap

import (
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// SortCriterion represents a sort criteria per RFC 5256
type SortCriterion struct {
	Field      string // ARRIVAL, DATE, FROM, SUBJECT, SIZE, UID
	Descending bool
}

// SortResult represents the result of a SORT command
type SortResult struct {
	SequenceNumbers []uint32
}

// parseSortCriteria parses SORT criteria from args
// Example: ["ARRIVAL", "REVERSE", "SUBJECT"]
func parseSortCriteria(args []string) ([]SortCriterion, error) {
	var criteria []SortCriterion
	// RFC 5256 §3: criteria sort ascending; REVERSE makes only the next
	// criterion descending. F5491: the default was descending, so SORT
	// (DATE) listed newest first and REVERSE DATE oldest first.
	descending := false

	for i := 0; i < len(args); i++ {
		arg := strings.ToUpper(args[i])
		switch arg {
		case "ARRIVAL":
			criteria = append(criteria, SortCriterion{Field: "ARRIVAL", Descending: descending})
			descending = false // reset after each criterion
		case "DATE":
			criteria = append(criteria, SortCriterion{Field: "DATE", Descending: descending})
			descending = false
		case "FROM":
			criteria = append(criteria, SortCriterion{Field: "FROM", Descending: descending})
			descending = false
		case "SUBJECT":
			criteria = append(criteria, SortCriterion{Field: "SUBJECT", Descending: descending})
			descending = false
		case "SIZE":
			criteria = append(criteria, SortCriterion{Field: "SIZE", Descending: descending})
			descending = false
		case "UID":
			criteria = append(criteria, SortCriterion{Field: "UID", Descending: descending})
			descending = false
		case "REVERSE":
			descending = true // applies to the next criterion
		case "SCORE":
			// NOTREVEALED - for threading, not supported in basic sort
			return nil, fmt.Errorf("unsupported sort criterion: SCORE")
		case "CC":
			criteria = append(criteria, SortCriterion{Field: "CC", Descending: descending})
			descending = false
		case "TO":
			criteria = append(criteria, SortCriterion{Field: "TO", Descending: descending})
			descending = false
		default:
			return nil, fmt.Errorf("unknown sort criterion: %s", arg)
		}
	}

	if len(criteria) == 0 {
		return nil, fmt.Errorf("no sort criteria provided")
	}

	return criteria, nil
}

// messageForSort is internal helper for sorting
type messageForSort struct {
	seqNum  uint32
	uid     uint32
	date    time.Time
	from    string
	subject string
	size    int64
	arrival time.Time
}

// sortMessagesByCriteria sorts messages according to RFC 5256
func sortMessagesByCriteria(messages []*storage.MessageMetadata, criteria []SortCriterion, seqNums []uint32) []uint32 {
	if len(messages) == 0 {
		return nil
	}

	// Build sortable list
	sortable := make([]messageForSort, len(messages))
	for i, msg := range messages {
		if t, err := parseMessageDate(msg.Date); err == nil {
			sortable[i] = messageForSort{
				seqNum:  seqNums[i],
				uid:     msg.UID,
				date:    t,
				from:    msg.From,
				subject: baseSubject(msg.Subject),
				size:    msg.Size,
				arrival: msg.InternalDate,
			}
		} else {
			sortable[i] = messageForSort{
				seqNum:  seqNums[i],
				uid:     msg.UID,
				from:    msg.From,
				subject: baseSubject(msg.Subject),
				size:    msg.Size,
				arrival: msg.InternalDate,
			}
		}
	}

	// Compare criteria in order, preserving mailbox order when all keys tie.
	sort.SliceStable(sortable, func(i, j int) bool {
		for _, c := range criteria {
			ascending := func(a, b int) bool {
				switch c.Field {
				case "ARRIVAL":
					return sortable[a].arrival.Before(sortable[b].arrival)
				case "DATE":
					return sortable[a].date.Before(sortable[b].date)
				case "FROM":
					return strings.ToLower(sortable[a].from) < strings.ToLower(sortable[b].from)
				case "SUBJECT":
					return sortable[a].subject < sortable[b].subject
				case "SIZE":
					return sortable[a].size < sortable[b].size
				case "UID":
					return sortable[a].uid < sortable[b].uid
				}
				return false
			}
			a, b := i, j
			if c.Descending {
				a, b = b, a
			}
			if ascending(a, b) {
				return true
			}
			if ascending(b, a) {
				return false
			}
		}
		return false
	})

	// Extract sequence numbers
	result := make([]uint32, len(sortable))
	for i, msg := range sortable {
		result[i] = msg.seqNum
	}

	return result
}

// baseSubject returns the RFC 5256 §2.1 base subject used by SORT SUBJECT:
// decoded, whitespace-collapsed and case-folded, with leading "Re:",
// "Fw:", "Fwd:" and "[blob]" prefixes, trailing "(fwd)" and the
// "[fwd: ... ]" wrapper removed. F5492: the raw subject was compared, so
// "Re: apple" sorted after "banana".
func baseSubject(subject string) string {
	if dec, err := new(mime.WordDecoder).DecodeHeader(subject); err == nil {
		subject = dec
	}
	s := strings.ToLower(strings.Join(strings.Fields(subject), " "))
	for {
		before := s
		// (2) trailing "(fwd)" and whitespace
		for strings.HasSuffix(s, "(fwd)") {
			s = strings.TrimSpace(strings.TrimSuffix(s, "(fwd)"))
		}
		// (3)+(4) leading subj-refwd and subj-blob prefixes
		for {
			prev := s
			s = trimSubjectPrefix(s)
			if s == prev {
				break
			}
		}
		// (6) "[fwd:" ... "]" wrapper
		if strings.HasPrefix(s, "[fwd:") && strings.HasSuffix(s, "]") {
			s = strings.TrimSpace(s[len("[fwd:") : len(s)-1])
		}
		if s == before {
			return s
		}
	}
}

// trimSubjectPrefix removes one leading "re:", "fw:", "fwd:" (optionally
// with a "[blob]" before the colon) or one "[blob]" that does not leave
// the subject empty.
func trimSubjectPrefix(s string) string {
	for _, p := range []string{"re", "fwd", "fw"} {
		if !strings.HasPrefix(s, p) {
			continue
		}
		rest := strings.TrimLeft(s[len(p):], " ")
		if strings.HasPrefix(rest, "[") {
			if end := strings.IndexByte(rest, ']'); end > 0 && !strings.ContainsAny(rest[1:end], "[]") {
				rest = strings.TrimLeft(rest[end+1:], " ")
			}
		}
		if strings.HasPrefix(rest, ":") {
			return strings.TrimSpace(rest[1:])
		}
	}
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 && !strings.ContainsAny(s[1:end], "[]") {
			if rest := strings.TrimSpace(s[end+1:]); rest != "" {
				return rest
			}
		}
	}
	return s
}

// ThreadAlgorithm represents the threading algorithm per RFC 5256
type ThreadAlgorithm string

const (
	ThreadReferences     ThreadAlgorithm = "REFERENCES"
	ThreadOrderedSubject ThreadAlgorithm = "ORDEREDSUBJECT"
)

// threadMessagesByReferences threads messages using REFERENCES algorithm
// Messages are linked by Message-ID headers per RFC 5256 Section 5
func threadMessagesByReferences(messages []*storage.MessageMetadata, seqNums []uint32) map[uint32][]uint32 {
	// Build a map of Message-ID -> sequence number
	idToSeq := make(map[string]uint32)
	children := make(map[uint32][]uint32)

	// Create seqNum to index mapping
	seqToIdx := make(map[uint32]int)
	for i, seq := range seqNums {
		seqToIdx[seq] = i
	}

	for i, msg := range messages {
		seq := seqNums[i]
		if msg.MessageID != "" {
			idToSeq[msg.MessageID] = seq
		}
	}

	// For each message, find its parent (In-Reply-To or References)
	for i, msg := range messages {
		seq := seqNums[i]
		added := false

		// Check In-Reply-To
		if msg.InReplyTo != "" {
			if parentSeq, ok := idToSeq[msg.InReplyTo]; ok {
				children[parentSeq] = append(children[parentSeq], seq)
				added = true
			}
		}

		// Check References header (may contain multiple IDs, use first that exists)
		if !added && len(msg.References) > 0 {
			for _, ref := range msg.References {
				if parentSeq, ok := idToSeq[ref]; ok {
					children[parentSeq] = append(children[parentSeq], seq)
					break
				}
			}
		}
	}

	return children
}

// threadMessagesByOrderedSubject threads messages by ORDEREDSUBJECT algorithm
// Messages with same subject are grouped together, ordered by date
func threadMessagesByOrderedSubject(messages []*storage.MessageMetadata, seqNums []uint32) map[uint32][]uint32 {
	// Group by normalized subject
	type msgInfo struct {
		seqNum uint32
		date   time.Time
	}
	subjectGroups := make(map[string][]msgInfo)

	for i, msg := range messages {
		normalizedSubject := strings.ToLower(strings.TrimSpace(msg.Subject))
		if normalizedSubject == "" {
			normalizedSubject = "(no subject)"
		}
		t, _ := parseMessageDate(msg.Date)
		subjectGroups[normalizedSubject] = append(subjectGroups[normalizedSubject], msgInfo{
			seqNum: seqNums[i],
			date:   t,
		})
	}

	// Sort each group by date
	for subject := range subjectGroups {
		sort.Slice(subjectGroups[subject], func(i, j int) bool {
			return subjectGroups[subject][i].date.Before(subjectGroups[subject][j].date)
		})
	}

	// Build thread tree - first message in each group is root
	children := make(map[uint32][]uint32)

	for _, group := range subjectGroups {
		if len(group) > 0 {
			root := group[0].seqNum
			for i := 1; i < len(group); i++ {
				children[root] = append(children[root], group[i].seqNum)
			}
		}
	}

	return children
}

// flattenThread returns all sequence numbers in a thread starting from root
func flattenThread(root uint32, children map[uint32][]uint32, visited map[uint32]bool) []uint32 {
	var result []uint32
	queue := []uint32{root}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if visited[curr] {
			continue
		}
		visited[curr] = true
		result = append(result, curr)
		queue = append(queue, children[curr]...)
	}

	return result
}

// ThreadResult represents the result of a THREAD command
type ThreadResult struct {
	Threads [][]uint32
}
