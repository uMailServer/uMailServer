package storage

import (
	"fmt"
	"sync"
	"testing"
)

// R208 F6850: concurrent first messages of one conversation must share a
// single thread ID (reservation is atomic).
func TestR208GetOrCreateThreadIDConcurrentSameThread(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/m.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const n = 32
	ids := make([]string, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			subj := "Re: Lunch plans"
			if i%2 == 0 {
				subj = "Lunch plans"
			}
			id, err := db.GetOrCreateThreadID("u@x.com", "INBOX", subj, "", nil)
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}(i)
	}
	close(start)
	wg.Wait()
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] || ids[0] == "" {
			t.Fatalf("thread ids diverged: %s vs %s", ids[0], fmt.Sprint(ids[i]))
		}
	}
	other, _ := db.GetOrCreateThreadID("u@x.com", "INBOX", "Different", "", nil)
	if other == ids[0] {
		t.Fatal("different subject shared thread")
	}
}
