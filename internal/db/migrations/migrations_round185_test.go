package migrations

import (
	"errors"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func openR185(t *testing.T) *bbolt.DB {
	t.Helper()
	d, err := bbolt.Open(filepath.Join(t.TempDir(), "m.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func mk(v string, up func(*bbolt.Tx) error) Migration { return Migration{Version: v, Up: up} }

func marker(name string) func(*bbolt.Tx) error {
	return func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("data"))
		if err != nil {
			return err
		}
		return b.Put([]byte(name), []byte("1"))
	}
}

// F6621: a failing migration leaves earlier ones in the batch unapplied too.
func TestR185_MigrateIsAtomic(t *testing.T) {
	db := openR185(t)
	boom := errors.New("boom")
	r := NewRegistry()
	r.Register(mk("001", marker("one")))
	r.Register(mk("002", func(*bbolt.Tx) error { return boom }))
	r.Register(mk("003", marker("three")))
	err := NewMigrator(db, r).Migrate()
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	st, _ := NewMigrator(db, r).Status()
	if st.Applied != 0 {
		t.Fatalf("applied=%d after failed batch, want 0", st.Applied)
	}
	_ = db.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket([]byte("data")); b != nil {
			t.Fatal("partial migration effects leaked")
		}
		return nil
	})
}

// Idempotent, and a fixed registry then succeeds from a clean state.
func TestR185_MigrateIdempotentAfterFix(t *testing.T) {
	db := openR185(t)
	count := 0
	r := NewRegistry()
	r.Register(mk("001", func(tx *bbolt.Tx) error { count++; return marker("one")(tx) }))
	for i := 0; i < 3; i++ {
		if err := NewMigrator(db, r).Migrate(); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("Up ran %d times", count)
	}
}

// F6620: newer-binary data is refused by Migrate, Rollback and CheckCompatible.
func TestR185_TooNewRefused(t *testing.T) {
	db := openR185(t)
	r := NewRegistry()
	r.Register(mk("001", marker("one")))
	r.Register(mk("002", marker("two")))
	if err := NewMigrator(db, r).Migrate(); err != nil {
		t.Fatal(err)
	}
	old := NewRegistry()
	old.Register(mk("001", marker("one")))
	m := NewMigrator(db, old)
	if err := m.CheckCompatible(); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("check: %v", err)
	}
	if err := m.Migrate(); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("migrate: %v", err)
	}
	if err := m.Rollback(); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("rollback: %v", err)
	}
	st, _ := NewMigrator(db, r).Status()
	if st.Applied != 2 {
		t.Fatalf("records were modified: %d", st.Applied)
	}
}

func TestR185_RollbackPicksHighestVersion(t *testing.T) {
	db := openR185(t)
	r := NewRegistry()
	downs := []string{}
	for _, v := range []string{"001", "002", "003"} {
		v := v
		r.Register(Migration{Version: v, Up: marker(v), Down: func(*bbolt.Tx) error { downs = append(downs, v); return nil }})
	}
	if err := NewMigrator(db, r).Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := NewMigrator(db, r).Rollback(); err != nil {
		t.Fatal(err)
	}
	if len(downs) != 1 || downs[0] != "003" {
		t.Fatalf("rolled back %v, want [003]", downs)
	}
}
