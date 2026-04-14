package autoconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- getAuthMethod coverage ---

func TestGetAuthMethod_WithSSL(t *testing.T) {
	handler := NewHandler(nil)

	// SSL = true should return password-encrypted
	if got := handler.getAuthMethod(true); got != "password-encrypted" {
		t.Errorf("getAuthMethod(true) = %q, want %q", got, "password-encrypted")
	}
}

func TestGetAuthMethod_WithoutSSL(t *testing.T) {
	handler := NewHandler(nil)

	// SSL = false should return password-cleartext
	if got := handler.getAuthMethod(false); got != "password-cleartext" {
		t.Errorf("getAuthMethod(false) = %q, want %q", got, "password-cleartext")
	}
}

// --- extractDomain coverage ---

func TestExtractDomain_WithAutoconfigPrefix(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "http://autoconfig.example.com/test", nil)
	domain := handler.extractDomain(req)
	if domain != "example.com" {
		t.Errorf("extractDomain(autoconfig.example.com) = %q, want %q", domain, "example.com")
	}
}

func TestExtractDomain_WithAutodiscoverPrefix(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "http://autodiscover.example.com/test", nil)
	domain := handler.extractDomain(req)
	if domain != "example.com" {
		t.Errorf("extractDomain(autodiscover.example.com) = %q, want %q", domain, "example.com")
	}
}

func TestExtractDomain_WithPort(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "http://example.com:8080/test", nil)
	domain := handler.extractDomain(req)
	if domain != "example.com" {
		t.Errorf("extractDomain(example.com:8080) = %q, want %q", domain, "example.com")
	}
}

func TestExtractDomain_InvalidDomain(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "http://invalid..com/test", nil)
	domain := handler.extractDomain(req)
	if domain != "" {
		t.Errorf("extractDomain(invalid..com) = %q, want empty", domain)
	}
}

func TestExtractDomain_PlainDomain(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/test", nil)
	domain := handler.extractDomain(req)
	if domain != "example.com" {
		t.Errorf("extractDomain(example.com) = %q, want %q", domain, "example.com")
	}
}

// --- DefaultPorts coverage ---

func TestDefaultPorts_ReturnsCorrectPorts(t *testing.T) {
	ports := DefaultPorts()
	if ports["imap"]["SSL"] != 993 {
		t.Errorf("Expected IMAP SSL port 993, got %d", ports["imap"]["SSL"])
	}
	if ports["imap"]["STARTTLS"] != 143 {
		t.Errorf("Expected IMAP STARTTLS port 143, got %d", ports["imap"]["STARTTLS"])
	}
	if ports["smtp"]["SSL"] != 465 {
		t.Errorf("Expected SMTP SSL port 465, got %d", ports["smtp"]["SSL"])
	}
	if ports["smtp"]["STARTTLS"] != 587 {
		t.Errorf("Expected SMTP STARTTLS port 587, got %d", ports["smtp"]["STARTTLS"])
	}
}

// --- extractEmailFromHost coverage ---

func TestExtractEmailFromHost_WithEmailInHost(t *testing.T) {
	handler := NewHandler(nil)

	email := handler.extractEmailFromHost("user@example.com")
	if email != "user@example.com" {
		t.Errorf("extractEmailFromHost(user@example.com) = %q, want %q", email, "user@example.com")
	}
}

func TestExtractEmailFromHost_WithAutodiscoverPrefix(t *testing.T) {
	handler := NewHandler(nil)

	email := handler.extractEmailFromHost("user.autodiscover.example.com")
	if email != "" {
		t.Errorf("extractEmailFromHost(autodiscover) = %q, want empty (no @ in host)", email)
	}
}

func TestExtractEmailFromHost_InvalidHost(t *testing.T) {
	handler := NewHandler(nil)

	email := handler.extractEmailFromHost("invalid")
	if email != "" {
		t.Errorf("extractEmailFromHost(invalid) = %q, want empty", email)
	}
}

// --- HandleAutoconfig edge cases ---

func TestHandleAutoconfig_InvalidHost(t *testing.T) {
	handler := NewHandler(nil)

	// Host that is not a valid domain
	req := httptest.NewRequest(http.MethodGet, "http://invalid..com/.well-known/autoconfig/mail/config-v1.1.xml", nil)
	w := httptest.NewRecorder()

	handler.HandleAutoconfig(w, req)

	// Should return 400 for invalid domain
	if w.Code != http.StatusBadRequest {
		t.Errorf("HandleAutoconfig() status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- HandleAutodiscover POST with invalid XML ---

func TestHandleAutodiscover_POST_InvalidXML(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "http://autodiscover.example.com/autodiscover/autodiscover.xml", strings.NewReader("not xml"))
	req.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()

	handler.HandleAutodiscover(w, req)

	// Should return 400 for invalid XML
	if w.Code != http.StatusBadRequest {
		t.Errorf("HandleAutodiscover() with invalid XML status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAutodiscover_POST_NoEmailInXML(t *testing.T) {
	handler := NewHandler(nil)

	// Valid XML but no email address
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/requestschema/2006">
  <Request>
    <EMailAddress></EMailAddress>
  </Request>
</Autodiscover>`

	req := httptest.NewRequest(http.MethodPost, "http://autodiscover.example.com/autodiscover/autodiscover.xml", strings.NewReader(xmlBody))
	req.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()

	handler.HandleAutodiscover(w, req)

	// Should return 400 when email is empty in XML body
	if w.Code != http.StatusBadRequest {
		t.Errorf("HandleAutodiscover() with empty email status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- parseEmailFromXML coverage ---

func TestParseEmailFromXML_ValidXML(t *testing.T) {
	handler := NewHandler(nil)

	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/requestschema/2006">
  <Request>
    <EMailAddress>user@example.com</EMailAddress>
  </Request>
</Autodiscover>`

	email := handler.parseEmailFromXML([]byte(xmlBody))
	if email != "user@example.com" {
		t.Errorf("parseEmailFromXML() = %q, want %q", email, "user@example.com")
	}
}

func TestParseEmailFromXML_InvalidXML(t *testing.T) {
	handler := NewHandler(nil)

	email := handler.parseEmailFromXML([]byte("not xml"))
	if email != "" {
		t.Errorf("parseEmailFromXML(invalid) = %q, want empty", email)
	}
}

func TestParseEmailFromXML_MissingEmail(t *testing.T) {
	handler := NewHandler(nil)

	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/requestschema/2006">
  <Request>
  </Request>
</Autodiscover>`

	email := handler.parseEmailFromXML([]byte(xmlBody))
	if email != "" {
		t.Errorf("parseEmailFromXML(missing email) = %q, want empty", email)
	}
}

// --- buildAutoconfig coverage ---

func TestBuildAutoconfig_WithProvider(t *testing.T) {
	handler := NewHandler(nil)
	handler.provider = &mockProvider{host: "mail.example.com"}

	config := handler.buildAutoconfig("example.com")
	if config == nil || len(config.Providers) != 1 {
		t.Errorf("Providers length = %d, want 1", len(config.Providers))
	}
}

// mockProvider implements ConfigProvider interface for testing
type mockProvider struct {
	host string
}

func (m *mockProvider) GetMailServerHost(domain string) string {
	return m.host
}

func (m *mockProvider) GetIncomingPort(domain string, protocol string, ssl bool) int {
	if ssl {
		return 993
	}
	return 143
}

func (m *mockProvider) GetOutgoingPort(domain string, ssl bool) int {
	if ssl {
		return 465
	}
	return 587
}

func (m *mockProvider) SupportsSSL(domain string) bool {
	return true
}
