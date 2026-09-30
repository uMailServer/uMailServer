package imap

import (
	"fmt"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// These tests pin the ordering contract of sortMessagesByCriteria (RFC 5256 SORT).
//
// The sort is deliberately implemented with sort.SliceStable so that messages
// comparing equal on the sort key keep their implicit mailbox order. That only
// holds if the comparator is a strict weak ordering: Less(i, j) must be false
// when i and j compare equal. sortMessagesByCriteria computed the descending
// comparator as !less, which reports true for equal elements and therefore
// reports an element as "before" itself.

// round005Arrival is a fixed timestamp so the proof does not depend on the clock.
var round005Arrival = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

// round005Messages builds n messages with identical size, subject, date and
// internal date, so that every supported sort field ties across the set.
func round005Messages(n int) []*storage.MessageMetadata {
	msgs := make([]*storage.MessageMetadata, n)
	for i := range msgs {
		msgs[i] = &storage.MessageMetadata{
			MessageID:    fmt.Sprintf("m%d", i+1),
			UID:          uint32(100 + i),
			Subject:      "Same Subject",
			From:         "sender@example.com",
			Date:         "1 Jan 2024 10:00:00 +0000",
			InternalDate: round005Arrival,
			Size:         100,
		}
	}
	return msgs
}

func round005SeqNums(n int) []uint32 {
	seq := make([]uint32, n)
	for i := range seq {
		seq[i] = uint32(i + 1)
	}
	return seq
}

// TestSortMessagesByCriteria_EqualKeysKeepStableOrder is the failing proof: a
// descending sort over messages that all compare equal must not reorder them.
func TestSortMessagesByCriteria_EqualKeysKeepStableOrder(t *testing.T) {
	const n = 4
	msgs := round005Messages(n)
	seq := round005SeqNums(n)
	want := []uint32{1, 2, 3, 4}

	// Every supported field is checked because they all share the comparator.
	fields := []string{"ARRIVAL", "DATE", "FROM", "SUBJECT", "SIZE"}

	for _, field := range fields {
		for _, desc := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/descending=%v", field, desc), func(t *testing.T) {
				got := sortMessagesByCriteria(msgs, []SortCriterion{{Field: field, Descending: desc}}, seq)
				if len(got) != n {
					t.Fatalf("got %d results, want %d (got=%v)", len(got), n, got)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("%s descending=%v: got %v, want %v", field, desc, got, want)
						return
					}
				}
			})
		}
	}
}

// TestSortMessagesByCriteria_DescendingDistinctKeys is the control: with all
// keys distinct the descending order is unambiguous and must hold before and
// after any fix. It also guards against "fixing" ties by breaking real order.
func TestSortMessagesByCriteria_DescendingDistinctKeys(t *testing.T) {
	// Sizes 10, 30, 20, 40 descending -> sequence numbers 4, 2, 3, 1.
	sizes := []int64{10, 30, 20, 40}
	msgs := make([]*storage.MessageMetadata, len(sizes))
	for i, sz := range sizes {
		msgs[i] = &storage.MessageMetadata{
			MessageID:    fmt.Sprintf("m%d", i+1),
			UID:          uint32(100 + i),
			Subject:      fmt.Sprintf("Subject %c", 'A'+i),
			From:         fmt.Sprintf("sender%d@example.com", i),
			Date:         fmt.Sprintf("%d Jan 2024 10:00:00 +0000", i+1),
			InternalDate: round005Arrival.Add(time.Duration(i) * time.Hour),
			Size:         sz,
		}
	}
	seq := round005SeqNums(len(sizes))
	want := []uint32{4, 2, 3, 1}

	got := sortMessagesByCriteria(msgs, []SortCriterion{{Field: "SIZE", Descending: true}}, seq)
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			return
		}
	}
}
