package imap

import (
	"testing"
	"time"
)

// Regression coverage for IMAP SEARCH command filtering.
//
// matchesCriteria previously short-circuited on `criteria.All` with an immediate
// `return true`. Because parseSearchCriteria seeds `All: true` for every parsed
// search, that made every IMAP SEARCH (UNSEEN, SEEN, FROM ..., ...) match every
// message in the mailbox. These tests drive the real parse -> search path to pin
// that a SEARCH criterion actually filters messages.
//
// RFC 3501 §6.4.4: SEARCH keys select messages; multiple keys are ANDed.

func newSearchFilterFixture(t *testing.T) (*BboltMailstore, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	ms, err := NewBboltMailstore(tmpDir)
	if err != nil {
		t.Fatalf("NewBboltMailstore: %v", err)
	}
	t.Cleanup(func() { ms.Close() })

	user := "testuser"
	mbox := "INBOX"
	if err := ms.CreateMailbox(user, mbox); err != nil {
		t.Fatalf("CreateMailbox: %v", err)
	}

	// Message 1: seen, from alice, large. Message 2: unseen, from bob, small.
	big := make([]byte, 0, 4096)
	big = append(big, []byte("From: alice@example.com\r\nSubject: Important\r\n\r\n")...)
	for len(big) < 2048 {
		big = append(big, 'a')
	}
	small := []byte("From: bob@example.com\r\nSubject: Routine\r\n\r\nBody2")

	if err := ms.AppendMessage(user, mbox, []string{"\\Seen"}, time.Now(), big); err != nil {
		t.Fatalf("AppendMessage seen: %v", err)
	}
	if err := ms.AppendMessage(user, mbox, nil, time.Now(), small); err != nil {
		t.Fatalf("AppendMessage unseen: %v", err)
	}
	return ms, user, mbox
}

// A parsed SEARCH criterion must select a subset of the mailbox, not everything.
func TestSearchCommand_CriterionFiltersMessages(t *testing.T) {
	ms, user, mbox := newSearchFilterFixture(t)

	cases := []struct {
		name      string
		args      []string
		wantCount int
	}{
		{"UNSEEN matches only the unseen message", []string{"UNSEEN"}, 1},
		{"SEEN matches only the seen message", []string{"SEEN"}, 1},
		{"FROM bob matches only bob's message", []string{"FROM", "bob"}, 1},
		{"SUBJECT Important matches only that message", []string{"SUBJECT", "Important"}, 1},
		{"ALL matches every message", []string{"ALL"}, 2},
		{"empty search matches every message", []string{}, 2},
		// AND semantics: ALL combined with another key is still that key's filter.
		// (A naive "fix" that flipped the parser default to All:false and kept
		// the short-circuit would wrongly return both here.)
		{"ALL SEEN matches only the seen message", []string{"ALL", "SEEN"}, 1},
		{"ALL UNSEEN matches only the unseen message", []string{"ALL", "UNSEEN"}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			criteria := parseSearchCriteria(tc.args)
			results, err := ms.SearchMessages(user, mbox, criteria)
			if err != nil {
				t.Fatalf("SearchMessages(%v): %v", tc.args, err)
			}
			if len(results) != tc.wantCount {
				t.Errorf("SEARCH %v returned %d message(s), want %d",
					tc.args, len(results), tc.wantCount)
			}
		})
	}
}

// UNSEEN must return exactly the unseen message (index 2), and SEEN exactly the
// seen message (index 1) — not merely the right count.
func TestSearchCommand_IdentifiesCorrectMessages(t *testing.T) {
	ms, user, mbox := newSearchFilterFixture(t)

	unseen, err := ms.SearchMessages(user, mbox, parseSearchCriteria([]string{"UNSEEN"}))
	if err != nil {
		t.Fatalf("UNSEEN: %v", err)
	}
	if len(unseen) != 1 || unseen[0] != 2 {
		t.Errorf("UNSEEN = %v, want [2] (the unseen message)", unseen)
	}

	seen, err := ms.SearchMessages(user, mbox, parseSearchCriteria([]string{"SEEN"}))
	if err != nil {
		t.Fatalf("SEEN: %v", err)
	}
	if len(seen) != 1 || seen[0] != 1 {
		t.Errorf("SEEN = %v, want [1] (the seen message)", seen)
	}
}

// Directly-constructed criteria (All unset) must keep filtering correctly; this
// path is unaffected by the bug and guards against regression of the fix.
func TestSearchCommand_DirectCriteriaStillFilters(t *testing.T) {
	ms, user, mbox := newSearchFilterFixture(t)

	unseen, err := ms.SearchMessages(user, mbox, SearchCriteria{Unseen: true})
	if err != nil {
		t.Fatalf("Unseen: %v", err)
	}
	if len(unseen) != 1 || unseen[0] != 2 {
		t.Errorf("direct Unseen = %v, want [2]", unseen)
	}

	deleted, err := ms.SearchMessages(user, mbox, SearchCriteria{Deleted: true})
	if err != nil {
		t.Fatalf("Deleted: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("direct Deleted = %v, want [] (no deleted messages)", deleted)
	}
}
