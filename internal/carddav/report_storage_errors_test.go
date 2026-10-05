package carddav

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddressbookReportValidControl(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateAddressbook("u", &Addressbook{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveContact("u", "c", &Contact{UID: "good"}, "BEGIN:VCARD\nUID:good\nFN:Control\nEND:VCARD"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/addressbooks/c", strings.NewReader("<addressbook-query/>")), "u")
	if w.Code != http.StatusMultiStatus || !strings.Contains(w.Body.String(), "good") {
		t.Fatalf("CONTROL invalid: %d %s", w.Code, w.Body.String())
	}
}

func TestAddressbookReportReadFailure(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateAddressbook("u", &Addressbook{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveContact("u", "c", &Contact{UID: "good"}, "BEGIN:VCARD\nUID:good\nFN:Control\nEND:VCARD"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.storage.dataDir, "u", "c")
	if err := os.Symlink(dir, filepath.Join(dir, "bad.vcf")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/addressbooks/c", strings.NewReader("<addressbook-query/>")), "u")
	t.Logf("EXPECTED: storage failure HTTP500 ACTUAL: status=%d body=%s\n", w.Code, w.Body.String())
	if w.Code != http.StatusInternalServerError {
		t.Error("REPORT storage failure hidden as success")
	}
}

func TestAddressbookReportEmptyCollection(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateAddressbook("u", &Addressbook{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/addressbooks/c", strings.NewReader("<addressbook-query/>")), "u")
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("empty collection: %d %s", w.Code, w.Body.String())
	}
}

func TestAddressbookReportCompatibility(t *testing.T) {
	s := NewServer(t.TempDir(), slog.Default())
	if err := s.storage.CreateAddressbook("u", &Addressbook{ID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := s.storage.SaveContact("u", "c", &Contact{UID: "good"}, "BEGIN:VCARD\nUID:good\nFN:Control\nEND:VCARD"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleReport(w, httptest.NewRequest("REPORT", "/dav/addressbooks/c", strings.NewReader("<broken")), "u")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid XML: %d", w.Code)
	}
}
