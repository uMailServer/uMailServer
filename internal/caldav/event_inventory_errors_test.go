package caldav

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetEventsValidControl(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetEvents("u", "c")
	if err != nil || len(xs) != 1 || xs[0] != "data" {
		t.Fatalf("CONTROL invalid: %v %v", xs, err)
	}
}

func TestGetEventsReadFailure(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.dataDir, "u", "c")
	if err := os.Symlink(dir, filepath.Join(dir, "bad.ics")); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetEvents("u", "c")
	t.Logf("EXPECTED: record read error ACTUAL: rows=%d error=%v\n", len(xs), err)
	if err == nil {
		t.Error("record read error silently omitted")
	}
}

func TestGetEventsEmptyCollection(t *testing.T) {
	s := NewStorage(t.TempDir())
	xs, err := s.GetEvents("u", "missing")
	if err != nil || len(xs) != 0 {
		t.Fatalf("missing collection: %v %v", xs, err)
	}
}

func TestGetEventsCompatibility(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateCalendar("u", &Calendar{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEvent("u", "c", &CalendarEvent{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.dataDir, "u", "c")
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetEvents("u", "c")
	if err != nil || len(xs) != 1 {
		t.Fatalf("unrelated file: %v %v", xs, err)
	}
}
