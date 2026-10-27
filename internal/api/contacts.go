package api

import (
	"errors"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/umailserver/umailserver/internal/carddav"
)

// The webmail contacts API edits the same address books CardDAV clients
// sync: a contact saved here appears on the user's phone and vice versa.

// ContactStore is the CardDAV storage the contacts API reads and writes.
type ContactStore interface {
	GetAddressbooks(user string) ([]*carddav.Addressbook, error)
	CreateAddressbook(user string, ab *carddav.Addressbook) error
	GetContacts(user, addressbookID string) ([]string, error)
	GetContact(user, addressbookID, uid string) (string, error)
	SaveContact(user, addressbookID string, contact *carddav.Contact, vcard string) error
	UpdateContact(user, addressbookID, uid string, update func(old string) (string, error)) error
	DeleteContact(user, addressbookID, uid string) error
}

// defaultAddressbookID is created for a user who has no address book yet.
const defaultAddressbookID = "contacts"

// Limits on one contact's fields.
const (
	maxContactValues    = 20
	maxContactFieldLen  = 256
	maxContactNoteLen   = 4096
	maxContactBodyBytes = 64 << 10
)

// ContactResponse is one contact in API responses.
type ContactResponse struct {
	carddav.Card
	Addressbook string `json:"addressbook"`
}

// contactRequest is the body of POST /api/v1/contacts and
// PUT /api/v1/contacts/{addressbook}/{uid}.
type contactRequest struct {
	Name         string   `json:"name"`
	Emails       []string `json:"emails"`
	Phones       []string `json:"phones"`
	Organization string   `json:"organization"`
	Note         string   `json:"note"`
}

// SetContactStore installs the address book storage; without it the
// contacts endpoints answer 503.
func (s *Server) SetContactStore(store ContactStore) {
	s.contactStore = store
}

// contactUser returns the authenticated user's CardDAV namespace.
func (s *Server) contactUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	user, ok := r.Context().Value("user").(string)
	if !ok || user == "" {
		s.sendError(w, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	if s.contactStore == nil {
		s.sendError(w, http.StatusServiceUnavailable, "contacts not available")
		return "", false
	}
	return carddav.CanonicalUser(user), true
}

// handleContacts serves GET (list, ?q= filters) and POST (create) on
// /api/v1/contacts.
func (s *Server) handleContacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, ok := s.contactUser(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		s.createContact(w, r, user)
		return
	}

	books, err := s.contactStore.GetAddressbooks(user)
	if err != nil {
		s.logger.Error("Failed to list address books", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to list contacts")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	contacts := []ContactResponse{}
	for _, ab := range books {
		cards, err := s.contactStore.GetContacts(user, ab.ID)
		if err != nil {
			s.logger.Error("Failed to read contacts", "error", err, "user", user, "addressbook", ab.ID)
			s.sendError(w, http.StatusInternalServerError, "failed to list contacts")
			return
		}
		for _, data := range cards {
			c := carddav.ParseCard(data)
			if c.UID == "" || (q != "" && !contactMatches(c, q)) {
				continue
			}
			contacts = append(contacts, ContactResponse{Card: c, Addressbook: ab.ID})
		}
	}
	sort.SliceStable(contacts, func(i, j int) bool {
		return strings.ToLower(contactSortKey(contacts[i].Card)) < strings.ToLower(contactSortKey(contacts[j].Card))
	})
	s.sendJSON(w, http.StatusOK, map[string]interface{}{"contacts": contacts})
}

func contactSortKey(c carddav.Card) string {
	if c.FullName != "" {
		return c.FullName
	}
	if len(c.Emails) > 0 {
		return c.Emails[0]
	}
	return c.UID
}

// contactMatches reports whether the lowercase query occurs in the
// contact's name, organization, emails or phones.
func contactMatches(c carddav.Card, q string) bool {
	fields := append([]string{c.FullName, c.Organization}, c.Emails...)
	fields = append(fields, c.Phones...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// handleContactPath serves PUT and DELETE on
// /api/v1/contacts/{addressbook}/{uid}.
func (s *Server) handleContactPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "PUT, DELETE")
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, ok := s.contactUser(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/contacts/")
	abID, uid, found := strings.Cut(rest, "/")
	if !found || abID == "" || uid == "" || strings.Contains(uid, "/") {
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}
	existing, err := s.contactStore.GetContact(user, abID, uid)
	if err != nil || existing == "" {
		// Invalid identifiers fail validation in the store: not found either way.
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}
	if ro, err := s.addressbookReadOnly(user, abID); err != nil || ro {
		s.sendError(w, http.StatusForbidden, "address book is read-only")
		return
	}

	if r.Method == http.MethodDelete {
		if err := s.contactStore.DeleteContact(user, abID, uid); err != nil {
			s.logger.Error("Failed to delete contact", "error", err, "user", user)
			s.sendError(w, http.StatusInternalServerError, "failed to delete contact")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	card, ok := s.readContactRequest(w, r)
	if !ok {
		return
	}
	card.UID = uid
	err = s.contactStore.UpdateContact(user, abID, uid, func(old string) (string, error) {
		return carddav.MergeCard(old, card), nil
	})
	if errors.Is(err, carddav.ErrContactNotFound) {
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}
	if err != nil {
		s.logger.Error("Failed to save contact", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to save contact")
		return
	}
	s.sendJSON(w, http.StatusOK, ContactResponse{Card: card, Addressbook: abID})
}

// createContact adds a contact to the user's first writable address book,
// creating a default one when the user has none.
func (s *Server) createContact(w http.ResponseWriter, r *http.Request, user string) {
	card, ok := s.readContactRequest(w, r)
	if !ok {
		return
	}
	abID, err := s.writableAddressbook(user)
	if err != nil {
		s.logger.Error("Failed to prepare address book", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to save contact")
		return
	}
	card.UID = uuid.NewString()
	if err := s.saveCard(user, abID, card, carddav.NewCardVCard(card)); err != nil {
		s.logger.Error("Failed to save contact", "error", err, "user", user)
		s.sendError(w, http.StatusInternalServerError, "failed to save contact")
		return
	}
	s.sendJSON(w, http.StatusCreated, ContactResponse{Card: card, Addressbook: abID})
}

func (s *Server) saveCard(user, abID string, card carddav.Card, vcard string) error {
	now := time.Now()
	return s.contactStore.SaveContact(user, abID, &carddav.Contact{UID: card.UID, FullName: card.FullName, Modified: now}, vcard)
}

func (s *Server) addressbookReadOnly(user, abID string) (bool, error) {
	books, err := s.contactStore.GetAddressbooks(user)
	if err != nil {
		return false, err
	}
	for _, ab := range books {
		if ab.ID == abID {
			return ab.ReadOnly, nil
		}
	}
	return false, nil
}

func (s *Server) writableAddressbook(user string) (string, error) {
	books, err := s.contactStore.GetAddressbooks(user)
	if err != nil {
		return "", err
	}
	sort.Slice(books, func(i, j int) bool { return books[i].ID < books[j].ID })
	for _, ab := range books {
		if ab.ID == defaultAddressbookID && !ab.ReadOnly {
			return ab.ID, nil
		}
	}
	id := defaultAddressbookID
	for _, ab := range books {
		if !ab.ReadOnly {
			return ab.ID, nil
		}
		if ab.ID == defaultAddressbookID {
			// Creating it again would overwrite its metadata and clear
			// ReadOnly; use a new book instead.
			id = uuid.NewString()
		}
	}
	now := time.Now()
	err = s.contactStore.CreateAddressbook(user, &carddav.Addressbook{
		ID: id, Name: "Contacts", Created: now, Modified: now,
	})
	return id, err
}

// readContactRequest decodes and validates a contact body.
func (s *Server) readContactRequest(w http.ResponseWriter, r *http.Request) (carddav.Card, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxContactBodyBytes)
	var req contactRequest
	if err := decodeJSON(r, &req); err != nil {
		s.sendError(w, jsonErrStatus(err), jsonErrMsg(err, "invalid request body"))
		return carddav.Card{}, false
	}
	card, err := validateContact(req)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, err.Error())
		return carddav.Card{}, false
	}
	return card, true
}

func validateContact(req contactRequest) (carddav.Card, error) {
	c := carddav.Card{
		FullName:     strings.TrimSpace(req.Name),
		Organization: strings.TrimSpace(req.Organization),
		Note:         strings.TrimSpace(req.Note),
		Emails:       []string{},
		Phones:       []string{},
	}
	if len(req.Emails) > maxContactValues || len(req.Phones) > maxContactValues {
		return c, errors.New("too many emails or phone numbers")
	}
	for _, e := range req.Emails {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		addr, err := mail.ParseAddress(e)
		if err != nil || addr.Address != e || len(e) > maxContactFieldLen {
			return c, errors.New("invalid email address: " + e)
		}
		c.Emails = append(c.Emails, e)
	}
	for _, p := range req.Phones {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > maxContactFieldLen || strings.ContainsAny(p, "\r\n") {
			return c, errors.New("invalid phone number")
		}
		c.Phones = append(c.Phones, p)
	}
	if c.FullName == "" && len(c.Emails) == 0 {
		return c, errors.New("a name or an email address is required")
	}
	if len(c.FullName) > maxContactFieldLen || len(c.Organization) > maxContactFieldLen ||
		len(c.Note) > maxContactNoteLen {
		return c, errors.New("contact field too long")
	}
	for _, v := range []string{c.FullName, c.Organization, c.Note} {
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return c, errors.New("contact fields must be valid UTF-8 text")
		}
	}
	if strings.ContainsAny(c.FullName+c.Organization, "\r\n") {
		return c, errors.New("name and organization must be one line")
	}
	return c, nil
}
