package caldav

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserKeyInjective_R134(t *testing.T) {
	names := []string{"a@b", "a_at_b", "_at_", "@", "a@@b", "_at@", "@at_", "a%b", "a%5Fat%5Fb", "a%40b", ".", "x", "x_y", "bob@home", "bob_at_home", "%", "é@x", "%C3%A9@x"}
	seen := map[string]string{}
	for _, n := range names {
		k := userKey(n)
		if prev, dup := seen[k]; dup {
			t.Errorf("collision: %q and %q -> %q", prev, n, k)
		}
		seen[k] = n
		if k == "." || k == ".." || k == "" || filepath.Base(k) != k {
			t.Errorf("%q -> unsafe key %q", n, k)
		}
	}
	// Existing data directories keep their names.
	for n, want := range map[string]string{"alice@example.com": "alice_at_example.com", "john_doe": "john_doe"} {
		if got := userKey(n); got != want {
			t.Errorf("legacy key changed: %q -> %q want %q", n, got, want)
		}
	}
}

func TestUserNamespacesIsolated_R134(t *testing.T) {
	st := NewStorage(t.TempDir())
	if err := st.CreateCalendar("bob@home", &Calendar{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if cal, _ := st.GetCalendar("bob_at_home", "c"); cal != nil {
		t.Fatal("user bob_at_home can see bob@home's calendar")
	}
	if cals, _ := st.GetCalendars("bob_at_home"); len(cals) != 0 {
		t.Fatal("namespace shared")
	}
	// User "." must not alias the storage root.
	if st.userDir(".") == st.dataDir {
		t.Fatal(`user "." aliases data root`)
	}
}

func TestLegacyPercentDirMigrated_R134(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "caldav", "100%", "c")
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".calendar.json"), []byte(`{"id":"c"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewStorage(root)
	if cal, err := st.GetCalendar("100%", "c"); err != nil || cal == nil {
		t.Fatalf("legacy data not reachable after migration: %v %v", cal, err)
	}
}
