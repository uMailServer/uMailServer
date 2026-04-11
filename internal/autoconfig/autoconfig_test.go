package autoconfig

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidator_ValidateEmail(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		email   string
		wantErr bool
	}{
		{"user@example.com", false},
		{"user.name@example.com", false},
		{"user+tag@example.com", false},
		{"invalid", true},
		{"@invalid.com", true},
		{"user@", true},
		{"user@.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			err := v.ValidateEmail(tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEmail(%q) error = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ExtractDomain(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		email string
		want  string
	}{
		{"user@example.com", "example.com"},
		{"USER@EXAMPLE.COM", "example.com"},
		{"user@mail.example.com", "mail.example.com"},
		{"invalid", ""},
		{"user@", ""},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := v.ExtractDomain(tt.email)
			if got != tt.want {
				t.Errorf("ExtractDomain(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

func TestValidator_IsValidDomain(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"mail.example.com", true},
		{"a.b.c.d", true},
		{"", false},
		{"..com", false},
		{".example.com", false},
		{"example.com.", false},
		{"a", false},
		{"a..b.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := v.IsValidDomain(tt.domain)
			if got != tt.want {
				t.Errorf("IsValidDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestBuildAutoconfigURL(t *testing.T) {
	url := BuildAutoconfigURL("example.com")
	want := "https://example.com/.well-known/autoconfig/mail/config-v1.1.xml"
	if url != want {
		t.Errorf("BuildAutoconfigURL() = %q, want %q", url, want)
	}
}

func TestBuildAutodiscoverURL(t *testing.T) {
	url := BuildAutodiscoverURL("example.com")
	want := "https://autodiscover.example.com/autodiscover/autodiscover.xml"
	if url != want {
		t.Errorf("BuildAutodiscoverURL() = %q, want %q", url, want)
	}
}

func TestDefaultPorts(t *testing.T) {
	ports := DefaultPorts()

	// IMAP
	if ports["imap"]["SSL"] != 993 {
		t.Errorf("IMAP SSL port = %d, want 993", ports["imap"]["SSL"])
	}
	if ports["imap"]["STARTTLS"] != 143 {
		t.Errorf("IMAP STARTTLS port = %d, want 143", ports["imap"]["STARTTLS"])
	}

	// SMTP
	if ports["smtp"]["SSL"] != 465 {
		t.Errorf("SMTP SSL port = %d, want 465", ports["smtp"]["SSL"])
	}
	if ports["smtp"]["STARTTLS"] != 587 {
		t.Errorf("SMTP STARTTLS port = %d, want 587", ports["smtp"]["STARTTLS"])
	}
}

func TestHandler_HandleAutoconfig(t *testing.T) {
	handler := NewHandler(nil)

	tests := []struct {
		name       string
		host       string
		wantStatus int
	}{
		{"valid domain", "autoconfig.example.com", http.StatusOK},
		{"domain in host", "example.com", http.StatusOK},
		{"IP address", "192.168.1.1", http.StatusOK}, // IPs are technically valid input for the handler
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/.well-known/autoconfig/mail/config-v1.1.xml", nil)
			w := httptest.NewRecorder()

			handler.HandleAutoconfig(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("HandleAutoconfig() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				// Verify it's valid XML
				var config AutoconfigClientConfig
				if err := xml.Unmarshal(w.Body.Bytes(), &config); err != nil {
					t.Errorf("HandleAutoconfig() returned invalid XML: %v", err)
				}
			}
		})
	}
}

func TestHandler_HandleAutodiscover(t *testing.T) {
	handler := NewHandler(nil)

	tests := []struct {
		name       string
		host       string
		query      string
		wantStatus int
	}{
		{"valid domain with email", "autodiscover.example.com", "?email=user@example.com", http.StatusOK},
		{"valid domain no email", "autodiscover.example.com", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "http://" + tt.host + "/autodiscover/autodiscover.xml"
			if tt.query != "" {
				url += tt.query
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			handler.HandleAutodiscover(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("HandleAutodiscover() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				// Verify it's valid XML
				var resp AutodiscoverResponse
				if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Errorf("HandleAutodiscover() returned invalid XML: %v", err)
				}
			}
		})
	}
}

func TestHandler_HandleAutodiscover_POST(t *testing.T) {
	handler := NewHandler(nil)

	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/requestschema/2006">
  <Request>
    <EMailAddress>user@example.com</EMailAddress>
  </Request>
</Autodiscover>`

	req := httptest.NewRequest(http.MethodPost, "http://autodiscover.example.com/autodiscover/autodiscover.xml", strings.NewReader(xmlBody))
	req.Header.Set("Content-Type", "application/xml")
	w := httptest.NewRecorder()

	handler.HandleAutodiscover(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HandleAutodiscover() POST status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp AutodiscoverResponse
	if err := xml.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("HandleAutodiscover() POST returned invalid XML: %v", err)
	}
}

func TestExtractEmailFromHost(t *testing.T) {
	handler := NewHandler(nil)

	tests := []struct {
		host string
		want string
	}{
		{"user@example.com", "user@example.com"},
		{"user@EXAMPLE.COM", "user@example.com"},      // lowercase conversion
		{"user@example.com:8080", "user@example.com"}, // with port
		{"example.com", ""},                           // no email
		{"192.168.1.1", ""},                           // IP
	}

	for _, tt := range tests {
		got := handler.extractEmailFromHost(tt.host)
		if got != tt.want {
			t.Errorf("extractEmailFromHost(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestAutoconfigClientConfig_XML(t *testing.T) {
	config := &AutoconfigClientConfig{
		Version: "1.1",
		Providers: []AutoconfigProvider{
			{
				ID:     "example.com",
				Domain: []string{"example.com"},
				IncomingServers: []AutoconfigServer{
					{
						Type:           "imap",
						Hostname:       "mail.example.com",
						Port:           993,
						SocketType:     "SSL",
						Username:       "%EMAILADDRESS%",
						Authentication: "password-encrypted",
					},
				},
			},
		},
	}

	data, err := xml.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("xml.MarshalIndent() error = %v", err)
	}

	var decoded AutoconfigClientConfig
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}

	if decoded.Version != "1.1" {
		t.Errorf("decoded.Version = %q, want %q", decoded.Version, "1.1")
	}
	if len(decoded.Providers) != 1 {
		t.Fatalf("decoded.Providers length = %d, want 1", len(decoded.Providers))
	}
	if decoded.Providers[0].ID != "example.com" {
		t.Errorf("decoded.Providers[0].ID = %q, want %q", decoded.Providers[0].ID, "example.com")
	}
}
