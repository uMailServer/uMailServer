package carddav

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetContactsValidControl(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContact("u", "c", &Contact{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetContacts("u", "c")
	if err != nil || len(xs) != 1 || xs[0] != "data" {
		t.Fatalf("CONTROL invalid: %v %v", xs, err)
	}
}

func TestGetContactsReadFailure(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContact("u", "c", &Contact{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.dataDir, "u", "c")
	if err := os.Symlink(dir, filepath.Join(dir, "bad.vcf")); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetContacts("u", "c")
	t.Logf("EXPECTED: record read error ACTUAL: rows=%d error=%v\n", len(xs), err)
	if err == nil {
		t.Error("record read error silently omitted")
	}
}

func TestGetContactsEmptyCollection(t *testing.T) {
	s := NewStorage(t.TempDir())
	xs, err := s.GetContacts("u", "missing")
	if err != nil || len(xs) != 0 {
		t.Fatalf("missing collection: %v %v", xs, err)
	}
}

func TestGetContactsCompatibility(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContact("u", "c", &Contact{UID: "good"}, "data"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.dataDir, "u", "c")
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("ignore"), 0600); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetContacts("u", "c")
	if err != nil || len(xs) != 1 {
		t.Fatalf("unrelated file: %v %v", xs, err)
	}
}
