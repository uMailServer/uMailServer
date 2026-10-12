package db

import (
	"errors"
	"testing"
)

// F7250: alias chains that loop back (a->b, b->a) must be refused in the
// same transaction that writes the closing alias.
func TestR248_AliasCycleRefused(t *testing.T) {
	d, _ := openR185(t)
	r256Domains(t, d, "a.test")
	if err := d.CreateAlias(&AliasData{Domain: "a.test", Alias: "a", Target: "b@a.test", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAlias(&AliasData{Domain: "a.test", Alias: "c", Target: "a@a.test", IsActive: true}); err != nil {
		t.Fatalf("non-cyclic chain must be allowed: %v", err)
	}
	if err := d.CreateAlias(&AliasData{Domain: "a.test", Alias: "b", Target: "c@a.test", IsActive: true}); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("3-cycle: want ErrAliasConflict, got %v", err)
	}
	if _, err := d.GetAlias("a.test", "b"); err == nil {
		t.Fatal("cyclic alias was stored")
	}
}

func TestR248_AliasUpdateCycleRefused(t *testing.T) {
	d, _ := openR185(t)
	r256Domains(t, d, "a.test")
	_ = d.CreateAlias(&AliasData{Domain: "a.test", Alias: "a", Target: "b@a.test", IsActive: true})
	_ = d.CreateAlias(&AliasData{Domain: "a.test", Alias: "b", Target: "ext@other.test", IsActive: true})
	if err := d.UpdateAlias(&AliasData{Domain: "a.test", Alias: "b", Target: "A@A.test", IsActive: true}); !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("update cycle: got %v", err)
	}
}
