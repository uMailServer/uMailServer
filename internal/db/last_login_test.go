package db

// F7782: nothing ever set AccountData.LastLoginAt.

import (
	"testing"
	"time"
)

func TestRecordLogin(t *testing.T) {
	d, err := Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.CreateAccount(&AccountData{Email: "u@ex.com", LocalPart: "u", Domain: "ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	last := func() time.Time {
		a, err := d.GetAccount("ex.com", "u")
		if err != nil {
			t.Fatal(err)
		}
		return a.LastLoginAt
	}
	t0 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	hour := time.Hour

	if err := d.RecordLogin("ex.com", "u", t0, hour); err != nil || !last().Equal(t0) {
		t.Fatalf("first login: err=%v last=%v", err, last())
	}
	// A login within the interval keeps the stored time.
	if err := d.RecordLogin("ex.com", "u", t0.Add(30*time.Minute), hour); err != nil || !last().Equal(t0) {
		t.Fatalf("login after 30m rewrote it: err=%v last=%v", err, last())
	}
	// Past the interval it moves forward; an older time never moves it back.
	if err := d.RecordLogin("ex.com", "u", t0.Add(2*time.Hour), hour); err != nil || !last().Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("login after 2h: err=%v last=%v", err, last())
	}
	if err := d.RecordLogin("ex.com", "u", t0, hour); err != nil || !last().Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("an older time moved it back: err=%v last=%v", err, last())
	}
	// An account that is not stored (an LDAP user) is not an error.
	if err := d.RecordLogin("ex.com", "nobody", t0, hour); err != nil {
		t.Fatalf("missing account: %v", err)
	}

	// An admin edit working from an older snapshot keeps the recorded login.
	stale, _ := d.GetAccount("ex.com", "u")
	stale.LastLoginAt = time.Time{}
	stale.IsAdmin = true
	if err := d.UpdateAccount(stale); err != nil {
		t.Fatal(err)
	}
	if !last().Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("UpdateAccount erased the last login: %v", last())
	}
}
