package imap

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

func TestMatchesCriteriaSinceIncludesBoundary(t *testing.T) {
	boundary := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, key := range []string{"SINCE", "SENTSINCE"} {
		t.Run(key, func(t *testing.T) {
			criteria := parseSearchCriteria([]string{key, "01-Jan-2024"})
			for _, tc := range []struct {
				offset time.Duration
				want   bool
			}{
				{-24 * time.Hour, false},
				{-time.Hour, false},
				{0, true},
				{time.Hour, true},
				{24 * time.Hour, true},
			} {
				at := boundary.Add(tc.offset)
				meta := &storage.MessageMetadata{InternalDate: at, Date: at.Format(time.RFC1123Z)}
				if got := matchesCriteria(meta, nil, &criteria); got != tc.want {
					t.Errorf("%s at %v = %v, want %v", key, at, got, tc.want)
				}
			}
		})
	}
}
