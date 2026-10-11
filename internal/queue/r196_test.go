package queue

import (
	"sync"
	"sync/atomic"
	"testing"
)

// F6730: two manager instances sharing one database must not both claim an entry.
func TestRegressionF6730_ClaimAcrossManagers(t *testing.T) {
	m1, _, database := setupManager(t)
	defer database.Close()
	m2 := &Manager{db: m1.db, logger: m1.logger}
	id, err := m1.Enqueue("a@x.test", []string{"b@y.test"}, []byte("Subject: s\r\n\r\nb"))
	if err != nil {
		t.Fatal(err)
	}
	var wins int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, m := range []*Manager{m1, m2} {
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(m *Manager) {
				defer wg.Done()
				<-start
				if _, ok := m.claimEntry(id + "-0"); ok {
					atomic.AddInt32(&wins, 1)
				}
			}(m)
		}
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("winners = %d, want 1", wins)
	}
}
