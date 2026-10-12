package auth

import (
	"testing"

	"github.com/go-ldap/ldap/v3"
)

// F7046: GetUser must fail closed when the filter matches several entries.
func TestSingleLDAPEntryFailsClosed(t *testing.T) {
	e := func(dn string) *ldap.Entry { return &ldap.Entry{DN: dn} }
	if _, err := singleLDAPEntry(nil); err == nil {
		t.Fatal("zero entries must error")
	}
	if got, err := singleLDAPEntry([]*ldap.Entry{e("a")}); err != nil || got.DN != "a" {
		t.Fatalf("one entry: %v %v", got, err)
	}
	if _, err := singleLDAPEntry([]*ldap.Entry{e("a"), e("b")}); err == nil {
		t.Fatal("multiple entries must error")
	}
}
