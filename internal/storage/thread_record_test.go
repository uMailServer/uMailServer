package storage

import (
	"sync"
	"testing"
	"time"
)

func TestRecordMessageInThread(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t0 := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	if err := db.RecordMessageInThread("u@x.test", "t1", "Re: Plan", "a@x.test", t0, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordMessageInThread("u@x.test", "t1", "Re: Plan", "b@x.test", t0.Add(time.Hour), []string{"\\Seen"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordMessageInThread("u@x.test", "t1", "Re: Plan", "a@x.test", t0.Add(2*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	th, err := db.GetThread("u@x.test", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if th.MessageCount != 3 || th.UnreadCount != 2 || len(th.Participants) != 2 || !th.LastActivity.Equal(t0.Add(2*time.Hour)) || th.Subject != NormalizeSubject("Re: Plan") {
		t.Fatalf("thread = %+v", th)
	}
	// users are isolated
	if _, err := db.GetThread("other@x.test", "t1"); err == nil {
		t.Error("thread visible to another user")
	}
	if err := db.RecordMessageInThread("u@x.test", "", "s", "a", t0, nil); err == nil {
		t.Error("empty thread id accepted")
	}
}

// Concurrent deliveries into one conversation must not lose updates (the old
// get-then-put in two transactions did).
func TestRecordMessageInThreadConcurrent(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := db.RecordMessageInThread("u@x.test", "t1", "s", "a@x.test", time.Now(), nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	th, err := db.GetThread("u@x.test", "t1")
	if err != nil || th.MessageCount != n || th.UnreadCount != n {
		t.Fatalf("thread = %+v err=%v, want %d messages", th, err, n)
	}
}
