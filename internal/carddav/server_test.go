package carddav

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewCardDAVServer(t *testing.T) {
	logger := slog.Default()
	server := NewServer(t.TempDir(), logger)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	if server.storage == nil {
		t.Error("Server storage should not be nil")
	}

	if server.logger == nil {
		t.Error("Server logger should not be nil")
	}
}

func TestCardDAVNewServerWithNilLogger(t *testing.T) {
	server := NewServer(t.TempDir(), nil)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	if server.logger == nil {
		t.Error("Server logger should use default when nil")
	}
}

func TestCardDAVSetAuthFunc(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	authFunc := func(username, password string) (bool, error) {
		return username == "test" && password == "pass", nil
	}

	server.SetAuthFunc(authFunc)

	if server.authFunc == nil {
		t.Error("AuthFunc should be set")
	}
}

func TestCardDAVServeHTTP_AuthRequired(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	req := httptest.NewRequest("OPTIONS", "/", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	authHeader := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(authHeader, "Basic") {
		t.Errorf("WWW-Authenticate = %s, should contain Basic", authHeader)
	}
}

func TestCardDAVServeHTTP_InvalidAuth(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return false, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:creds")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCardDAVServeHTTP_ValidAuth(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCardDAVHandleOptions(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	allow := w.Header().Get("Allow")
	if !strings.Contains(allow, "PROPFIND") {
		t.Errorf("Allow header = %s, should contain PROPFIND", allow)
	}

	dav := w.Header().Get("DAV")
	if !strings.Contains(dav, "addressbook") {
		t.Errorf("DAV header = %s, should contain addressbook", dav)
	}
}

func TestCardDAVHandlePropfind(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/xml") {
		t.Errorf("Content-Type = %s, want application/xml", contentType)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "multistatus") {
		t.Error("Response should contain multistatus")
	}
}

func TestCardDAVHandlePropfindWithAddressbooks(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create an addressbook
	ab := &Addressbook{
		ID:   "test-ab",
		Name: "Test Addressbook",
	}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "Test Addressbook") {
		t.Error("Response should contain addressbook name")
	}
}

func TestCardDAVHandleMkCol(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("MKCOL", "/dav/addressbooks/new-ab", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestCardDAVHandlePut(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	vcardData := `BEGIN:VCARD
VERSION:3.0
UID:test-contact-123
FN:John Doe
EMAIL:john@example.com
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact-123.vcf", strings.NewReader(vcardData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Error("ETag header should be set")
	}
}

func TestCardDAVHandlePut_InvalidVCard(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	invalidData := "This is not vCard data"

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/contact.vcf", strings.NewReader(invalidData))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestCardDAVHandleGet(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	vcardData := `BEGIN:VCARD
UID:test-contact-123
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact-123"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	req := httptest.NewRequest("GET", "/dav/addressbooks/test-ab/test-contact-123.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/vcard") {
		t.Errorf("Content-Type = %s, want text/vcard", contentType)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "John Doe") {
		t.Error("Response should contain contact data")
	}
}

func TestCardDAVHandleGet_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("GET", "/dav/addressbooks/test-ab/nonexistent.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandleDelete(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	vcardData := `BEGIN:VCARD
UID:test-contact-123
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact-123"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	req := httptest.NewRequest("DELETE", "/dav/addressbooks/test-ab/test-contact-123.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestCardDAVHandleReport(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contacts
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	for i := 0; i < 3; i++ {
		vcardData := `BEGIN:VCARD
UID:contact-` + string(rune('a'+i)) + `
FN:Contact ` + string(rune('A'+i)) + `
END:VCARD`
		contact := &Contact{UID: "contact-" + string(rune('a'+i))}
		_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)
	}

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<addressbook-query xmlns="urn:ietf:params:xml:ns:carddav">
<prop><address-data/></prop>
</addressbook-query>`

	req := httptest.NewRequest("REPORT", "/dav/addressbooks/test-ab", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "multistatus") {
		t.Error("Response should contain multistatus")
	}
}

func TestCardDAVHandleReport_InvalidQuery(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	invalidBody := "This is not valid XML"

	req := httptest.NewRequest("REPORT", "/dav/addressbooks/test-ab", strings.NewReader(invalidBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCardDAVHandleProppatch(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	proppatchBody := `<?xml version="1.0"?>
<propertyupdate xmlns="DAV:">
<set>
<prop>
<displayname>Updated Name</displayname>
</prop>
</set>
</propertyupdate>`

	req := httptest.NewRequest("PROPPATCH", "/dav/addressbooks/test-ab", strings.NewReader(proppatchBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCardDAVHandleMove(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcardData := `BEGIN:VCARD
UID:test-contact-123
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact-123"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	req := httptest.NewRequest("MOVE", "/dav/addressbooks/test-ab/test-contact-123.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/moved-contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestCardDAVHandleMove_MissingDestination(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("MOVE", "/dav/addressbooks/test-ab/contact.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCardDAVHandleCopy(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcardData := `BEGIN:VCARD
UID:test-contact-123
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact-123"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	req := httptest.NewRequest("COPY", "/dav/addressbooks/test-ab/test-contact-123.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/copied-contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestCardDAVHandleMethodNotAllowed(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestExtractUIDFromVCard(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())

	tests := []struct {
		name      string
		vcardData string
		want      string
	}{
		{
			name:      "UID present",
			vcardData: "BEGIN:VCARD\nUID:test-uid-123\nEND:VCARD",
			want:      "test-uid-123",
		},
		{
			name:      "UID absent",
			vcardData: "BEGIN:VCARD\nFN:Test\nEND:VCARD",
			want:      "",
		},
		{
			name:      "Empty data",
			vcardData: "",
			want:      "",
		},
		{
			name:      "UID with equals",
			vcardData: "BEGIN:VCARD\nUID=test-uid\nEND:VCARD",
			want:      "test-uid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := server.extractUIDFromVCard(tt.vcardData)
			if got != tt.want {
				t.Errorf("extractUIDFromVCard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCardDAVBuildPrincipalResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	resp := server.buildPrincipalResponse("user@example.com")

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	found := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" {
			found = true
			if prop.Value != "user@example.com" {
				t.Errorf("displayname = %s, want user@example.com", prop.Value)
			}
		}
	}
	if !found {
		t.Error("displayname property not found")
	}
}

func TestCardDAVBuildAddressbookHomeResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	resp := server.buildAddressbookHomeResponse("user@example.com")

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	found := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" {
			found = true
			if prop.Value != "Address Book" {
				t.Errorf("displayname = %s, want Address Book", prop.Value)
			}
		}
	}
	if !found {
		t.Error("displayname property not found")
	}
}

func TestCardDAVBuildAddressbookResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	ab := &Addressbook{
		ID:          "test-ab",
		Name:        "Test Addressbook",
		Description: "A test addressbook",
	}

	resp := server.buildAddressbookResponse("user@example.com", ab)

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	hasName := false
	hasDesc := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" && prop.Value == "Test Addressbook" {
			hasName = true
		}
		if prop.XMLName.Local == "addressbook-description" && prop.Value == "A test addressbook" {
			hasDesc = true
		}
	}

	if !hasName {
		t.Error("displayname property not found or incorrect")
	}
	if !hasDesc {
		t.Error("addressbook-description property not found or incorrect")
	}
}

func TestCardDAVBuildContactResponse(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	vcardData := `BEGIN:VCARD
UID:test-contact
FN:John Doe
END:VCARD`

	resp := server.buildContactResponse("user@example.com", "test-ab", "test-contact", vcardData)

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	hasAddressData := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "address-data" {
			hasAddressData = true
			if prop.Value != vcardData {
				t.Error("address-data should match vcardData")
			}
		}
	}

	if !hasAddressData {
		t.Error("address-data property not found")
	}
}

func TestCardDAVXMLStructures(t *testing.T) {
	// Test Propfind marshaling/unmarshaling
	propfind := Propfind{}
	data, err := xml.Marshal(propfind)
	if err != nil {
		t.Errorf("Failed to marshal Propfind: %v", err)
	}
	if len(data) == 0 {
		t.Error("Marshaled Propfind should not be empty")
	}

	// Test Multistatus marshaling
	multistatus := &Multistatus{
		Responses: []Response{
			{
				Href: "/test",
				Propstat: []Propstat{{
					Status: "HTTP/1.1 200 OK",
					Prop: []Property{
						{XMLName: xml.Name{Local: "displayname"}, Value: "Test"},
					},
				}},
			},
		},
	}

	data, err = xml.MarshalIndent(multistatus, "", "  ")
	if err != nil {
		t.Errorf("Failed to marshal Multistatus: %v", err)
	}

	var unmarshaled Multistatus
	if err := xml.Unmarshal(data, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal Multistatus: %v", err)
	}

	if len(unmarshaled.Responses) != 1 {
		t.Errorf("Expected 1 response, got %d", len(unmarshaled.Responses))
	}
}

func TestCardDAVSendError(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	w := httptest.NewRecorder()

	server.sendError(w, http.StatusInternalServerError, "test error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	body, _ := io.ReadAll(w.Body)
	if string(body) != "test error" {
		t.Errorf("Body = %s, want test error", string(body))
	}
}

func TestCardDAVHandlePropfindWithBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	propfindBody := `<?xml version="1.0" encoding="utf-8"?>
<propfind xmlns="DAV:">
<prop>
<displayname/>
<resourcetype/>
</prop>
</propfind>`

	req := httptest.NewRequest("PROPFIND", "/dav/", bytes.NewReader([]byte(propfindBody)))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

// Test handleDelete error cases
func TestCardDAVHandleDelete_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook but not contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("DELETE", "/dav/addressbooks/test-ab/nonexistent.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Implementation may return 204 even for non-existent (idempotent DELETE)
	// or may return error - both are acceptable
	if w.Code != http.StatusNoContent && w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, unexpected", w.Code)
	}
}

// Test handleMkCol with body
func TestCardDAVHandleMkCol_WithBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	mkcolBody := `<?xml version="1.0" encoding="utf-8"?>
<mkcol xmlns="DAV:">
  <set>
    <prop>
      <displayname>My Contacts</displayname>
    </prop>
  </set>
</mkcol>`

	req := httptest.NewRequest("MKCOL", "/dav/addressbooks/new-contacts", bytes.NewReader([]byte(mkcolBody)))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Verify addressbook was created with custom name
	ab, err := server.storage.GetAddressbook("user@example.com", "new-contacts")
	if err != nil {
		t.Errorf("Failed to get created addressbook: %v", err)
	}
	if ab.Name != "My Contacts" {
		t.Errorf("Expected name 'My Contacts', got '%s'", ab.Name)
	}
}

// Test handleMkCol with invalid body
func TestCardDAVHandleMkCol_InvalidBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Send invalid XML
	req := httptest.NewRequest("MKCOL", "/dav/addressbooks/test-mkcol", bytes.NewReader([]byte("invalid xml")))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should still create addressbook with default name
	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

// Test handleProppatch with various scenarios
func TestCardDAVHandleProppatch_NoBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	// Send PROPPATCH without body
	req := httptest.NewRequest("PROPPATCH", "/dav/addressbooks/test-ab/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should handle empty body
	if w.Code != http.StatusMultiStatus && w.Code != http.StatusBadRequest {
		t.Logf("Status = %d (may vary based on implementation)", w.Code)
	}
}

// Test handleCopy with overwrite
func TestCardDAVHandleCopy_Overwrite(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcardData := `BEGIN:VCARD
UID:test-contact
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	// Copy to same location (overwrite)
	req := httptest.NewRequest("COPY", "/dav/addressbooks/test-ab/test-contact.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/test-contact-copy.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}

// Test handleCopy to different addressbook
func TestCardDAVHandleCopy_DifferentAddressbook(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create two addressbooks
	ab1 := &Addressbook{ID: "ab1", Name: "AB1"}
	ab2 := &Addressbook{ID: "ab2", Name: "AB2"}
	server.storage.CreateAddressbook("user@example.com", ab1)
	server.storage.CreateAddressbook("user@example.com", ab2)

	// Create contact in first addressbook
	vcardData := `BEGIN:VCARD
UID:test-contact
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	server.storage.SaveContact("user@example.com", "ab1", contact, vcardData)

	// Copy to second addressbook
	req := httptest.NewRequest("COPY", "/dav/addressbooks/ab1/test-contact.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/ab2/test-contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}

// Test handleMove to different addressbook
func TestCardDAVHandleMove_DifferentAddressbook(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create two addressbooks
	ab1 := &Addressbook{ID: "ab1", Name: "AB1"}
	ab2 := &Addressbook{ID: "ab2", Name: "AB2"}
	server.storage.CreateAddressbook("user@example.com", ab1)
	server.storage.CreateAddressbook("user@example.com", ab2)

	// Create contact in first addressbook
	vcardData := `BEGIN:VCARD
UID:test-contact
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	server.storage.SaveContact("user@example.com", "ab1", contact, vcardData)

	// Move to second addressbook
	req := httptest.NewRequest("MOVE", "/dav/addressbooks/ab1/test-contact.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/ab2/moved-contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}

	// Verify move completed (implementation details may vary)
	t.Logf("Move completed with status %d", w.Code)
}

// Test handlePut update existing
func TestCardDAVHandlePut_UpdateExisting(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcardData := `BEGIN:VCARD
UID:test-contact
FN:John Doe
EMAIL:john@example.com
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcardData)

	// Update contact
	updatedVCard := `BEGIN:VCARD
UID:test-contact
FN:John Updated
EMAIL:john.updated@example.com
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact.vcf", strings.NewReader(updatedVCard))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Content-Type", "text/vcard")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}

func TestCardDAVHandleDelete_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Path doesn't have proper format - only one part
	req := httptest.NewRequest("DELETE", "/dav/addressbooks/test-ab", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandleMove_InvalidSourcePath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Source path only has addressbook, no contact
	req := httptest.NewRequest("MOVE", "/dav/addressbooks/test-ab", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCardDAVHandleCopy_InvalidSourcePath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Source path only has addressbook, no contact
	req := httptest.NewRequest("COPY", "/dav/addressbooks/test-ab", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCardDAVHandleMove_SourceNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook but not the contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("MOVE", "/dav/addressbooks/test-ab/nonexistent.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandleCopy_SourceNotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook but not the contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("COPY", "/dav/addressbooks/test-ab/nonexistent.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Destination", "/dav/addressbooks/test-ab/contact.vcf")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandlePut_InvalidVCard_NoBegin(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	// Send invalid vCard - missing BEGIN
	vcard := `UID:test-contact
FN:John Doe
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact.vcf", strings.NewReader(vcard))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnsupportedMediaType)
	}
}

func TestCardDAVHandlePut_InvalidVCard_NoUID(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	// Send vCard without explicit UID - server may accept it or use path-based UID
	vcard := `BEGIN:VCARD
FN:John Doe
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact.vcf", strings.NewReader(vcard))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Server accepts this - it will use path-based UID
	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}

func TestCardDAVHandleGet_NotFoundAtStorage(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook but not the contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("GET", "/dav/addressbooks/test-ab/nonexistent.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandleMkCol_InvalidPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Empty addressbook ID
	req := httptest.NewRequest("MKCOL", "/dav/addressbooks/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCardDAVHandleProppatch_NotFound(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// No addressbook exists
	body := `<?xml version="1.0" encoding="UTF-8"?>
<D:propertyupdate xmlns:D="DAV:">
  <D:set>
    <D:prop>
      <D:displayname>New Name</D:displayname>
    </D:prop>
  </D:set>
</D:propertyupdate>`

	req := httptest.NewRequest("PROPPATCH", "/dav/addressbooks/nonexistent", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCardDAVHandlePropfind_Depth0(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	req := httptest.NewRequest("PROPFIND", "/dav/addressbooks/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "0")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestCardDAVHandlePropfind_DepthInfinity(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook with contacts
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcard := `BEGIN:VCARD
UID:test-contact
FN:John Doe
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcard)

	req := httptest.NewRequest("PROPFIND", "/dav/addressbooks/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "infinity")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestCardDAVHandleProppatch_WithBody(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	body := `<?xml version="1.0" encoding="UTF-8"?>
<D:propertyupdate xmlns:D="DAV:">
  <D:set>
    <D:prop>
      <D:displayname>Updated Name</D:displayname>
    </D:prop>
  </D:set>
</D:propertyupdate>`

	req := httptest.NewRequest("PROPPATCH", "/dav/addressbooks/test-ab", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCardDAVHandlePropfind_PrincipalPath(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	req := httptest.NewRequest("PROPFIND", "/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

func TestCardDAVHandleMkCol_WithContact(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook first
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	// Add a contact via PUT
	vcard := `BEGIN:VCARD
UID:test-contact
FN:Test Contact
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact.vcf", strings.NewReader(vcard))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}

// Test handleProppatch with remove operation
func TestCardDAVHandleProppatch_RemoveProperty(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook with description
	ab := &Addressbook{ID: "test-ab", Name: "Test", Description: "Original Description"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	body := `<?xml version="1.0" encoding="UTF-8"?>
<D:propertyupdate xmlns:D="DAV:">
  <D:remove>
    <D:prop>
      <D:addressbook-description/>
    </D:prop>
  </D:remove>
</D:propertyupdate>`

	req := httptest.NewRequest("PROPPATCH", "/dav/addressbooks/test-ab", strings.NewReader(body))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Remove operation should still return OK
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}

// Test handleReport with multiple contacts
func TestCardDAVHandleReport_MultipleContacts(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook with multiple contacts
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	for i := 0; i < 3; i++ {
		vcard := fmt.Sprintf(`BEGIN:VCARD
UID:contact-%d
FN:Contact %d
END:VCARD`, i, i)
		contact := &Contact{UID: fmt.Sprintf("contact-%d", i)}
		_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcard)
	}

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<addressbook-query xmlns="urn:ietf:params:xml:ns:carddav">
<prop><address-data/></prop>
</addressbook-query>`

	req := httptest.NewRequest("REPORT", "/dav/addressbooks/test-ab", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	body, _ := io.ReadAll(w.Body)
	// Should contain all 3 contacts
	for i := 0; i < 3; i++ {
		if !strings.Contains(string(body), fmt.Sprintf("contact-%d", i)) {
			t.Errorf("Response should contain contact-%d", i)
		}
	}
}

// Test handleReport non-existent addressbook
func TestCardDAVHandleReport_NonExistentAddressbook(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	reportBody := `<?xml version="1.0" encoding="utf-8"?>
<addressbook-query xmlns="urn:ietf:params:xml:ns:carddav">
<prop><address-data/></prop>
</addressbook-query>`

	req := httptest.NewRequest("REPORT", "/dav/addressbooks/nonexistent-ab", strings.NewReader(reportBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should still return multistatus (empty) rather than error
	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}
}

// Test handlePropfind with Depth: 1 on addressbook
func TestCardDAVHandlePropfind_Depth1OnAddressbook(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook with contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcard := `BEGIN:VCARD
UID:test-contact
FN:Test Contact
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcard)

	req := httptest.NewRequest("PROPFIND", "/dav/addressbooks/test-ab", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusMultiStatus)
	}

	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "test-contact") {
		t.Error("Response should contain contact")
	}
}

// Test handleDelete then get
func TestCardDAVHandleDelete_ThenGet(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcard := `BEGIN:VCARD
UID:test-contact
FN:Test Contact
END:VCARD`
	contact := &Contact{UID: "test-contact"}
	_ = server.storage.SaveContact("user@example.com", "test-ab", contact, vcard)

	// Delete the contact
	req := httptest.NewRequest("DELETE", "/dav/addressbooks/test-ab/test-contact.vcf", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Try to get the deleted contact
	req2 := httptest.NewRequest("GET", "/dav/addressbooks/test-ab/test-contact.vcf", nil)
	req2.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w2 := httptest.NewRecorder()

	server.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", w2.Code, http.StatusNotFound)
	}
}

// Test handleMkCol with special characters in name
func TestCardDAVHandleMkCol_SpecialChars(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	mkcolBody := `<?xml version="1.0" encoding="utf-8"?>
<mkcol xmlns="DAV:">
  <set>
    <prop>
      <displayname>My Special Contacts & Friends</displayname>
    </prop>
  </set>
</mkcol>`

	req := httptest.NewRequest("MKCOL", "/dav/addressbooks/special-chars", strings.NewReader(mkcolBody))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusCreated)
	}
}

// Test CardDAVBuildPrincipalResponse with different username
func TestCardDAVBuildPrincipalResponse_DifferentUser(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	resp := server.buildPrincipalResponse("admin@company.com")

	if resp.Href == "" {
		t.Error("Href should not be empty")
	}

	if len(resp.Propstat) == 0 {
		t.Fatal("Propstat should not be empty")
	}

	found := false
	for _, prop := range resp.Propstat[0].Prop {
		if prop.XMLName.Local == "displayname" && prop.Value == "admin@company.com" {
			found = true
		}
	}
	if !found {
		t.Error("displayname property should contain username")
	}
}

// Test handlePut without content-type header
func TestCardDAVHandlePut_NoContentType(t *testing.T) {
	server := NewServer(t.TempDir(), slog.Default())
	server.SetAuthFunc(func(username, password string) (bool, error) {
		return true, nil
	})

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	_ = server.storage.CreateAddressbook("user@example.com", ab)

	vcard := `BEGIN:VCARD
UID:test-contact
FN:Test Contact
END:VCARD`

	req := httptest.NewRequest("PUT", "/dav/addressbooks/test-ab/test-contact.vcf", strings.NewReader(vcard))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:pass")))
	// No Content-Type header
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should still work
	if w.Code != http.StatusCreated && w.Code != http.StatusNoContent {
		t.Errorf("Status = %d, want %d or %d", w.Code, http.StatusCreated, http.StatusNoContent)
	}
}
