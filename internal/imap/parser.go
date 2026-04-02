package imap

import (
	"fmt"
	"strconv"
	"strings"
)

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
