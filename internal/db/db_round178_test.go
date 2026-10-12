package db

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// F6560: UpdateAlias must apply the same self-loop/mailbox-shadow rules as
// CreateAlias.
func TestF6560_UpdateAliasRejectsSelfLoop(t *testing.T) {
	d := openR126(t)
	r256Domains(t, d, "ex.com")
	if err := d.CreateAlias(&AliasData{Alias: "sales", Domain: "ex.com", Target: "bob@ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	err := d.UpdateAlias(&AliasData{Alias: "sales", Domain: "ex.com", Target: "sales@ex.com", IsActive: true})
	if !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("self-target update: %v", err)
	}
	if got, _ := d.GetAlias("ex.com", "sales"); got.Target != "bob@ex.com" {
		t.Fatalf("target changed to %q", got.Target)
	}
}

// F6561: ConsumeTOTPStep must only move the guard forward.
func TestF6561_ConsumeTOTPStepMonotonic(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob", TOTPLastUsedStep: 10}); err != nil {
		t.Fatal(err)
	}
	ok, err := d.ConsumeTOTPStep("ex.com", "bob", 10, 5)
	if err != nil || ok {
		t.Fatalf("rewind accepted ok=%v err=%v", ok, err)
	}
	if a, _ := d.GetAccount("ex.com", "bob"); a.TOTPLastUsedStep != 10 {
		t.Fatalf("step=%d", a.TOTPLastUsedStep)
	}
	if ok, err := d.ConsumeTOTPStep("ex.com", "bob", 10, 11); err != nil || !ok {
		t.Fatalf("forward: %v %v", ok, err)
	}
}

// F6562: fn must not be able to change the identity that keys the row.
func TestF6562_MutateAccountKeepsIdentity(t *testing.T) {
	d := openR126(t)
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob", Email: "bob@ex.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MutateAccount("ex.com", "bob", func(a *AccountData) error {
		a.Domain, a.LocalPart = "evil.com", "root"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetAccount("ex.com", "bob")
	if got.Domain != "ex.com" || got.LocalPart != "bob" {
		t.Fatalf("identity changed: %s/%s", got.Domain, got.LocalPart)
	}
}

// F6563: a revocation lookup that finds nothing to clean must not commit a
// write transaction (fsync per authenticated request).
func TestF6563_IsTokenRevokedReadOnly(t *testing.T) {
	d := openR126(t)
	st := d.BoltDB().Stats()
	before := st.TxStats.GetWrite()
	for i := 0; i < 5; i++ {
		if r, err := d.IsTokenRevoked("nope"); err != nil || r {
			t.Fatal(r, err)
		}
	}
	st = d.BoltDB().Stats()
	if after := st.TxStats.GetWrite(); after != before {
		t.Fatalf("write txs: %d -> %d", before, after)
	}
}

// F6565: lock contention names the file and the cause.
func TestF6565_OpenLockedMessage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, err = Open(p)
	if err == nil || !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err=%v", err)
	}
}

// F6566: deleting an account/domain removes aliases that target it.
func TestF6566_DeleteCascadesAliasTargets(t *testing.T) {
	d := openR126(t)
	r256Domains(t, d, "ex.com", "ex.org")
	if err := d.CreateAccount(&AccountData{Domain: "ex.com", LocalPart: "bob"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []*AliasData{
		{Alias: "sales", Domain: "ex.com", Target: "Bob@ex.com", IsActive: true},
		{Alias: "other", Domain: "ex.com", Target: "amy@ex.com", IsActive: true},
		{Alias: "x", Domain: "ex.org", Target: "bob@ex.com", IsActive: true},
	} {
		if err := d.CreateAlias(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.DeleteAccount("ex.com", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetAlias("ex.com", "sales"); err == nil {
		t.Fatal("alias to deleted account survived")
	}
	if _, err := d.GetAlias("ex.org", "x"); err == nil {
		t.Fatal("cross-domain alias to deleted account survived")
	}
	if _, err := d.GetAlias("ex.com", "other"); err != nil {
		t.Fatal("unrelated alias removed")
	}
	if err := d.CreateAlias(&AliasData{Alias: "y", Domain: "ex.org", Target: "q@ex.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteDomain("ex.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetAlias("ex.org", "y"); err == nil {
		t.Fatal("alias to deleted domain survived")
	}
}
