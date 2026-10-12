package migrations

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func r256Open(t *testing.T) *bbolt.DB {
	t.Helper()
	b, err := bbolt.Open(filepath.Join(t.TempDir(), "m.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// F7333: two concurrent Rollbacks must undo two DIFFERENT migrations, never
// run the same Down twice. Rollback A holds the write transaction inside
// Down(v3) while B has already read the applied set; the old code then ran
// Down(v3) a second time for B.
func TestF7333_ConcurrentRollbackDistinctVersions(t *testing.T) {
	b := r256Open(t)
	var mu sync.Mutex
	downs := map[string]int{}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	reg := NewRegistry()
	for _, v := range []string{"1", "2", "3"} {
		v := v
		reg.Register(Migration{Version: v,
			Up: func(*bbolt.Tx) error { return nil },
			Down: func(*bbolt.Tx) error {
				mu.Lock()
				downs[v]++
				mu.Unlock()
				if v == "3" {
					once.Do(func() { close(entered); <-release })
				}
				return nil
			}})
	}
	m := NewMigrator(b, reg)
	if err := m.Migrate(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() { defer wg.Done(); errs[0] = m.Rollback() }()
	<-entered
	wg.Add(1)
	go func() { defer wg.Done(); errs[1] = m.Rollback() }()
	time.Sleep(200 * time.Millisecond) // let B reach/read before A commits
	close(release)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("rollbacks: %v %v", errs[0], errs[1])
	}
	if downs["3"] != 1 || downs["2"] != 1 || downs["1"] != 0 {
		t.Fatalf("Down calls = %v, want 3:1 2:1 1:0", downs)
	}
	if st, _ := m.Status(); st.Applied != 1 {
		t.Fatalf("applied = %d, want 1", st.Applied)
	}
}

// F7334: duplicate/empty versions are refused up front and nothing runs.
func TestF7334_DuplicateVersionRefused(t *testing.T) {
	b := r256Open(t)
	ran := 0
	up := func(*bbolt.Tx) error { ran++; return nil }
	reg := NewRegistry()
	reg.Register(Migration{Version: "1", Up: up})
	reg.Register(Migration{Version: "1", Up: up})
	if err := NewMigrator(b, reg).Migrate(); err == nil || ran != 0 {
		t.Fatalf("duplicate version: err=%v ran=%d", err, ran)
	}
	reg = NewRegistry()
	reg.Register(Migration{Version: "", Up: up})
	if err := NewMigrator(b, reg).Migrate(); err == nil || ran != 0 {
		t.Fatalf("empty version: err=%v ran=%d", err, ran)
	}
}
