package caldav

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCalendarsValidControl(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetCalendars("u")
	if err != nil || len(xs) != 1 {
		t.Fatalf("CONTROL invalid: %v %v", xs, err)
	}
}

func TestGetCalendarsReadFailure(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dataDir, "u", "c", ".calendar.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetCalendars("u")
	t.Logf("EXPECTED: metadata read error ACTUAL: rows=%d error=%v\n", len(xs), err)
	if err == nil {
		t.Error("corrupt metadata silently omitted")
	}
}

func TestGetCalendarsEmptyCollection(t *testing.T) {
	s := NewStorage(t.TempDir())
	xs, err := s.GetCalendars("u")
	if err != nil || len(xs) != 0 {
		t.Fatalf("missing user: %v %v", xs, err)
	}
}

func TestGetCalendarsCompatibility(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.dataDir, "u", "orphan"), 0750); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetCalendars("u")
	if err != nil || len(xs) != 1 {
		t.Fatalf("orphan folder compatibility: %v %v", xs, err)
	}
}
