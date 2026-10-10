package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func openR126(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "r126.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// F6080: a stale write-back must not resurrect a deleted account.
func TestF6080_UpdateAccountDoesNotResurrect(t *testing.T) {
	d := openR126(t)
	a := &AccountData{Domain: "ex.com", LocalPart: "bob", PasswordHash: "h", IsActive: true}
	if err := d.CreateAccount(a); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteAccount("ex.com", "bob"); err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateAccount(a); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("want ErrAccountNotFound, got %v", err)
	}
	if _, err := d.GetAccount("ex.com", "bob"); err == nil {
		t.Fatal("deleted account resurrected")
	}
}

// F6081: same for aliases.
func TestF6081_UpdateAliasDoesNotResurrect(t *testing.T) {
	d := openR126(t)
	al := &AliasData{Alias: "s", Domain: "ex.com", Target: "bob@ex.com", IsActive: true}
	if err := d.CreateAlias(al); err != nil {
		t.Fatal(err)
	}
	_ = d.DeleteAlias("ex.com", "s")
	if err := d.UpdateAlias(al); !errors.Is(err, ErrAliasNotFound) {
		t.Fatalf("want ErrAliasNotFound, got %v", err)
	}
	if _, err := d.GetAlias("ex.com", "s"); err == nil {
		t.Fatal("deleted alias resurrected")
	}
}

// F6082: same for domains (a resurrected domain also carries stale DKIM keys).
func TestF6082_UpdateDomainDoesNotResurrect(t *testing.T) {
	d := openR126(t)
	dm := &DomainData{Name: "ex.com", IsActive: true, DKIMPrivateKey: "k"}
	if err := d.CreateDomain(dm); err != nil {
		t.Fatal(err)
	}
	_ = d.DeleteDomain("ex.com")
	if err := d.UpdateDomain(dm); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("want ErrDomainNotFound, got %v", err)
	}
	if _, err := d.GetDomain("ex.com"); err == nil {
		t.Fatal("deleted domain resurrected")
	}
}

// F6083: an alias may not shadow a mailbox (any case) or point at itself.
func TestF6083_AliasCollisions(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "Bob", Domain: "ex.com", Target: "x@ex.com", IsActive: true}); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("alias shadowing mailbox: got %v", err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "loop", Domain: "ex.com", Target: "LOOP@ex.com", IsActive: true}); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("self alias: got %v", err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "ok", Domain: "ex.com", Target: "bob@ex.com", IsActive: true}); err != nil {
		t.Fatalf("valid alias rejected: %v", err)
	}
}

// F6084: account creation must not shadow an existing alias.
func TestF6084_AccountVsExistingAlias(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAlias(&AliasData{Alias: "sales", Domain: "ex.com", Target: "bob@ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "Sales"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("got %v", err)
	}
}

// F6085: names that break the key scheme are rejected.
func TestF6085_InvalidNames(t *testing.T) {
	d := openR126(t)
	bad := []*AccountData{
		{Domain: "", LocalPart: "a"},
		{Domain: "ex.com", LocalPart: ""},
		{Domain: "ex.com/x", LocalPart: "a"},
		{Domain: "ex.com", LocalPart: "a/b"},
	}
	for _, a := range bad {
		if err := d.CreateAccount(a); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CreateAccount(%q,%q): %v", a.Domain, a.LocalPart, err)
		}
	}
	for _, n := range []string{"", "a/b", "a:b"} {
		if err := d.CreateDomain(&DomainData{Name: n}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("CreateDomain(%q): %v", n, err)
		}
	}
	if err := d.CreateAlias(&AliasData{Alias: "", Domain: "ex.com", Target: "a@ex.com"}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty alias: %v", err)
	}
}
