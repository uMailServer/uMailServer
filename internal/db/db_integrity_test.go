package db

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func openIntegrityDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "integrity.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// F4836: a duplicate CreateDomain must not overwrite the stored domain
// (DKIM private key, catch-all, active flag).
func TestCreateDomainRejectsDuplicate(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateDomain(&DomainData{Name: "a.test", IsActive: true, DKIMPrivateKey: "K1"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := d.CreateDomain(&DomainData{Name: "a.test", DKIMPrivateKey: "K2"}); !errors.Is(err, ErrDomainExists) {
		t.Fatalf("duplicate create: err=%v, want ErrDomainExists", err)
	}
	got, err := d.GetDomain("a.test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DKIMPrivateKey != "K1" || !got.IsActive {
		t.Fatalf("stored domain overwritten: %+v", got)
	}
}

// F4837: a duplicate CreateAlias (case-insensitive local part) must not
// retarget the existing alias.
func TestCreateAliasRejectsDuplicate(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAlias(&AliasData{Alias: "sales", Domain: "a.test", Target: "bob@a.test", IsActive: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "Sales", Domain: "a.test", Target: "mallory@a.test", IsActive: true}); !errors.Is(err, ErrAliasExists) {
		t.Fatalf("duplicate create: err=%v, want ErrAliasExists", err)
	}
	if tg, err := d.ResolveAlias("a.test", "sales"); err != nil || tg != "bob@a.test" {
		t.Fatalf("alias retargeted: target=%q err=%v", tg, err)
	}
}

// F4838: DeleteDomain must remove the domain's accounts and aliases in the
// same transaction and leave other domains (including prefix-sharing names)
// untouched.
func TestDeleteDomainCascades(t *testing.T) {
	d := openIntegrityDB(t)
	for _, dom := range []string{"a.test", "a.testx"} {
		if err := d.CreateDomain(&DomainData{Name: dom, IsActive: true}); err != nil {
			t.Fatal(err)
		}
		if err := d.CreateAccount(&AccountData{Domain: dom, LocalPart: "bob", IsActive: true}); err != nil {
			t.Fatal(err)
		}
		if err := d.CreateAlias(&AliasData{Alias: "sales", Domain: dom, Target: "bob@" + dom, IsActive: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.DeleteDomain("a.test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := d.GetAccount("a.test", "bob"); err == nil {
		t.Fatal("orphan account of deleted domain still present")
	}
	if _, err := d.GetAlias("a.test", "sales"); err == nil {
		t.Fatal("orphan alias of deleted domain still present")
	}
	if _, err := d.GetAccount("a.testx", "bob"); err != nil {
		t.Fatalf("other domain account removed: %v", err)
	}
	if _, err := d.GetAlias("a.testx", "sales"); err != nil {
		t.Fatalf("other domain alias removed: %v", err)
	}
	if err := d.DeleteDomain("a.test"); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
}

// F5165: addresses and domains differing only by letter case name the same
// mailbox/domain, so a case variant must be rejected as a duplicate instead
// of creating a second account, domain or alias.
func TestCreateRejectsCaseVariantDuplicates(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAccount(&AccountData{Domain: "example.com", LocalPart: "alice", PasswordHash: "h1"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := d.CreateAccount(&AccountData{Domain: "Example.COM", LocalPart: "Alice", PasswordHash: "h2"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("case-variant account: err=%v, want ErrAccountExists", err)
	}
	if err := d.CreateAccount(&AccountData{Domain: "example.com", LocalPart: "alice2"}); err != nil {
		t.Fatalf("distinct account rejected: %v", err)
	}
	if err := d.CreateDomain(&DomainData{Name: "example.com", DKIMPrivateKey: "K1"}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if err := d.CreateDomain(&DomainData{Name: "EXAMPLE.com", DKIMPrivateKey: "K2"}); !errors.Is(err, ErrDomainExists) {
		t.Fatalf("case-variant domain: err=%v, want ErrDomainExists", err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "sales", Domain: "example.com", Target: "alice@example.com", IsActive: true}); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if err := d.CreateAlias(&AliasData{Alias: "Sales", Domain: "Example.com", Target: "mallory@evil.test", IsActive: true}); !errors.Is(err, ErrAliasExists) {
		t.Fatalf("case-variant alias: err=%v, want ErrAliasExists", err)
	}
}

// F5166: UpdateAccount from a stale snapshot must not erase a quota
// reservation (IncrementQuota) or rewind the TOTP replay guard
// (ConsumeTOTPStep) committed after the snapshot was read.
func TestUpdateAccountKeepsConcurrentCounters(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAccount(&AccountData{Domain: "a.test", LocalPart: "bob", QuotaLimit: 10000, IsActive: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	stale, err := d.GetAccount("a.test", "bob")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	const deliveries = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < deliveries; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := d.IncrementQuota("a.test", "bob", 1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for j := 0; j < 20; j++ {
			c := *stale
			c.ForwardTo = "x@b.test"
			if err := d.UpdateAccount(&c); err != nil {
				t.Error(err)
			}
		}
	}()
	close(start)
	wg.Wait()
	if ok, err := d.ConsumeTOTPStep("a.test", "bob", 0, 100); err != nil || !ok {
		t.Fatalf("consume: ok=%v err=%v", ok, err)
	}
	c := *stale
	c.ForwardTo = "x@b.test"
	if err := d.UpdateAccount(&c); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := d.GetAccount("a.test", "bob")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.QuotaUsed != deliveries || got.TOTPLastUsedStep != 100 || got.ForwardTo != "x@b.test" {
		t.Fatalf("quota_used=%d totp_step=%d forward=%q; want %d, 100, x@b.test",
			got.QuotaUsed, got.TOTPLastUsedStep, got.ForwardTo, deliveries)
	}
}
