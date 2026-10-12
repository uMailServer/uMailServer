package db

import (
	"errors"
	"sync"
	"testing"
)

// r256Domains registers domains so aliases may be created under them (F7330).
func r256Domains(t *testing.T, d *DB, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := d.CreateDomain(&DomainData{Name: n, IsActive: true}); err != nil {
			t.Fatal(err)
		}
	}
}

// F7330: an alias under an unknown (or deleted) domain is refused.
func TestF7330_CreateAliasRequiresDomain(t *testing.T) {
	d := openR126(t)
	err := d.CreateAlias(&AliasData{Alias: "a", Domain: "nodomain.test", Target: "x@y.test", IsActive: true})
	if !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("want ErrDomainNotFound, got %v", err)
	}
	r256Domains(t, d, "gone.test")
	if err := d.DeleteDomain("gone.test"); err != nil {
		t.Fatal(err)
	}
	err = d.CreateAlias(&AliasData{Alias: "a", Domain: "gone.test", Target: "x@y.test", IsActive: true})
	if !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("deleted domain: want ErrDomainNotFound, got %v", err)
	}
	if al, _ := d.ListAliases(); len(al) != 0 {
		t.Fatalf("orphan alias stored: %v", al)
	}
}

// F7330: concurrent CreateAlias / DeleteDomain never leave an orphan alias.
func TestF7330_NoOrphanAliasUnderRace(t *testing.T) {
	d := openR126(t)
	for i := 0; i < 50; i++ {
		r256Domains(t, d, "race.test")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = d.CreateAlias(&AliasData{Alias: "a", Domain: "race.test", Target: "x@y.test", IsActive: true})
		}()
		go func() { defer wg.Done(); _ = d.DeleteDomain("race.test") }()
		wg.Wait()
		if _, err := d.GetDomain("race.test"); err != nil {
			if al, _ := d.ListAliases(); len(al) != 0 {
				t.Fatalf("iteration %d: orphan alias of deleted domain: %v", i, al)
			}
		} else {
			_ = d.DeleteDomain("race.test")
		}
	}
}

// F7331: DeleteDomain matches case-insensitively, like CreateDomain.
func TestF7331_DeleteDomainCaseInsensitive(t *testing.T) {
	d := openR126(t)
	r256Domains(t, d, "Case.Test")
	if err := d.CreateAlias(&AliasData{Alias: "a", Domain: "Case.Test", Target: "x@y.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&AccountData{Domain: "Case.Test", LocalPart: "bob", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteDomain("case.test"); err != nil {
		t.Fatal(err)
	}
	if ds, _ := d.ListDomains(); len(ds) != 0 {
		t.Fatalf("domain survived: %v", ds)
	}
	if al, _ := d.ListAliases(); len(al) != 0 {
		t.Fatalf("alias survived: %v", al)
	}
	if ac, _ := d.ListAccountsByDomain("Case.Test"); len(ac) != 0 {
		t.Fatalf("account survived: %v", ac)
	}
}
