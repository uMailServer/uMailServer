package server

import (
	"path/filepath"
	"testing"

	"github.com/umailserver/umailserver/internal/carddav"
)

// The webmail contacts API and the CardDAV server must use the same address
// books, whether or not the CardDAV listener runs.
func TestContactStorageSharedWithCardDAV(t *testing.T) {
	srv := junkRTServer(t, false)
	webmail := srv.contactStorage()
	if err := webmail.SaveContact("alice@test.com", "contacts", &carddav.Contact{UID: "c1"},
		carddav.NewCardVCard(carddav.Card{UID: "c1", FullName: "Bob"})); err != nil {
		t.Fatal(err)
	}
	// What the CardDAV server reads when it starts.
	dav := carddav.NewServer(filepath.Join(srv.config.Server.DataDir, "carddav"), nil)
	if vcf, err := dav.Storage().GetContact("alice@test.com", "contacts", "c1"); err != nil || vcf == "" {
		t.Fatalf("CardDAV does not see the webmail's contact: %q %v", vcf, err)
	}

	srv.carddavServer = dav
	if srv.contactStorage() != dav.Storage() {
		t.Fatal("contacts API does not share the running CardDAV server's storage")
	}
}
