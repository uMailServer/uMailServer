// Package carddav provides CardDAV (RFC 6352) address book synchronization support
package carddav

import (
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/umailserver/umailserver/internal/tracing"
)

// Server represents a CardDAV server
type Server struct {
	logger          *slog.Logger
	authFunc        func(username, password string) (bool, error)
	dataDir         string
	storage         *Storage
	tracingProvider *tracing.Provider
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

// handlePropfind handles PROPFIND requests
func (s *Server) handlePropfind(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

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
		if err == nil {
			for _, ab := range addressbooks {
				multistatus.Responses = append(multistatus.Responses, s.buildAddressbookResponse(username, ab))

				// If depth is infinity or 1, include contacts
				if depth == "infinity" || depth == "1" {
					contacts, err := s.storage.GetContacts(username, ab.ID)
					if err == nil {
						for _, contact := range contacts {
							uid := s.extractUIDFromVCard(contact)
							if uid != "" {
								multistatus.Responses = append(multistatus.Responses, s.buildContactResponse(username, ab.ID, uid, contact))
							}
						}
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

// handleReport handles REPORT requests
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Parse addressbook query
	var query AddressbookQuery
	if err := xml.Unmarshal(body, &query); err != nil {
		s.logger.Debug("Failed to parse REPORT", "error", err)
		s.sendError(w, http.StatusBadRequest, "invalid addressbook query")
		return
	}

	// Build response
	multistatus := &Multistatus{}

	// Extract address book ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		addressbookID := parts[0]
		contacts, err := s.storage.GetContacts(username, addressbookID)
		if err == nil {
			for _, contact := range contacts {
				uid := s.extractUIDFromVCard(contact)
				if uid != "" {
					multistatus.Responses = append(multistatus.Responses, s.buildContactResponse(username, addressbookID, uid, contact))
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

// handlePut handles PUT requests for creating/updating contacts
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, username string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	// Validate vCard data
	if !strings.Contains(string(body), "BEGIN:VCARD") {
		s.sendError(w, http.StatusUnsupportedMediaType, "invalid vcard data")
		return
	}

	// Extract UID from vCard
	uid := s.extractUIDFromVCard(string(body))
	if uid == "" {
		uid = uuid.New().String()
		// Add UID to vCard if missing
		body = []byte(strings.Replace(string(body), "BEGIN:VCARD\r\n", fmt.Sprintf("BEGIN:VCARD\r\nUID:%s\r\n", uid), 1))
	}

	// Extract address book ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 || parts[0] == "" {
		s.sendError(w, http.StatusBadRequest, "invalid addressbook ID")
		return
	}
	addressbookID := parts[0]

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
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

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

	// Get destination from Destination header
	dest := r.Header.Get("Destination")
	if dest == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destPath := strings.TrimPrefix(dest, "/dav/addressbooks/")
	destParts := strings.SplitN(destPath, "/", 2)
	if len(destParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destAddressbookID := destParts[0]
	destContactUID := strings.TrimSuffix(destParts[1], filepath.Ext(destParts[1]))

	// Get contact data
	vcardData, err := s.storage.GetContact(username, srcAddressbookID, srcContactUID)
	if err != nil || vcardData == "" {
		s.sendError(w, http.StatusNotFound, "source contact not found")
		return
	}

	// If UID changed in destination, update vCard
	if destContactUID != srcContactUID {
		vcardData = strings.Replace(vcardData, fmt.Sprintf("UID:%s", srcContactUID), fmt.Sprintf("UID:%s", destContactUID), 1)
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

	// Delete source contact
	if err := s.storage.DeleteContact(username, srcAddressbookID, srcContactUID); err != nil {
		s.logger.Error("Failed to delete source contact", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
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

	// Get destination from Destination header
	dest := r.Header.Get("Destination")
	if dest == "" {
		s.sendError(w, http.StatusBadRequest, "missing destination header")
		return
	}

	// Parse destination path
	destPath := strings.TrimPrefix(dest, "/dav/addressbooks/")
	destParts := strings.SplitN(destPath, "/", 2)
	if len(destParts) < 2 {
		s.sendError(w, http.StatusBadRequest, "invalid destination path")
		return
	}

	destAddressbookID := destParts[0]
	destContactUID := strings.TrimSuffix(destParts[1], filepath.Ext(destParts[1]))

	// Get contact data
	vcardData, err := s.storage.GetContact(username, srcAddressbookID, srcContactUID)
	if err != nil || vcardData == "" {
		s.sendError(w, http.StatusNotFound, "source contact not found")
		return
	}

	// If UID changed in destination, update vCard
	if destContactUID != srcContactUID {
		vcardData = strings.Replace(vcardData, fmt.Sprintf("UID:%s", srcContactUID), fmt.Sprintf("UID:%s", destContactUID), 1)
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

	w.WriteHeader(http.StatusNoContent)
}

// buildPrincipalResponse builds a response for the principal resource
func (s *Server) buildPrincipalResponse(username string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/principals/%s/", username),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        \u003ccollection/\u003e\n        \u003cprincipal/\u003e\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: username},
				{XMLName: xml.Name{Space: "CARDDAV:", Local: "addressbook-home-set"}, Value: fmt.Sprintf("<href>/dav/addressbooks/%s/</href>", username)},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildAddressbookHomeResponse builds a response for the addressbook home resource
func (s *Server) buildAddressbookHomeResponse(username string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/addressbooks/%s/", username),
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
	return Response{
		Href: fmt.Sprintf("/dav/addressbooks/%s/%s/", username, ab.ID),
		Propstat: []Propstat{{
			Prop: []Property{
				{XMLName: xml.Name{Space: "DAV:", Local: "resourcetype"}, Value: "\n        <collection/>\n        <addressbook xmlns=\"urn:ietf:params:xml:ns:carddav\"/>\n      "},
				{XMLName: xml.Name{Space: "DAV:", Local: "displayname"}, Value: ab.Name},
				{XMLName: xml.Name{Space: "CARDDAV:", Local: "addressbook-description"}, Value: ab.Description},
				{XMLName: xml.Name{Space: "DAV:", Local: "getctag"}, Value: fmt.Sprintf("\"%d\"", ab.Modified.Unix())},
			},
			Status: "HTTP/1.1 200 OK",
		}},
	}
}

// buildContactResponse builds a PROPFIND/REPORT response for a contact
func (s *Server) buildContactResponse(username, addressbookID, uid, vcardData string) Response {
	return Response{
		Href: fmt.Sprintf("/dav/addressbooks/%s/%s/%s.vcf", username, addressbookID, uid),
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

// extractUIDFromVCard extracts the UID from vCard data
func (s *Server) extractUIDFromVCard(vcardData string) string {
	lines := strings.Split(vcardData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UID:") {
			return strings.TrimPrefix(line, "UID:")
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

// Response represents a response element in multistatus
type Response struct {
	XMLName  xml.Name   `xml:"response"`
	Href     string     `xml:"href"`
	Propstat []Propstat `xml:"propstat"`
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

// AddressbookQuery represents an addressbook-query REPORT
type AddressbookQuery struct {
	XMLName    xml.Name    `xml:"addressbook-query"`
	PropFilter *PropFilter `xml:"prop-filter,omitempty"`
	Prop       *Prop       `xml:"prop,omitempty"`
}

// PropFilter represents a property filter
type PropFilter struct {
	XMLName xml.Name `xml:"prop-filter"`
	Name    string   `xml:"name,attr"`
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
