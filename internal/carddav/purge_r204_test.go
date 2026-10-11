package carddav

import "testing"

func TestR204_PurgeUser(t *testing.T) {
	st := NewStorage(t.TempDir())
	for _, u := range []string{"alice@x.test", "Alice@x.test", "bob@x.test"} {
		if err := st.CreateAddressbook(u, &Addressbook{ID: "ab1", Name: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PurgeUser("alice@x.test"); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"alice@x.test", "Alice@x.test"} {
		if abs, _ := st.GetAddressbooks(u); len(abs) != 0 {
			t.Fatalf("%s books survive", u)
		}
	}
	if abs, _ := st.GetAddressbooks("bob@x.test"); len(abs) != 1 {
		t.Fatal("control purged")
	}
	if err := st.PurgeUser("alice@x.test"); err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeUser("a/b"); err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestR204_CanonicalUser(t *testing.T) {
	if got := canonicalUser(" Alice@X.Test. "); got != "alice@x.test" {
		t.Fatal(got)
	}
}
