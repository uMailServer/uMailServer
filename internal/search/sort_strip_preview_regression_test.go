package search

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSearchEqualScorePaginationIsStable is the regression test for F5195:
// equal-score hits came back in map-iteration order, so offset pages
// duplicated some messages and skipped others.
func TestSearchEqualScorePaginationIsStable(t *testing.T) {
	idx := NewIndex()
	const n = 60
	for i := 1; i <= n; i++ {
		idx.Add(&Document{ID: fmt.Sprintf("INBOX:%d", i), Content: "invoice"})
	}
	for attempt := 0; attempt < 10; attempt++ {
		seen := make(map[string]bool, n)
		for off := 0; off < n; off += 10 {
			for _, r := range idx.Search("invoice", SearchOptions{Limit: 10, Offset: off}) {
				if seen[r.DocID] {
					t.Fatalf("attempt %d: %s returned on two pages", attempt, r.DocID)
				}
				seen[r.DocID] = true
			}
		}
		if len(seen) != n {
			t.Fatalf("attempt %d: paged %d of %d docs", attempt, len(seen), n)
		}
	}
}

// TestStripHTMLAllocatesLinearly is the regression test for F5196: per-rune
// string concatenation made stripHTML quadratic, so a single large inbound
// message stalled an index worker for seconds to hours.
func TestStripHTMLAllocatesLinearly(t *testing.T) {
	in := strings.Repeat("<b>abcdefg</b> ", 256*1024/15)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = stripHTML(in)
	runtime.ReadMemStats(&after)
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > uint64(len(in))*16 {
		t.Fatalf("stripHTML allocated %d bytes for %d-byte input", alloc, len(in))
	}
}

// TestGeneratePreviewKeepsUTF8Valid is the regression test for F5197: the
// byte cut at maxLen split multi-byte runes in non-ASCII subjects.
func TestGeneratePreviewKeepsUTF8Valid(t *testing.T) {
	for _, s := range []string{"a" + strings.Repeat("ş", 60), strings.Repeat("€", 50), strings.Repeat("😀", 40)} {
		if got := generatePreview(s, 100); !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 preview %q", got)
		}
	}
}
