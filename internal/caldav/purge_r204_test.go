package caldav

import (
	"os"
	"path/filepath"
	"testing"
)

// F6810: account deletion must remove calendars (any letter-case variant).
func TestR204_PurgeUser(t *testing.T) {
	dir := t.TempDir()
	st := NewStorage(dir)
	for _, u := range []string{"alice@x.test", "Alice@x.test", "bob@x.test"} {
		if err := st.CreateCalendar(u, &Calendar{ID: "c1", Name: "c"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PurgeUser("alice@x.test"); err != nil {
		t.Fatal(err)
	}
	if cals, _ := st.GetCalendars("alice@x.test"); len(cals) != 0 {
		t.Fatalf("calendars survive: %v", cals)
	}
	if _, err := os.Stat(filepath.Join(dir, "caldav", "Alice_at_x.test")); err == nil {
		t.Fatal("case variant survives")
	}
	if cals, _ := st.GetCalendars("bob@x.test"); len(cals) != 1 {
		t.Fatal("control user purged")
	}
	if err := st.PurgeUser("alice@x.test"); err != nil { // idempotent
		t.Fatal(err)
	}
	if err := st.PurgeUser("../bob@x.test"); err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestR204_CanonicalUser(t *testing.T) {
	if got := canonicalUser(" Alice@X.Test. "); got != "alice@x.test" {
		t.Fatalf("canonicalUser: %q", got)
	}
}
