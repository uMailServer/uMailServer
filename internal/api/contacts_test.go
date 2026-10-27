package api

// The webmail had no contacts endpoint. /api/v1/contacts now edits the
// user's CardDAV address books.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/carddav"
	"github.com/umailserver/umailserver/internal/db"
)

func contactsServer(t *testing.T, withStore bool) (*Server, *carddav.Storage) {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatalf("INVALID open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := NewServer(database, nil, Config{JWTSecret: adminMetricsSecret, TokenExpiry: time.Hour})
	seedSessionAccount(t, database, "x@ex.com", true)
	seedSessionAccount(t, database, "y@ex.com", false)
	var store *carddav.Storage
	if withStore {
		store = carddav.NewStorage(t.TempDir())
		s.SetContactStore(store)
	}
	return s, store
}

// contactsDo sends a request as y@ex.com (or x@ex.com when asX).
func contactsDo(t *testing.T, h http.Handler, method, path, body string, asX bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json") // the webmail client always sends it
	req.Header.Set("Authorization", "Bearer "+adminMetricsToken(t, asX))
	req.RemoteAddr = "127.0.0.1:4000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func contactsList(t *testing.T, h http.Handler, query string, asX bool) []ContactResponse {
	t.Helper()
	rec := contactsDo(t, h, http.MethodGet, "/api/v1/contacts"+query, "", asX)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body)
	}
	var out struct {
		Contacts []ContactResponse `json:"contacts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Contacts == nil {
		t.Fatalf("list body %s: %v", rec.Body, err)
	}
	return out.Contacts
}

func TestContactsCRUD(t *testing.T) {
	s, store := contactsServer(t, true)
	if got := contactsList(t, s, "", false); len(got) != 0 {
		t.Fatalf("new user has contacts: %+v", got)
	}

	rec := contactsDo(t, s, http.MethodPost, "/api/v1/contacts",
		`{"name":"Jane Doe","emails":["jane@example.com"],"phones":["+1 555 0100"],"organization":"Acme"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body)
	}
	var created ContactResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.UID == "" || created.Addressbook != defaultAddressbookID {
		t.Fatalf("created %+v", created)
	}
	// Stored as a vCard a CardDAV client can read.
	vcf, _ := store.GetContact("y@ex.com", defaultAddressbookID, created.UID)
	if c := carddav.ParseCard(vcf); c.FullName != "Jane Doe" || len(c.Emails) != 1 || !strings.HasPrefix(vcf, "BEGIN:VCARD") {
		t.Fatalf("stored vCard:\n%s", vcf)
	}

	rec = contactsDo(t, s, http.MethodPost, "/api/v1/contacts", `{"emails":["bob@other.example"]}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create email-only status=%d body=%s", rec.Code, rec.Body)
	}
	list := contactsList(t, s, "", false)
	if len(list) != 2 || list[0].Emails[0] != "bob@other.example" || list[1].FullName != "Jane Doe" {
		t.Fatalf("list (sorted by name/email): %+v", list)
	}
	if got := contactsList(t, s, "?q=ACME", false); len(got) != 1 || got[0].UID != created.UID {
		t.Fatalf("search by organization: %+v", got)
	}

	path := "/api/v1/contacts/" + defaultAddressbookID + "/" + created.UID
	rec = contactsDo(t, s, http.MethodPut, path, `{"name":"Jane Q. Doe","emails":["jq@example.com"]}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body)
	}
	vcf, _ = store.GetContact("y@ex.com", defaultAddressbookID, created.UID)
	if c := carddav.ParseCard(vcf); c.FullName != "Jane Q. Doe" || c.Emails[0] != "jq@example.com" || len(c.Phones) != 0 {
		t.Fatalf("updated vCard:\n%s", vcf)
	}

	if rec := contactsDo(t, s, http.MethodDelete, path, "", false); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body)
	}
	if rec := contactsDo(t, s, http.MethodDelete, path, "", false); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status=%d", rec.Code)
	}
	if got := contactsList(t, s, "", false); len(got) != 1 {
		t.Fatalf("after delete: %+v", got)
	}
}

// A contact a CardDAV client stored keeps its other properties when edited
// in the webmail.
func TestContactsEditKeepsCardDAVProperties(t *testing.T) {
	s, store := contactsServer(t, true)
	now := time.Now()
	if err := store.CreateAddressbook("y@ex.com", &carddav.Addressbook{ID: "phone", Name: "Phone", Created: now, Modified: now}); err != nil {
		t.Fatal(err)
	}
	vcf := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:abc\r\nN:Roe;Rick;;;\r\nFN:Rick Roe\r\n" +
		"PHOTO;ENCODING=b;TYPE=JPEG:AAAA\r\nBDAY:1980-05-06\r\nEND:VCARD\r\n"
	if err := store.SaveContact("y@ex.com", "phone", &carddav.Contact{UID: "abc"}, vcf); err != nil {
		t.Fatal(err)
	}
	if got := contactsList(t, s, "", false); len(got) != 1 || got[0].FullName != "Rick Roe" || got[0].Addressbook != "phone" {
		t.Fatalf("CardDAV contact not listed: %+v", got)
	}
	rec := contactsDo(t, s, http.MethodPut, "/api/v1/contacts/phone/abc", `{"name":"Rick Roe","emails":["rick@example.com"]}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body)
	}
	saved, _ := store.GetContact("y@ex.com", "phone", "abc")
	for _, keep := range []string{"PHOTO;ENCODING=b;TYPE=JPEG:AAAA", "BDAY:1980-05-06", "N:Roe;Rick;;;", "rick@example.com"} {
		if !strings.Contains(saved, keep) {
			t.Errorf("edited vCard lost %q:\n%s", keep, saved)
		}
	}
}

func TestContactsIsolationAndValidation(t *testing.T) {
	s, store := contactsServer(t, true)
	rec := contactsDo(t, s, http.MethodPost, "/api/v1/contacts", `{"name":"Secret","emails":["s@example.com"]}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("INVALID: create as x status=%d", rec.Code)
	}
	var xc ContactResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &xc)
	path := "/api/v1/contacts/" + xc.Addressbook + "/" + xc.UID

	if got := contactsList(t, s, "", false); len(got) != 0 {
		t.Fatalf("y sees x's contacts: %+v", got)
	}
	if rec := contactsDo(t, s, http.MethodPut, path, `{"name":"Hijack"}`, false); rec.Code != http.StatusNotFound {
		t.Fatalf("y edits x's contact: status=%d", rec.Code)
	}
	if rec := contactsDo(t, s, http.MethodDelete, path, "", false); rec.Code != http.StatusNotFound {
		t.Fatalf("y deletes x's contact: status=%d", rec.Code)
	}
	if vcf, _ := store.GetContact("x@ex.com", xc.Addressbook, xc.UID); !strings.Contains(vcf, "FN:Secret") {
		t.Fatalf("x's contact changed:\n%s", vcf)
	}
	for _, p := range []string{"/api/v1/contacts/../x@ex.com/" + xc.UID, "/api/v1/contacts/" + xc.Addressbook + "/..%2F..%2Fx", "/api/v1/contacts/onlyone"} {
		if rec := contactsDo(t, s, http.MethodDelete, p, "", false); rec.Code == http.StatusNoContent || rec.Code == http.StatusOK {
			t.Errorf("DELETE %s status=%d", p, rec.Code)
		}
	}

	for _, body := range []string{
		`{}`,
		`{"name":"  "}`,
		`{"name":"A","emails":["not an address"]}`,
		`{"name":"A","emails":["Jane <jane@example.com>"]}`,
		`{"name":"line\nbreak"}`,
		`{"name":"` + strings.Repeat("a", 300) + `"}`,
	} {
		if rec := contactsDo(t, s, http.MethodPost, "/api/v1/contacts", body, false); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s: status=%d, want 400", body, rec.Code)
		}
	}
}

func TestContactsUnavailableWithoutStore(t *testing.T) {
	s, _ := contactsServer(t, false)
	if rec := contactsDo(t, s, http.MethodGet, "/api/v1/contacts", "", false); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rec.Code)
	}
}
