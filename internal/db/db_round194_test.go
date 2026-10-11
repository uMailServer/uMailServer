package db

import (
	"errors"
	"sync"
	"testing"
)

func seedAdmins(t *testing.T, d *DB, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := d.CreateAccount(&AccountData{LocalPart: n, Domain: "a.test", PasswordHash: "x", IsAdmin: true, IsActive: true}); err != nil {
			t.Fatal(err)
		}
	}
}

// F6710: the last-admin invariant lives in the write transaction.
func TestR194_DeleteAccountKeepAdmin(t *testing.T) {
	d, _ := openR185(t)
	seedAdmins(t, d, "x", "y")
	_ = d.CreateAccount(&AccountData{LocalPart: "u", Domain: "a.test", PasswordHash: "x", IsActive: true})
	if err := d.DeleteAccountKeepAdmin("a.test", "u"); err != nil { // control: non-admin
		t.Fatal(err)
	}
	if err := d.DeleteAccountKeepAdmin("a.test", "x"); err != nil { // another admin remains
		t.Fatal(err)
	}
	if err := d.DeleteAccountKeepAdmin("a.test", "y"); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("last admin delete: %v", err)
	}
	if _, err := d.GetAccount("a.test", "y"); err != nil {
		t.Fatalf("last admin vanished: %v", err)
	}
	if err := d.DeleteAccountKeepAdmin("a.test", "zz"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestR194_MutateAccountKeepAdminConcurrentDemotion(t *testing.T) {
	d, _ := openR185(t)
	seedAdmins(t, d, "x", "y")
	var wg sync.WaitGroup
	res := make([]error, 2)
	for i, n := range []string{"x", "y"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, res[i] = d.MutateAccountKeepAdmin("a.test", n, func(a *AccountData) error { a.IsActive = false; return nil })
		}()
	}
	wg.Wait()
	ok, refused := 0, 0
	for _, e := range res {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrLastActiveAdmin) {
			refused++
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("want exactly one demotion to succeed, got %v", res)
	}
	// Control: plain edit of the remaining admin is fine, demotion refused.
	if _, err := d.MutateAccountKeepAdmin("a.test", "x", func(a *AccountData) error { a.ForwardTo = "q@a.test"; return nil }); err != nil && !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatal(err)
	}
}

func TestR194_DeleteDomainKeepAdmin(t *testing.T) {
	d, _ := openR185(t)
	_ = d.CreateDomain(&DomainData{Name: "a.test", IsActive: true})
	_ = d.CreateDomain(&DomainData{Name: "b.test", IsActive: true})
	seedAdmins(t, d, "x")
	if err := d.DeleteDomainKeepAdmin("a.test"); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("got %v", err)
	}
	if err := d.DeleteDomainKeepAdmin("b.test"); err != nil { // control: no admins inside
		t.Fatal(err)
	}
	_ = d.CreateAccount(&AccountData{LocalPart: "o", Domain: "c.test", PasswordHash: "x", IsAdmin: true, IsActive: true})
	if err := d.DeleteDomainKeepAdmin("a.test"); err != nil {
		t.Fatal(err)
	}
}
