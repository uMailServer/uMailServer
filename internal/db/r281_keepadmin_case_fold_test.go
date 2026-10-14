package db

import (
	"errors"
	"testing"
)

// F7550: DeleteDomainKeepAdmin's guard must see admins stored under a
// case-variant of the domain name, because the cascade deletes them too.
func TestR281_DeleteDomainKeepAdminCaseFold(t *testing.T) {
	d, _ := openR185(t)
	if err := d.CreateDomain(&DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateAccount(&AccountData{LocalPart: "root", Domain: "EXAMPLE.com", IsAdmin: true, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteDomainKeepAdmin("example.com"); !errors.Is(err, ErrLastActiveAdmin) {
		t.Fatalf("want ErrLastActiveAdmin, got %v", err)
	}
	if a, _ := d.GetAccount("EXAMPLE.com", "root"); a == nil {
		t.Fatal("last admin was deleted")
	}
	// Another admin elsewhere lets the cascade proceed.
	if err := d.CreateAccount(&AccountData{LocalPart: "adm", Domain: "other.com", IsAdmin: true, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteDomainKeepAdmin("example.com"); err != nil {
		t.Fatalf("delete with another admin: %v", err)
	}
	if a, _ := d.GetAccount("EXAMPLE.com", "root"); a != nil {
		t.Fatal("case-variant account should be cascaded")
	}
	// Inactive case-variant admin does not count / block.
	_ = d.CreateDomain(&DomainData{Name: "x.com", IsActive: true})
	_ = d.CreateAccount(&AccountData{LocalPart: "off", Domain: "X.COM", IsAdmin: true, IsActive: false})
	if err := d.DeleteDomainKeepAdmin("x.com"); err != nil {
		t.Fatalf("inactive admin must not block: %v", err)
	}
}
