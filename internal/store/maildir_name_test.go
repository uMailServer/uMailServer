package store

// Regression test for the generateUniqueName collision defect: the name was
// built as timestamp.pid+micros-concat.hostname with NO unique component (the
// comment claimed "random component for uniqueness" but none existed), so two
// Deliver calls landing in the same microsecond produced identical filenames
// and the cur/ rename silently OVERWROTE the first message (data loss). The
// pid+micros concatenation was also non-monotonic across digit-width changes
// (micros=9999 sorts after micros=10000), so filename-sorted listings
// diverged from delivery order — maildir listing order is message order.
// Fix: a per-process atomic counter in the unique part, with fixed-width
// fields so lexicographic order matches delivery order.

import (
	"testing"
)

// Rapid-fire generation must never repeat a name: each call becomes a maildir
// filename, and a duplicate means one delivered message silently overwrites
// another via rename.
func TestGenerateUniqueName_NoCollisionsUnderRapidDelivery(t *testing.T) {
	s := NewMaildirStore(t.TempDir())

	const n = 200000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		name := s.generateUniqueName()
		if _, dup := seen[name]; dup {
			t.Fatalf("FAIL: generateUniqueName produced a duplicate after %d calls (%d unique): %q — two deliveries sharing this filename would overwrite each other", i+1, len(seen), name)
		}
		seen[name] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("FAIL: %d unique names from %d calls", len(seen), n)
	}
}

// The unique part must sort monotonically with delivery order even when the
// microsecond value crosses digit-width boundaries (9999 -> 10000): maildir
// listings are filename-sorted, and message order must match delivery order.
// This exercises the real production formatter with adversarial widths.
func TestFormatMaildirName_MonotonicAcrossMicrosWidths(t *testing.T) {
	deliveryOrder := []string{
		formatMaildirName(1790123456, 9999, 63721, 1, "host"),
		formatMaildirName(1790123456, 10000, 63721, 2, "host"),
		formatMaildirName(1790123456, 10001, 63721, 3, "host"),
		formatMaildirName(1790123456, 99999, 63721, 4, "host"),
		formatMaildirName(1790123456, 100000, 63721, 5, "host"),
	}
	for i := 1; i < len(deliveryOrder); i++ {
		if deliveryOrder[i-1] >= deliveryOrder[i] {
			t.Fatalf("FAIL: filename order breaks delivery order at index %d:\n  [%d] %s\n  [%d] %s",
				i, i-1, deliveryOrder[i-1], i, deliveryOrder[i])
		}
	}
}
