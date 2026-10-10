// Package carddav provides CardDAV (RFC 6352) address book synchronization support
package carddav

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/umailserver/umailserver/internal/tracing"
)

// maxRequestBodyBytes caps every CardDAV request body (F5091). vCards and
// DAV XML requests are small; without a cap an authenticated client could
// make the server buffer an arbitrarily large body in memory.
const maxRequestBodyBytes = 10 << 20

// Server represents a CardDAV server
type Server struct {
	logger          *slog.Logger
	authFunc        func(username, password string) (bool, error)
	dataDir         string
	storage         *Storage
	tracingProvider *tracing.Provider
	// writeMu serializes conditional writes so an If-Match/If-None-Match
	// evaluation and the write it guards are atomic (F5089).
	writeMu sync.Mutex
}

// SetTracingProvider attaches an OpenTelemetry tracing provider so each
// CardDAV request emits a carddav.<METHOD> span. Nil disables tracing.
func (s *Server) SetTracingProvider(provider *tracing.Provider) {
	s.tracingProvider = provider
}

// NewServer creates a new CardDAV server
func NewServer(dataDir string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		logger:  logger,
		dataDir: dataDir,
		storage: NewStorage(dataDir),
	}
}

// SetAuthFunc sets the authentication function
func (s *Server) SetAuthFunc(fn func(username, password string) (bool, error)) {
	s.authFunc = fn
}

// ServeHTTP implements the http.Handler interface, wrapping the actual
// dispatch in a tracing span when a provider is configured.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tracing.HTTPMiddleware(s.tracingProvider, "carddav", http.HandlerFunc(s.handle)).ServeHTTP(w, r)
}

// handle does the auth+dispatch work; ServeHTTP wraps it in a tracing span.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// Authenticate request
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="CardDAV"`)
		s.sendError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if s.authFunc != nil {
		authenticated, err := s.authFunc(username, password)
		if err != nil || !authenticated {
			w.Header().Set("WWW-Authenticate", `Basic realm="CardDAV"`)
			s.sendError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
	}

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	}

	// Log request
	s.logger.Debug("CardDAV request",
		"method", r.Method,
		"path", r.URL.Path,
		"user", username,
	)

	// Route based on method
	switch r.Method {
	case "OPTIONS":
		s.handleOptions(w, r)
	case "PROPFIND":
		s.handlePropfind(w, r, username)
	case "REPORT":
		s.handleReport(w, r, username)
	case "PUT":
		s.handlePut(w, r, username)
	case "GET":
		s.handleGet(w, r, username)
	case "DELETE":
		s.handleDelete(w, r, username)
	case "MKCOL":
		s.handleMkCol(w, r, username)
	case "PROPPATCH":
		s.handleProppatch(w, r, username)
	case "MOVE":
		s.handleMove(w, r, username)
	case "COPY":
		s.handleCopy(w, r, username)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleOptions handles OPTIONS requests
func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "OPTIONS, GET, PUT, DELETE, PROPFIND, PROPPATCH, REPORT, MKCOL, MOVE, COPY")
	w.Header().Set("DAV", "1, 2, 3, addressbook")
	w.WriteHeader(http.StatusOK)
}

// readBody reads the request body, answering 413 when it exceeds
// maxRequestBodyBytes (F5091) and 400 on any other read failure.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.sendError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return nil, false
		}
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	return body, true
}

// etagListMatches reports whether a comma-separated If-Match/If-None-Match
// list names etag. Strong comparison (If-Match) never matches a weak entity
// tag; weak comparison (If-None-Match) ignores the W/ prefix (RFC 7232 §2.3.2).
func etagListMatches(list, etag string, weak bool) bool {
	for _, candidate := range strings.Split(list, ",") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "W/") {
			if !weak {
				continue
			}
			candidate = strings.TrimPrefix(candidate, "W/")
		}
		if candidate == etag {
			return true
		}
	}
	return false
}

// preconditionsHold evaluates If-Match and If-None-Match for a state-changing
// request against the target's current state (RFC 7232 §3.1, §3.2, §6). A
// false result must be answered with 412 and the method not performed (F5089).
func preconditionsHold(r *http.Request, exists bool, etag string) bool {
	if values, present := r.Header["If-Match"]; present {
		list := strings.Join(values, ",")
		if strings.TrimSpace(list) == "*" {
			if !exists {
				return false
			}
		} else if !exists || !etagListMatches(list, etag, false) {
			return false
		}
	}
	if values, present := r.Header["If-None-Match"]; present {
		list := strings.Join(values, ",")
		if strings.TrimSpace(list) == "*" {
			if exists {
				return false
			}
		} else if exists && etagListMatches(list, etag, true) {
			return false
		}
	}
	return true
}

// destinationPath parses the RFC 4918 §10.3 Destination header, which may be
// an absolute URI or an absolute path, into the decoded path below
// /dav/addressbooks/ (F5093). ok is false when the header is malformed or
// points outside the address book namespace.
func destinationPath(destination string) (string, bool) {
	u, err := url.Parse(destination)
	if err != nil {
		return "", false
	}
	const prefix = "/dav/addressbooks/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(u.Path, prefix), true
}

// handlePropfind handles PROPFIND requests
func (s *Server) handlePropfind(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	// Parse PROPFIND request
	var propfind Propfind
	if len(body) > 0 {
		if err := xml.Unmarshal(body, &propfind); err != nil {
			s.logger.Debug("Failed to parse PROPFIND", "error", err, "body", string(body))
			// Continue with empty propfind (allprop)
		}
	}

	// Determine depth
	depth := r.Header.Get("Depth")
	if depth == "" {
		depth = "1"
	}

	// Build response
	multistatus := &Multistatus{}

	// A request-URI below the home names one address book or one contact;
	// answer for that resource only (RFC 4918 §9.1). Previously such requests
	// returned nothing at Depth 0 and every address book otherwise (F5482).
	if rel := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/"); rel != r.URL.Path && rel != "" {
		if !s.propfindTarget(w, multistatus, username, rel, depth) {
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		output, _ := xml.MarshalIndent(multistatus, "", "  ")
		_, _ = w.Write([]byte(xml.Header))
		_, _ = w.Write(output)
		return
	}

	// Root principal
	if r.URL.Path == "/" || r.URL.Path == "/dav/" {
		multistatus.Responses = append(multistatus.Responses, s.buildPrincipalResponse(username))
	}

	// Address book home
	if r.URL.Path == "/" || r.URL.Path == "/dav/" || r.URL.Path == "/dav/addressbooks/" {
		multistatus.Responses = append(multistatus.Responses, s.buildAddressbookHomeResponse(username))
	}

	// Query address books
	if depth != "0" {
		addressbooks, err := s.storage.GetAddressbooks(username)
		if err != nil {
			s.logger.Error("Failed to query addressbooks", "error", err)
			s.sendError(w, http.StatusInternalServerError, "failed to query addressbooks")
			return
		}
		for _, ab := range addressbooks {
			multistatus.Responses = append(multistatus.Responses, s.buildAddressbookResponse(username, ab))

			// If depth is infinity or 1, include contacts
			if depth == "infinity" || depth == "1" {
				contacts, err := s.storage.GetContacts(username, ab.ID)
				if err != nil {
					// One unreadable contact file must not fail the whole
					// PROPFIND; skip this address book's contacts and keep
					// serving the accumulated multistatus (RFC 4918 §9.1).
					s.logger.Error("Failed to query contacts", "error", err)
					continue
				}
				for _, contact := range contacts {
					uid := s.extractUIDFromVCard(contact)
					if uid != "" {
						multistatus.Responses = append(multistatus.Responses, s.buildContactResponse(username, ab.ID, uid, contact))
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	output, _ := xml.MarshalIndent(multistatus, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

// propfindTarget appends the PROPFIND responses for a request-URI naming one
// address book (/dav/addressbooks/{id}[/]) or one contact
// (/dav/addressbooks/{id}/{uid}.vcf) of the authenticated user (F5482). When
// the target cannot be served it writes the error response and returns false.
func (s *Server) propfindTarget(w http.ResponseWriter, ms *Multistatus, username, rel, depth string) bool {
	parts := strings.SplitN(rel, "/", 2)
	addressbookID := parts[0]
	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil && !errors.Is(err, errInvalidID) {
		s.logger.Error("Failed to read addressbook", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to read addressbook")
		return false
	}
	if ab == nil {
		s.sendError(w, http.StatusNotFound, "address book not found")
		return false
	}

	if len(parts) == 2 && parts[1] != "" {
		contactUID := strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))
		vcardData, err := s.storage.GetContact(username, addressbookID, contactUID)
		if err != nil && !errors.Is(err, errInvalidID) {
			s.logger.Error("Failed to read contact", "error", err)
			s.sendError(w, http.StatusInternalServerError, "failed to read contact")
			return false
		}
		if vcardData == "" {
			s.sendError(w, http.StatusNotFound, "contact not found")
			return false
		}
		ms.Responses = append(ms.Responses, s.buildContactResponse(username, addressbookID, contactUID, vcardData))
		return true
	}

	ms.Responses = append(ms.Responses, s.buildAddressbookResponse(username, ab))
	if depth == "0" {
		return true
	}
	contacts, err := s.storage.GetContacts(username, addressbookID)
	if err != nil {
		s.logger.Error("Failed to query contacts", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to query contacts")
		return false
	}
	for _, contact := range contacts {
		if uid := s.extractUIDFromVCard(contact); uid != "" {
			ms.Responses = append(ms.Responses, s.buildContactResponse(username, addressbookID, uid, contact))
		}
	}
	return true
}

// handleReport handles REPORT requests: addressbook-query (filtered,
// F5570/F5571) and addressbook-multiget (F5572).
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	var query AddressbookQuery
	var multiget *AddressbookMultiget
	if reportRootName(body) == "addressbook-multiget" {
		multiget = &AddressbookMultiget{}
		if err := xml.Unmarshal(body, multiget); err != nil || len(multiget.Hrefs) == 0 {
			s.sendError(w, http.StatusBadRequest, "invalid addressbook multiget")
			return
		}
	} else if err := xml.Unmarshal(body, &query); err != nil {
		s.logger.Debug("Failed to parse REPORT", "error", err)
		s.sendError(w, http.StatusBadRequest, "invalid addressbook query")
		return
	}
	if query.Filter != nil {
		if err := query.Filter.validate(); err != nil {
			if errors.Is(err, errUnsupportedCollation) {
				s.sendPreconditionError(w, "supported-collation")
				return
			}
			s.sendError(w, http.StatusBadRequest, "invalid addressbook query filter")
			return
		}
	}

	// Build response
	multistatus := &Multistatus{}

	// Extract address book ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		addressbookID := parts[0]

		// Verify the address book belongs to this user
		ab, err := s.storage.GetAddressbook(username, addressbookID)
		if err != nil || ab == nil {
			s.sendError(w, http.StatusForbidden, "address book not found")
			return
		}

		if multiget != nil {
			for _, href := range multiget.Hrefs {
				multistatus.Responses = append(multistatus.Responses, s.multigetResponse(username, addressbookID, strings.TrimSpace(href)))
			}
		} else {
			contacts, err := s.storage.GetContacts(username, addressbookID)
			if err != nil {
				s.logger.Error("Failed to query addressbook contacts", "error", err)
				s.sendError(w, http.StatusInternalServerError, "failed to query addressbook contacts")
				return
			}
			for _, contact := range contacts {
				uid := s.extractUIDFromVCard(contact)
				if uid == "" {
					continue
				}
				if query.Filter != nil && !query.Filter.matches(parseVCardProps(contact)) {
					continue
				}
				multistatus.Responses = append(multistatus.Responses, s.buildContactResponse(username, addressbookID, uid, contact))
			}
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)

	output, _ := xml.MarshalIndent(multistatus, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

// reportRootName returns the local name of the REPORT body's root element,
// or "" when none can be read.
func reportRootName(body []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if start, ok := tok.(xml.StartElement); ok {
			return start.Name.Local
		}
	}
}

// multigetResponse answers one addressbook-multiget href (RFC 6352 §8.7).
// The href must name a contact in the request-URI's address book of the
// authenticated user; anything else gets a per-href error status rather than
// failing the whole report. The client's href is echoed so it can match the
// response to its request.
func (s *Server) multigetResponse(username, addressbookID, href string) Response {
	status := func(code int) Response {
		return Response{Href: href, Status: fmt.Sprintf("HTTP/1.1 %d %s", code, http.StatusText(code))}
	}
	u, err := url.Parse(href)
	if err != nil {
		return status(http.StatusBadRequest)
	}
	rest, ok := strings.CutPrefix(u.Path, "/dav/addressbooks/"+addressbookID+"/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return status(http.StatusNotFound)
	}
	contactUID := strings.TrimSuffix(rest, filepath.Ext(rest))
	vcardData, err := s.storage.GetContact(username, addressbookID, contactUID)
	if err != nil && !errors.Is(err, errInvalidID) {
		s.logger.Error("Failed to read contact", "error", err)
		return status(http.StatusInternalServerError)
	}
	if vcardData == "" {
		return status(http.StatusNotFound)
	}
	resp := s.buildContactResponse(username, addressbookID, contactUID, vcardData)
	resp.Href = href
	return resp
}

// sendPreconditionError answers a CardDAV precondition failure with 403 and
// a DAV:error body naming the precondition (RFC 4918 §16, RFC 6352 §8.6).
func (s *Server) sendPreconditionError(w http.ResponseWriter, precondition string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(xml.Header + `<D:error xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav"><C:` + precondition + `/></D:error>`))
}

// handlePut handles PUT requests for creating/updating contacts
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, username string) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	// Validate vCard data
	if !strings.Contains(string(body), "BEGIN:VCARD") {
		s.sendError(w, http.StatusUnsupportedMediaType, "invalid vcard data")
		return
	}

	// Extract address book ID and contact UID from URL path (same convention
	// as handleGet, including extension trimming).
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
		return
	}
	addressbookID := parts[0]
	urlUID := ""
	if len(parts) == 2 {
		urlUID = strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))
	}

	// Verify the address book belongs to this user
	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}

	// RFC 6352 §6.3.2: the request-URI names the resource, so its UID is
	// authoritative. A body UID that differs would store the contact at an
	// address the client cannot address back, so reject the mismatch. A
	// UID-less vCard adopts the URL UID (and is made self-describing) rather
	// than an unreachable random UUID.
	uid := s.extractUIDFromVCard(string(body))
	if uid == "" {
		if urlUID == "" {
			uid = uuid.New().String()
		} else {
			uid = urlUID
		}
		// Add UID to vCard if missing, after the BEGIN:VCARD line and with
		// that line's own terminator: an LF-only body previously got no UID
		// and was then skipped by PROPFIND/REPORT listings (F5480).
		body = insertVCardUID(body, uid)
	} else if urlUID != "" && uid != urlUID {
		s.sendError(w, http.StatusForbidden, "UID in request URL does not match UID in vCard data")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.storage.GetContact(username, addressbookID, uid)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid contact")
		return
	}
	if !preconditionsHold(r, existing != "", s.storage.GetETag(username, addressbookID, uid)) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
	}

	// Parse vCard to create contact object
	contact := &Contact{
		UID:      uid,
		Modified: time.Now(),
		Created:  time.Now(),
	}

	// Store the contact
	if err := s.storage.SaveContact(username, addressbookID, contact, string(body)); err != nil {
		s.logger.Error("Failed to save contact", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to save contact")
		return
	}

	w.Header().Set("ETag", s.storage.GetETag(username, addressbookID, uid))
	w.WriteHeader(http.StatusCreated)
}

// handleGet handles GET requests for retrieving contacts
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, username string) {
	// Extract address book ID and contact UID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}

	addressbookID := parts[0]
	contactUID := strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))

	// Verify the address book belongs to this user
	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}

	// Retrieve contact from storage
	vcardData, err := s.storage.GetContact(username, addressbookID, contactUID)
	if err != nil || vcardData == "" {
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}

	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("ETag", s.storage.GetETag(username, addressbookID, contactUID))
	// #nosec G705 -- Content-Type is explicitly text/vcard, not executable HTML
	_, _ = w.Write([]byte(vcardData))
}

// handleDelete handles DELETE requests
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, username string) {
	// Extract address book ID and contact UID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		s.sendError(w, http.StatusNotFound, "contact not found")
		return
	}

	addressbookID := parts[0]
	contactUID := strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))

	// Verify the address book belongs to this user
	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	existing, err := s.storage.GetContact(username, addressbookID, contactUID)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid contact")
		return
	}
	if !preconditionsHold(r, existing != "", s.storage.GetETag(username, addressbookID, contactUID)) {
		s.sendError(w, http.StatusPreconditionFailed, "precondition failed")
		return
	}

	// Delete contact from storage
	if err := s.storage.DeleteContact(username, addressbookID, contactUID); err != nil {
		s.logger.Error("Failed to delete contact", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to delete contact")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMkCol handles MKCOL requests
func (s *Server) handleMkCol(w http.ResponseWriter, r *http.Request, username string) {
	// Extract address book ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	addressbookID := strings.TrimSuffix(path, "/")

	if addressbookID == "" {
		s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
		return
	}

	// Read request body for address book properties
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	name := addressbookID
	description := ""

	// Parse MKCOL request for displayname and description
	if len(body) > 0 {
		var mkcol struct {
			XMLName xml.Name `xml:"mkcol"`
			Set     struct {
				Prop struct {
					DisplayName string `xml:"displayname"`
					Description string `xml:"addressbook-description"`
				} `xml:"prop"`
			} `xml:"set"`
		}
		if err := xml.Unmarshal(body, &mkcol); err == nil {
			if mkcol.Set.Prop.DisplayName != "" {
				name = mkcol.Set.Prop.DisplayName
			}
			description = mkcol.Set.Prop.Description
		}
	}

	// Create addressbook
	ab := &Addressbook{
		ID:          addressbookID,
		Name:        name,
		Description: description,
	}

	if err := s.storage.CreateAddressbook(username, ab); err != nil {
		if errors.Is(err, errInvalidID) {
			s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
			return
		}
		s.logger.Error("Failed to create addressbook", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to create addressbook")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleProppatch handles PROPPATCH requests
func (s *Server) handleProppatch(w http.ResponseWriter, r *http.Request, username string) {
	// Extract address book ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
		return
	}
	addressbookID := parts[0]

	// Read and parse PROPPATCH request
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}

	// Get current address book
	ab, err := s.storage.GetAddressbook(username, addressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusNotFound, "addressbook not found")
		return
	}

	// Parse property update
	var proppatch struct {
		XMLName xml.Name `xml:"propertyupdate"`
		Set     *struct {
			Prop []struct {
				XMLName xml.Name
				Value   string `xml:",chardata"`
			} `xml:"prop"`
		} `xml:"set"`
		Remove *struct {
			Prop []struct {
				XMLName xml.Name
			} `xml:"prop"`
		} `xml:"remove"`
	}

	if err := xml.Unmarshal(body, &proppatch); err == nil {
		// Handle set operations
		if proppatch.Set != nil {
			for _, prop := range proppatch.Set.Prop {
				switch prop.XMLName.Local {
				case "displayname":
					ab.Name = prop.Value
				case "addressbook-description":
					ab.Description = prop.Value
				}
			}
		}
	}

	// Update address book
	if err := s.storage.UpdateAddressbook(username, ab); err != nil {
		s.logger.Error("Failed to update addressbook", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to update addressbook")
		return
	}

	w.WriteHeader(http.StatusOK)
}

// destinationWritable enforces the RFC 4918 §10.6 Overwrite header for
// MOVE/COPY: with "Overwrite: F" an existing destination must not be
// replaced and the request fails with 412 (F5483). Callers hold writeMu so
// the check and the write are atomic. It writes the error response itself.
func (s *Server) destinationWritable(w http.ResponseWriter, r *http.Request, username, addressbookID, contactUID string) bool {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Overwrite")), "F") {
		return true
	}
	existing, err := s.storage.GetContact(username, addressbookID, contactUID)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid destination contact")
		return false
	}
	if existing != "" {
		s.sendError(w, http.StatusPreconditionFailed, "destination exists and Overwrite is F")
		return false
	}
	return true
}

// moveCopyStatus is the success status of MOVE/COPY: 201 Created when the
// destination did not exist, 204 No Content when an existing resource was
// replaced (RFC 4918 §9.8.5, §9.9.4). Both used to answer 204 (F5574).
func moveCopyStatus(priorDestination string) int {
	if priorDestination == "" {
		return http.StatusCreated
	}
	return http.StatusNoContent
}

// handleMove handles MOVE requests
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, username string) {
	// Extract source address book and contact from URL path
	srcPath := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	srcParts := strings.SplitN(srcPath, "/", 2)
	if len(srcParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid source path")
		return
	}

	srcAddressbookID := srcParts[0]
	srcContactUID := strings.TrimSuffix(srcParts[1], filepath.Ext(srcParts[1]))

	// Verify the source address book belongs to this user
	ab, err := s.storage.GetAddressbook(username, srcAddressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}

	// Get destination from Destination header
	dest := r.Header.Get("Destination")
	if dest == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destPath, destOK := destinationPath(dest)
	if !destOK {
		s.sendError(w, http.StatusForbidden, "destination outside address book namespace")
		return
	}
	destParts := strings.SplitN(destPath, "/", 2)
	if len(destParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destAddressbookID := destParts[0]
	destContactUID := strings.TrimSuffix(destParts[1], filepath.Ext(destParts[1]))

	// Verify the destination address book belongs to this user
	ab, err = s.storage.GetAddressbook(username, destAddressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "destination address book not found")
		return
	}

	// RFC 4918 §9.9.4: MOVE onto itself is forbidden. Saving and then
	// deleting the same file used to destroy the contact (F5484).
	if srcAddressbookID == destAddressbookID && srcContactUID == destContactUID {
		s.sendError(w, http.StatusForbidden, "source and destination are the same")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if !s.destinationWritable(w, r, username, destAddressbookID, destContactUID) {
		return
	}
	// Whether the destination already exists decides 201 vs 204 (F5574).
	// The error is ignored on purpose: an invalid destination fails in
	// SaveContact below, which reports it.
	prior, _ := s.storage.GetContact(username, destAddressbookID, destContactUID)

	// Get contact data
	vcardData, err := s.storage.GetContact(username, srcAddressbookID, srcContactUID)
	if err != nil || vcardData == "" {
		s.sendError(w, http.StatusNotFound, "source contact not found")
		return
	}

	// If UID changed in destination, rewrite the UID property whatever its
	// case or parameters, so no stale UID survives the rename (F5573).
	if destContactUID != srcContactUID {
		vcardData = rewriteVCardUID(vcardData, destContactUID)
	}

	// Create contact in destination
	contact := &Contact{
		UID:      destContactUID,
		Modified: time.Now(),
		Created:  time.Now(),
	}

	if err := s.storage.SaveContact(username, destAddressbookID, contact, vcardData); err != nil {
		s.logger.Error("Failed to save contact at destination", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to move contact")
		return
	}

	// Delete source contact — failure here means the contact exists at both source and destination
	if err := s.storage.DeleteContact(username, srcAddressbookID, srcContactUID); err != nil {
		s.logger.Error("Failed to delete source contact after move", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to delete source contact")
		return
	}

	w.WriteHeader(moveCopyStatus(prior))
}

// handleCopy handles COPY requests
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request, username string) {
	// Extract source address book and contact from URL path
	srcPath := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	srcParts := strings.SplitN(srcPath, "/", 2)
	if len(srcParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid source path")
		return
	}

	srcAddressbookID := srcParts[0]
	srcContactUID := strings.TrimSuffix(srcParts[1], filepath.Ext(srcParts[1]))

	// Verify the source address book belongs to this user
	ab, err := s.storage.GetAddressbook(username, srcAddressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}

	// Get destination from Destination header
	dest := r.Header.Get("Destination")
	if dest == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destPath, destOK := destinationPath(dest)
	if !destOK {
		s.sendError(w, http.StatusForbidden, "destination outside address book namespace")
		return
	}
	destParts := strings.SplitN(destPath, "/", 2)
	if len(destParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destAddressbookID := destParts[0]
	destContactUID := strings.TrimSuffix(destParts[1], filepath.Ext(destParts[1]))

	// Verify the destination address book belongs to this user
	ab, err = s.storage.GetAddressbook(username, destAddressbookID)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "destination address book not found")
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if !s.destinationWritable(w, r, username, destAddressbookID, destContactUID) {
		return
	}
	// Whether the destination already exists decides 201 vs 204 (F5574).
	// The error is ignored on purpose: an invalid destination fails in
	// SaveContact below, which reports it.
	prior, _ := s.storage.GetContact(username, destAddressbookID, destContactUID)

	// Get contact data
	vcardData, err := s.storage.GetContact(username, srcAddressbookID, srcContactUID)
	if err != nil || vcardData == "" {
		s.sendError(w, http.StatusNotFound, "source contact not found")
		return
	}

	// If UID changed in destination, rewrite the UID property whatever its
	// case or parameters, so no stale UID survives the rename (F5573).
	if destContactUID != srcContactUID {
		vcardData = rewriteVCardUID(vcardData, destContactUID)
	}

	// Create contact in destination
	contact := &Contact{
		UID:      destContactUID,
		Modified: time.Now(),
		Created:  time.Now(),
	}

	if err := s.storage.SaveContact(username, destAddressbookID, contact, vcardData); err != nil {
		s.logger.Error("Failed to save contact at destination", "error", err)
		s.sendError(w, http.StatusInternalServerError, "failed to copy contact")
		return
	}

	w.WriteHeader(moveCopyStatus(prior))
}

// buildPrincipalResponse builds a response for the principal resource
func (s *Server) buildPrincipalResponse(username string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/principals/%s/", username),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        \u003ccollection/\u003e\n        \u003cprincipal/\u003e\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: username},
				{XMLName: xml.Name{Space: "CARDDAV:", Local: "addressbook-home-set"}, Value: "<href>/dav/addressbooks/</href>"},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildAddressbookHomeResponse builds a response for the addressbook home resource
func (s *Server) buildAddressbookHomeResponse(username string) Response {
	return Response{
		// Request convention: "/dav/addressbooks/{addressbookID}/..." — the
		// authenticated username scopes storage and is not part of the URL.
		Href: "/dav/addressbooks/",
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "<collection/>"},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: "Address Book"},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// sendError sends an error response
func (s *Server) sendError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(message))
}

// buildAddressbookResponse builds a PROPFIND response for an address book
func (s *Server) buildAddressbookResponse(username string, ab *Addressbook) Response {
	// Hrefs follow the server's request convention "/dav/addressbooks/{addressbookID}/"
	// (the authenticated username scopes storage and is not part of the URL), so
	// clients operating on these hrefs (RFC 4918 §8.3) reach the item handlers.
	return Response{
		Href: fmt.Sprintf("/dav/addressbooks/%s/", ab.ID),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        <collection/>\n        <addressbook xmlns=\"urn:ietf:params:xml:ns:carddav\"/>\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: ab.Name},
				{XMLName: xml.Name{Space: "CARDDAV:", Local: "addressbook-description"}, Value: ab.Description},
				// Nanosecond precision: a whole-second ctag hid changes made
				// within the same second from ctag-based sync (F5481).
				{XMLName: xml.Name{Space: "DAV:", Local: "getctag"}, Value: fmt.Sprintf("\"%d\"", ab.Modified.UnixNano())},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildContactResponse builds a PROPFIND/REPORT response for a contact
func (s *Server) buildContactResponse(username, addressbookID, uid, vcardData string) Response {
	// Request convention: "/dav/addressbooks/{addressbookID}/{uid}.vcf" (no username segment).
	return Response{
		Href: fmt.Sprintf("/dav/addressbooks/%s/%s.vcf", addressbookID, uid),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "getcontenttype"}, Value: "text/vcard; charset=utf-8"},
				{XMLName: xml.Name{Space: "DAV:", Local: "getetag"}, Value: s.storage.GetETag(username, addressbookID, uid)},
				{XMLName: xml.Name{Space: "CARDDAV:", Local: "address-data"}, Value: vcardData},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// insertVCardUID inserts a UID line directly after the first BEGIN:VCARD
// line, reusing that line's terminator (CRLF or LF) (F5480). Data without a
// terminated BEGIN:VCARD line is returned unchanged.
func insertVCardUID(data []byte, uid string) []byte {
	text := string(data)
	begin := strings.Index(text, "BEGIN:VCARD")
	if begin < 0 {
		return data
	}
	nl := strings.IndexByte(text[begin:], '\n')
	if nl < 0 {
		return data
	}
	end := begin + nl + 1
	eol := "\n"
	if nl > 0 && text[begin+nl-1] == '\r' {
		eol = "\r\n"
	}
	return []byte(text[:end] + "UID:" + uid + eol + text[end:])
}

// extractUIDFromVCard extracts the UID from vCard data. The property name is
// case-insensitive and may carry a group and parameters ("uid:x",
// "UID;VALUE=text:x"); such cards were previously unlisted and got a second
// UID line on PUT (F5573).
func (s *Server) extractUIDFromVCard(vcardData string) string {
	lines := strings.Split(vcardData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if uid, ok := isUIDLine(line); ok {
			return uid
		}
		if strings.HasPrefix(line, "UID=") {
			return strings.TrimPrefix(line, "UID=")
		}
	}
	return ""
}

// XML structures for WebDAV/CardDAV

// Propfind represents a PROPFIND request
type Propfind struct {
	XMLName xml.Name  `xml:"propfind"`
	AllProp *struct{} `xml:"allprop,omitempty"`
	Prop    *Prop     `xml:"prop,omitempty"`
}

// Prop represents properties
type Prop struct {
	XMLName xml.Name `xml:"prop"`
	Inner   []byte   `xml:",innerxml"`
}

// Multistatus represents a 207 Multi-Status response
type Multistatus struct {
	XMLName   xml.Name   `xml:"multistatus"`
	XMLNSDav  string     `xml:"xmlns:dav,attr,omitempty"`
	XMLNSCard string     `xml:"xmlns:card,attr,omitempty"`
	Responses []Response `xml:"response"`
}

// Response represents a response element in multistatus. Status is set
// instead of Propstat for an href that could not be served (RFC 4918 §14.24).
type Response struct {
	XMLName  xml.Name   `xml:"response"`
	Href     string     `xml:"href"`
	Propstat []Propstat `xml:"propstat"`
	Status   string     `xml:"status,omitempty"`
}

// Propstat represents property status
type Propstat struct {
	XMLName xml.Name   `xml:"propstat"`
	Prop    []Property `xml:"prop"`
	Status  string     `xml:"status"`
}

// Property represents a single property
type Property struct {
	XMLName xml.Name `xml:""`
	Value   string   `xml:",chardata"`
}

// AddressbookQuery represents an addressbook-query REPORT (RFC 6352 §10.3).
// The prop-filters live inside CARDDAV:filter (F5570).
type AddressbookQuery struct {
	XMLName xml.Name     `xml:"addressbook-query"`
	Prop    *Prop        `xml:"prop,omitempty"`
	Filter  *QueryFilter `xml:"filter"`
}

// AddressbookMultiget represents an addressbook-multiget REPORT
// (RFC 6352 §10.7) (F5572).
type AddressbookMultiget struct {
	XMLName xml.Name `xml:"addressbook-multiget"`
	Prop    *Prop    `xml:"prop,omitempty"`
	Hrefs   []string `xml:"href"`
}

// Contact represents a vCard contact
type Contact struct {
	UID          string     `json:"uid"`
	FullName     string     `json:"full_name"`
	FirstName    string     `json:"first_name,omitempty"`
	LastName     string     `json:"last_name,omitempty"`
	Email        []Email    `json:"email,omitempty"`
	Phone        []Phone    `json:"phone,omitempty"`
	Address      []Address  `json:"address,omitempty"`
	Organization string     `json:"organization,omitempty"`
	Title        string     `json:"title,omitempty"`
	Note         string     `json:"note,omitempty"`
	Birthday     *time.Time `json:"birthday,omitempty"`
	Photo        string     `json:"photo,omitempty"`
	Created      time.Time  `json:"created"`
	Modified     time.Time  `json:"modified"`
}

// Email represents an email address
type Email struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// Phone represents a phone number
type Phone struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Address represents a physical address
type Address struct {
	Type       string `json:"type"`
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	Region     string `json:"region,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	Country    string `json:"country,omitempty"`
}

// Addressbook represents an addressbook collection
type Addressbook struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ReadOnly    bool      `json:"read_only,omitempty"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
}
