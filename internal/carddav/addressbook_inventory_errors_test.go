package carddav

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetAddressbooksValidControl(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetAddressbooks("u")
	if err != nil || len(xs) != 1 {
		t.Fatalf("CONTROL invalid: %v %v", xs, err)
	}
}

func TestGetAddressbooksReadFailure(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dataDir, "u", "c", ".addressbook.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetAddressbooks("u")
	t.Logf("EXPECTED: metadata read error ACTUAL: rows=%d error=%v\n", len(xs), err)
	if err == nil {
		t.Error("corrupt metadata silently omitted")
	}
}

func TestGetAddressbooksEmptyCollection(t *testing.T) {
	s := NewStorage(t.TempDir())
	xs, err := s.GetAddressbooks("u")
	if err != nil || len(xs) != 0 {
		t.Fatalf("missing user: %v %v", xs, err)
	}
}

func TestGetAddressbooksCompatibility(t *testing.T) {
	s := NewStorage(t.TempDir())
	if err := s.CreateAddressbook("u", &Addressbook{ID: "c", Name: "Control"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.dataDir, "u", "orphan"), 0750); err != nil {
		t.Fatal(err)
	}
	xs, err := s.GetAddressbooks("u")
	if err != nil || len(xs) != 1 {
		t.Fatalf("orphan folder compatibility: %v %v", xs, err)
	}
}
