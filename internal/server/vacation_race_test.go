package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestSendVacationReply_ConcurrentCleanupRace drives sendVacationReply past the
// 100-entry cleanup threshold from many goroutines at once. The cleanup must run
// while vacationRepliesMu is held; run with -race to catch a regression.
func TestSendVacationReply_ConcurrentCleanupRace(t *testing.T) {
	s := &Server{vacationReplies: make(map[string]time.Time)}
	stale := time.Now().Add(-72 * time.Hour)
	for i := 0; i < 150; i++ {
		s.vacationReplies[fmt.Sprintf("r%d@example.com|s@example.com", i)] = stale
	}

	settings := `{"enabled":true,"message":"away"}`
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.sendVacationReply("me@example.com", fmt.Sprintf("sender%d@example.com", i), settings)
		}(i)
	}
	wg.Wait()

	s.vacationRepliesMu.Lock()
	defer s.vacationRepliesMu.Unlock()
	for key, sent := range s.vacationReplies {
		if sent.Equal(stale) {
			t.Fatalf("stale entry %q survived cleanup", key)
		}
	}
	if got := len(s.vacationReplies); got != 64 {
		t.Fatalf("expected 64 fresh entries after cleanup, got %d", got)
	}
}
