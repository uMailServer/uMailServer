package carddav

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F6910: userKey must be injective.
func TestUserKeyInjective_F6910(t *testing.T) {
	names := []string{"a@b", "a_at_b", "_at_", "@", "a@@b", "a%b", "a%5Fat%5Fb", "a%40b", ".", "x", "%"}
	seen := map[string]string{}
	for _, n := range names {
		k := userKey(n)
		if p, dup := seen[k]; dup {
			t.Errorf("collision %q and %q -> %q", p, n, k)
		}
		seen[k] = n
		if k == "" || k == "." || filepath.Base(k) != k {
			t.Errorf("unsafe key %q for %q", k, n)
		}
	}
	if got := userKey("alice@example.com"); got != "alice_at_example.com" {
		t.Errorf("existing directory name changed: %q", got)
	}
}

func TestUserNamespacesIsolated_F6910(t *testing.T) {
	st := NewStorage(t.TempDir())
	if err := st.CreateAddressbook("bob@home", &Addressbook{ID: "ab"}); err != nil {
		t.Fatal(err)
	}
	if ab, _ := st.GetAddressbook("bob_at_home", "ab"); ab != nil {
		t.Fatal("bob_at_home sees bob@home's address book")
	}
}

func TestLegacyPercentDirMigrated_F6910(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "carddav", "100%", "ab")
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".addressbook.json"), []byte(`{"id":"ab"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewStorage(root)
	if ab, err := st.GetAddressbook("100%", "ab"); err != nil || ab == nil {
		t.Fatalf("legacy data unreachable: %v %v", ab, err)
	}
}

func TestPurgeUserDoesNotTouchCollidingUser_F6910(t *testing.T) {
	st := NewStorage(t.TempDir())
	_ = st.CreateAddressbook("a@b", &Addressbook{ID: "x"})
	_ = st.CreateAddressbook("a_at_b", &Addressbook{ID: "y"})
	if err := st.PurgeUser("a_at_b"); err != nil {
		t.Fatal(err)
	}
	if ab, _ := st.GetAddressbook("a@b", "x"); ab == nil {
		t.Fatal("purging a_at_b removed a@b's data")
	}
	if ab, _ := st.GetAddressbook("a_at_b", "y"); ab != nil {
		t.Fatal("a_at_b data survived purge")
	}
}

// F6911: Depth handling.
func TestPropfindDepth_F6911(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "PROPFIND", "/dav/addressbooks/", "", map[string]string{"Depth": "infinity"})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "propfind-finite-depth") {
		t.Fatalf("infinity: %d %s", w.Code, w.Body.String())
	}
	if w := discDo(s, "PROPFIND", "/dav/addressbooks/", "", map[string]string{"Depth": "garbage"}); w.Code != http.StatusBadRequest {
		t.Fatalf("garbage: %d", w.Code)
	}
	for _, d := range []string{"0", "1", ""} {
		h := map[string]string{}
		if d != "" {
			h["Depth"] = d
		}
		if w := discDo(s, "PROPFIND", "/dav/addressbooks/", "", h); w.Code != http.StatusMultiStatus {
			t.Fatalf("depth %q: %d", d, w.Code)
		}
	}
}

// F6912: class 2 (locking) must not be advertised without LOCK.
func TestOptionsNoClass2_F6912(t *testing.T) {
	s := discSetup(t)
	w := discDo(s, "OPTIONS", "/dav/", "", nil)
	if dav := w.Header().Get("DAV"); strings.Contains(dav, "2") || !strings.Contains(dav, "addressbook") {
		t.Fatalf("DAV header %q", dav)
	}
}
