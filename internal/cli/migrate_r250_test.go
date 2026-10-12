package cli

// R250 regression tests for internal/cli/migrate.go.
//
// F7270: mboxrd ">From " escapes were stored verbatim; the importer must
//        unescape one level of ">" on body lines.
// F7271: imported messages bypassed per-account quota accounting (the
//        delivery path charges db quota); imports now charge it and refuse
//        when the quota is exhausted.
// F7272: MBOX messages that failed to import still produced exit status 0.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db"
	"github.com/umailserver/umailserver/internal/storage"
)

func r250Setup(t *testing.T) (*MigrationManager, string, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.CreateDomain(&db.DomainData{Name: "example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(dir, "mail")
	ms, err := storage.NewMessageStore(base)
	if err != nil {
		t.Fatal(err)
	}
	return NewMigrationManager(d, ms, nil), base, d
}

func TestR250F7270MboxUnescapesFromLines(t *testing.T) {
	mm, base, _ := r250Setup(t)
	f := filepath.Join(t.TempDir(), "bob@example.com.mbox")
	body := "From a@b Mon Jan  1 00:00:00 2024\nSubject: x\n\n>From here\n>>From there\n> From not\nend\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mm.MigrateFromMBOX(f); err != nil {
		t.Fatal(err)
	}
	got := mig69Stored(t, base, "bob@example.com")
	if len(got) != 1 {
		t.Fatalf("stored %d", len(got))
	}
	want := "Subject: x\n\nFrom here\n>From there\n> From not\nend\n"
	if got[0] != want {
		t.Fatalf("got %q want %q", got[0], want)
	}
}

func TestR250F7271ImportChargesQuota(t *testing.T) {
	mm, _, d := r250Setup(t)
	if err := d.CreateAccount(&db.AccountData{Email: "bob@example.com", LocalPart: "bob", Domain: "example.com", IsActive: true, QuotaLimit: 150}); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "bob@example.com.mbox")
	m1 := "From a@b x\nSubject: one\n\n" + strings.Repeat("a", 60) + "\n"
	m2 := "From a@b x\nSubject: two\n\n" + strings.Repeat("b", 120) + "\n"
	if err := os.WriteFile(f, []byte(m1+m2), 0o600); err != nil {
		t.Fatal(err)
	}
	err := mm.MigrateFromMBOX(f)
	acc, _ := d.GetAccount("example.com", "bob")
	if acc.QuotaUsed == 0 {
		t.Fatalf("quota not charged")
	}
	if err == nil { // F7272: the refused message must surface as an error
		t.Fatalf("expected error for over-quota message")
	}
	if acc.QuotaUsed > 150 {
		t.Fatalf("quota exceeded: %d", acc.QuotaUsed)
	}
}

func TestR250F7272MboxFailureReturnsError(t *testing.T) {
	mm, _, _ := r250Setup(t)
	f := filepath.Join(t.TempDir(), "..mbox") // folder "." is an invalid user
	if err := os.WriteFile(f, []byte("From a@b x\nSubject: s\n\nhi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mm.MigrateFromMBOX(f); err == nil {
		t.Fatal("expected error when no message could be imported")
	}
}

// F7273: an IMAP migration without a target user stored nothing yet exited 0.
func TestR250F7273IMAPRequiresTarget(t *testing.T) {
	mm, _, _ := r250Setup(t)
	err := mm.MigrateFromIMAP(MigrateOptions{SourceURL: "imaps://127.0.0.1:1", Username: "u", Password: "p"})
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("got %v", err)
	}
}
