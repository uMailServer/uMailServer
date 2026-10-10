package db

import (
	"errors"
	"sync"
	"testing"
)

// F6250/F6256: MutateAccount applies fn to the stored row atomically; the
// session cut-off and the counters never move backwards, and UpdateAccount
// with a stale snapshot cannot rewind the cut-off.
func TestF6256_MutateAccountAtomicAndMonotonic(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob", PasswordHash: "h", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	stale, _ := d.GetAccount("ex.com", "bob")

	if _, err := d.MutateAccount("ex.com", "bob", func(a *AccountData) error {
		a.TokensValidAfter = 500
		a.QuotaUsed = 99 // ignored: quota is owned by IncrementQuota
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MutateAccount("ex.com", "bob", func(a *AccountData) error {
		a.TokensValidAfter = 100 // would rewind
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetAccount("ex.com", "bob")
	if got.TokensValidAfter != 500 || got.QuotaUsed != 0 {
		t.Fatalf("cutoff=%d quota=%d", got.TokensValidAfter, got.QuotaUsed)
	}
	stale.PasswordHash = "stale"
	if err := d.UpdateAccount(stale); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetAccount("ex.com", "bob")
	if got.TokensValidAfter != 500 {
		t.Fatalf("stale UpdateAccount rewound the session cut-off to %d", got.TokensValidAfter)
	}

	// abort leaves the row untouched; missing account is ErrAccountNotFound
	boom := errors.New("boom")
	if _, err := d.MutateAccount("ex.com", "bob", func(a *AccountData) error { a.PasswordHash = "x"; return boom }); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if got, _ = d.GetAccount("ex.com", "bob"); got.PasswordHash != "stale" {
		t.Fatalf("aborted mutation was written: %q", got.PasswordHash)
	}
	if _, err := d.MutateAccount("ex.com", "nobody", func(*AccountData) error { return nil }); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("err=%v", err)
	}
}

// Concurrent field updates through MutateAccount do not lose each other.
func TestF6256_MutateAccountConcurrentFields(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = d.MutateAccount("ex.com", "bob", func(a *AccountData) error { a.ForwardKeepCopy = true; return nil })
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _ = d.MutateAccount("ex.com", "bob", func(a *AccountData) error { a.IsAdmin = true; return nil })
		}
	}()
	wg.Wait()
	got, _ := d.GetAccount("ex.com", "bob")
	if !got.ForwardKeepCopy || !got.IsAdmin {
		t.Fatalf("lost update: %+v", got)
	}
}
