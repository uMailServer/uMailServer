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

// F5510: parallel CreateAccountInDomain calls must never exceed the domain's
// MaxAccounts. All creators are released together behind one channel; bbolt
// serializes the check-and-insert transactions.
func TestCreateAccountInDomainEnforcesLimitUnderConcurrency(t *testing.T) {
	d := openIntegrityDB(t)
	const limit, parallel = 3, 16
	if err := d.CreateDomain(&DomainData{Name: "a.test", MaxAccounts: limit, IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	// Prefix-sharing domain must not count toward a.test's limit.
	if err := d.CreateAccount(&AccountData{LocalPart: "z", Domain: "a.testx"}); err != nil {
		t.Fatalf("create other: %v", err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	created, limited := 0, 0
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := d.CreateAccountInDomain(&AccountData{LocalPart: string(rune('a' + i)), Domain: "a.test"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrDomainAccountLimit):
				limited++
			default:
				t.Errorf("create %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	list, err := d.ListAccountsByDomain("a.test")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if created != limit || limited != parallel-limit || len(list) != limit {
		t.Fatalf("created=%d limited=%d stored=%d, want %d/%d/%d", created, limited, len(list), limit, parallel-limit, limit)
	}
	// Freeing a slot allows exactly one more create; duplicates still win over the limit check.
	if err := d.DeleteAccount("a.test", list[0].LocalPart); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: list[1].LocalPart, Domain: "a.test"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("duplicate: err=%v, want ErrAccountExists", err)
	}
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: "new", Domain: "a.test"}); err != nil {
		t.Fatalf("create after delete: %v", err)
	}
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: "over", Domain: "a.test"}); !errors.Is(err, ErrDomainAccountLimit) {
		t.Fatalf("over limit: err=%v, want ErrDomainAccountLimit", err)
	}
}

// F5511: CreateAccountInDomain must reject an unknown or just-deleted domain,
// so a create racing DeleteDomain cannot leave an orphan account.
func TestCreateAccountInDomainRejectsMissingDomain(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: "bob", Domain: "none.test"}); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("unknown domain: err=%v, want ErrDomainNotFound", err)
	}
	if err := d.CreateDomain(&DomainData{Name: "a.test", IsActive: true}); err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: "alice", Domain: "a.test"}); err != nil {
		t.Fatalf("create on live domain (unlimited): %v", err)
	}
	if err := d.DeleteDomain("a.test"); err != nil {
		t.Fatalf("delete domain: %v", err)
	}
	if err := d.CreateAccountInDomain(&AccountData{LocalPart: "bob", Domain: "a.test"}); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("deleted domain: err=%v, want ErrDomainNotFound", err)
	}
	if _, err := d.GetAccount("a.test", "bob"); err == nil {
		t.Fatal("orphan account created for deleted domain")
	}
}

// F5512: a stale UpdateQueueEntry after Dequeue must not resurrect the entry.
func TestUpdateQueueEntryDoesNotResurrectDroppedEntry(t *testing.T) {
	d := openIntegrityDB(t)
	if err := d.Enqueue(&QueueEntry{ID: "q1", Status: "sending"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	snap, err := d.GetQueueEntry("q1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	snap.Status = "pending"
	if err := d.UpdateQueueEntry(snap); err != nil {
		t.Fatalf("update live entry: %v", err)
	}
	if err := d.Dequeue("q1"); err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err := d.UpdateQueueEntry(snap); !errors.Is(err, ErrQueueEntryNotFound) {
		t.Fatalf("update dropped entry: err=%v, want ErrQueueEntryNotFound", err)
	}
	if _, err := d.GetQueueEntry("q1"); err == nil {
		t.Fatal("dropped queue entry resurrected")
	}
	if err := d.UpdateQueueEntry(&QueueEntry{ID: "never"}); !errors.Is(err, ErrQueueEntryNotFound) {
		t.Fatalf("update unknown entry: err=%v, want ErrQueueEntryNotFound", err)
	}
}
